//! Gemma 4 (text) model family: config, quantized weights and forward pass.
//!
//! Unlike Gemma 3 (dense, f32), Gemma 4 ships affine-quantized and is a
//! shared+routed MoE: every layer runs a dense "shared" MLP plus 128 routed
//! experts (top-8) via `gather_qmm`. This module loads the pre-quantized
//! weights as-is and computes through MLX's quantized ops -- the reason the
//! engine needed mxfp4-capable mlx-rs, though the 26B itself is affine.
//!
//! The checkpoint is multimodal (`Gemma4ForConditionalGeneration`); for text
//! inference we load only `language_model.*` and ignore vision/audio.
//!
//! Ported against mlx-lm's `gemma4_text.py` (the authoritative arch). 26B
//! specifics: no KV-sharing, no per-layer-input gating, K-eq-V on full layers
//! (V reuses the K projection but is RMS-normed without scale and not RoPEd),
//! global vs local head_dim (512/256), partial "proportional" RoPE on full
//! layers, and a final logit softcap.

use std::collections::{HashMap, HashSet};

use anyhow::{anyhow, Context, Result};
use mlx_rs::{fast, ops, Array, Dtype};
use oos_infer::openai::ChatMessage;
use oos_infer::ModelFiles;
use serde::Deserialize;
use std::path::Path;
use tokenizers::Tokenizer;

use super::gemma4_assistant::SharedKv;
use super::{step_masks, toolfmt, KvCache, KvSlot, MaskKind, Model, StepMasks};

/// The family's compute dtype. bf16 like the mlx_lm reference: activations,
/// norms and the KV cache at half the f32 memory traffic -- decisive on
/// Apple Silicon, where the quantized matmuls and attention are bandwidth-
/// bound. The quantized ops emit the activation dtype, so seeding embeddings
/// and parameters as bf16 keeps the whole stack in bf16; only the sampled
/// logit row is widened back to f32 at the engine boundary.
pub(super) const COMPUTE: Dtype = Dtype::Bfloat16;

/// Gemma 4 text-tower parameters, parsed from config.json's `text_config`.
///
/// Fields the 26B doesn't exercise (per-layer-input gating, KV-sharing,
/// double-wide MLP) are intentionally omitted: this targets the 26B MoE
/// checkpoint, and unsupported shapes should fail loudly at load, not be
/// silently mishandled.
#[allow(dead_code)]
#[derive(Debug, Clone, Deserialize)]
pub struct Gemma4Config {
    pub hidden_size: usize,
    pub num_hidden_layers: usize,
    pub intermediate_size: usize,
    pub num_attention_heads: usize,
    pub head_dim: usize,
    pub global_head_dim: usize,
    pub num_key_value_heads: usize,
    pub num_global_key_value_heads: usize,
    pub rms_norm_eps: f32,
    pub vocab_size: usize,
    pub sliding_window: usize,
    pub num_experts: usize,
    pub top_k_experts: usize,
    pub moe_intermediate_size: usize,
    pub final_logit_softcapping: f32,
    pub layer_types: Vec<String>,
    pub rope_parameters: RopeParameters,
    #[serde(default)]
    pub attention_k_eq_v: bool,
    #[serde(skip)]
    pub eos_token_id: Vec<u32>,
}

#[derive(Debug, Clone, Deserialize)]
pub struct RopeParameters {
    pub full_attention: RopeSpec,
    pub sliding_attention: RopeSpec,
}

#[derive(Debug, Clone, Deserialize)]
pub struct RopeSpec {
    pub rope_theta: f32,
    #[serde(default = "default_partial")]
    pub partial_rotary_factor: f32,
}

fn default_partial() -> f32 {
    1.0
}

/// gemma4's text_config carries a scalar eos; the full turn-terminator set
/// (e.g. <end_of_turn> = 106) lives in the top-level config's eos_token_id list.
fn parse_eos(root: &serde_json::Value) -> Vec<u32> {
    match root.get("eos_token_id") {
        Some(serde_json::Value::Array(a)) => a
            .iter()
            .filter_map(|v| v.as_u64().map(|u| u as u32))
            .collect(),
        Some(serde_json::Value::Number(n)) => n
            .as_u64()
            .map(|u| vec![u as u32])
            .unwrap_or_else(|| vec![1]),
        _ => vec![1],
    }
}

/// Whether a layer is local sliding-window or periodic global full attention.
/// They differ in head_dim, KV-head count, K-eq-V, and RoPE.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub(super) enum LayerKind {
    Sliding,
    Full,
}

impl Gemma4Config {
    /// Parse `text_config` out of the multimodal config.json wrapper.
    fn load(path: &Path) -> Result<Self> {
        let text =
            std::fs::read_to_string(path).with_context(|| format!("reading {}", path.display()))?;
        let root: serde_json::Value =
            serde_json::from_str(&text).with_context(|| format!("parsing {}", path.display()))?;
        let tc = root
            .get("text_config")
            .cloned()
            .unwrap_or_else(|| root.clone());
        let mut cfg: Gemma4Config =
            serde_json::from_value(tc).context("parsing gemma4 text_config")?;
        cfg.eos_token_id = parse_eos(&root);
        Ok(cfg)
    }

    fn kind(&self, layer_idx: usize) -> LayerKind {
        match self.layer_types.get(layer_idx).map(String::as_str) {
            Some("full_attention") => LayerKind::Full,
            _ => LayerKind::Sliding,
        }
    }

    fn embed_scale(&self) -> f32 {
        (self.hidden_size as f32).sqrt()
    }
}

/// A quantized linear weight, stored exactly as the checkpoint packs it:
/// `weight` is bit-packed u32 (never cast), `scales` (and `biases`, for affine)
/// carry the per-group params. The format is config-driven, so block-scaled
/// modes like mxfp4 -- which carry no biases -- load through the same struct.
/// HF lays weights out as `[out, in]`, so the matmul transposes.
///
/// Shared with the gemma4_assistant drafter (same checkpoints, same format
/// axis), hence the pub(super) visibility on the quant infrastructure here.
pub(super) struct QLinear {
    weight: Array,
    scales: Array,
    /// `None` for biasless formats (mxfp4/mxfp8); affine carries per-group biases.
    biases: Option<Array>,
    group_size: i32,
    bits: i32,
    /// Quantization mode from config.json (e.g. "affine", "mxfp4"); `None` is
    /// the affine default. Threaded into every quantized op so the format --
    /// not the architecture -- decides the math.
    mode: Option<String>,
}

impl QLinear {
    /// Gather the packed rows for `ids` and dequantize just those -- the
    /// quantized-embedding lookup. Output dtype follows the format's scales;
    /// callers cast to their compute dtype. pub(super): the mistral family
    /// shares the quant infrastructure (same checkpoints' format axis).
    pub(super) fn dequant_rows(&self, ids: &Array) -> Result<Array> {
        use mlx_rs::ops::indexing::IndexOp;
        let w = self.weight.index(ids);
        let s = self.scales.index(ids);
        let b = self.biases.as_ref().map(|bz| bz.index(ids));
        Ok(ops::dequantize(
            &w,
            &s,
            b.as_ref(),
            self.group_size,
            self.bits,
            self.mode.as_deref(),
        )?)
    }

    pub(super) fn forward(&self, x: &Array) -> Result<Array> {
        Ok(ops::quantized_matmul(
            x,
            &self.weight,
            &self.scales,
            self.biases.as_ref(),
            true,
            self.group_size,
            self.bits,
            self.mode.as_deref(),
        )?)
    }
}

/// Per-path quantization spec, read from config.json's `quantization` block
/// rather than hardcoded per-path bit widths. This keeps the loader
/// format-driven and architecture-independent: the checkpoint declares each
/// module's group_size/bits/mode, so a future mxfp4 model loads with no code
/// change here.
///
/// The block is a flat map of scalar globals (`group_size`/`bits`/`mode`) plus
/// per-module overrides keyed by the exact tensor prefix. Overrides carry only
/// the fields they change; the rest inherit the global.
pub(super) struct QuantConfig {
    mode: Option<String>,
    group_size: i32,
    bits: i32,
    /// Per-prefix (group_size, bits) overrides; keys match the prefix passed
    /// to `spec_for` verbatim.
    overrides: HashMap<String, (i32, i32)>,
}

impl QuantConfig {
    pub(super) fn load(path: &Path) -> Result<Self> {
        let text =
            std::fs::read_to_string(path).with_context(|| format!("reading {}", path.display()))?;
        let root: serde_json::Value =
            serde_json::from_str(&text).with_context(|| format!("parsing {}", path.display()))?;
        // mlx-community ships the block under both keys; they are identical.
        let block = root
            .get("quantization")
            .or_else(|| root.get("quantization_config"))
            .and_then(|v| v.as_object())
            .ok_or_else(|| anyhow!("config.json has no quantization block"))?;

        // Pass 1: scalar globals. Defaults reproduce the prior hardcoded
        // behavior (group 64 / 4-bit) if a field is absent.
        let mut group_size: i32 = 64;
        let mut bits: i32 = 4;
        let mut mode: Option<String> = None;
        for (k, v) in block {
            match (k.as_str(), v) {
                ("group_size", serde_json::Value::Number(n)) => {
                    if let Some(x) = n.as_i64() {
                        group_size = x as i32;
                    }
                }
                ("bits", serde_json::Value::Number(n)) => {
                    if let Some(x) = n.as_i64() {
                        bits = x as i32;
                    }
                }
                ("mode", serde_json::Value::String(s)) => mode = Some(s.clone()),
                _ => {}
            }
        }

        // Pass 2: per-module overrides; missing fields inherit the global.
        // Two passes so resolution is independent of JSON key ordering.
        let mut overrides = HashMap::new();
        for (k, v) in block {
            if let serde_json::Value::Object(o) = v {
                let gs = o
                    .get("group_size")
                    .and_then(|x| x.as_i64())
                    .map(|x| x as i32)
                    .unwrap_or(group_size);
                let b = o
                    .get("bits")
                    .and_then(|x| x.as_i64())
                    .map(|x| x as i32)
                    .unwrap_or(bits);
                overrides.insert(k.clone(), (gs, b));
            }
        }

        Ok(Self {
            mode,
            group_size,
            bits,
            overrides,
        })
    }

    /// Resolve (group_size, bits) for a tensor prefix: an exact-match override
    /// if the checkpoint declared one, else the global default.
    fn spec_for(&self, prefix: &str) -> (i32, i32) {
        // qwen3_5 strips the multimodal `language_model.` prefix off its weight
        // names (to load 9B text and 35B VLM checkpoints uniformly), so its
        // lookup prefixes are `model.*` while the override keys stay
        // `language_model.model.*`; fall back to the prefixed key so the 8-bit
        // router / shared_expert_gate overrides still resolve. Gemma keeps the
        // full prefix and hits the exact match first, unaffected.
        self.overrides
            .get(prefix)
            .or_else(|| self.overrides.get(&format!("language_model.{prefix}")))
            .copied()
            .unwrap_or((self.group_size, self.bits))
    }

    /// Build a [`QLinear`] for `prefix` from a loaded tensor map. The format
    /// details come from the tensors themselves, not the architecture: a
    /// missing `.biases` marks a biasless format (mxfp4/mxfp8 -- affine ships
    /// them), and integer (e8m0 block-exponent) scales stay native because
    /// the quantized ops require them verbatim, while float scales are cast
    /// to the compute dtype to match the activations. The bit-packed
    /// `weight` is never cast.
    pub(super) fn qlinear(&self, w: &HashMap<String, Array>, prefix: &str) -> Result<QLinear> {
        let fetch = |name: String| -> Result<Array> {
            w.get(&name)
                .cloned()
                .ok_or_else(|| anyhow!("missing tensor {name}"))
        };
        let (group_size, bits) = self.spec_for(prefix);
        let biases = match w.get(&format!("{prefix}.biases")) {
            Some(a) => Some(a.as_dtype(COMPUTE)?),
            None => None,
        };
        let scales_raw = fetch(format!("{prefix}.scales"))?;
        let scales = if scales_raw.dtype() == Dtype::Uint8 {
            scales_raw
        } else {
            scales_raw.as_dtype(COMPUTE)?
        };
        // Mixed checkpoints (e.g. qat-nvfp4: fp4 attention/experts plus
        // affine 8-bit dense MLP) declare per-module overrides without a
        // mode field, so the mode is read off the tensors instead: only
        // affine ships per-group biases, every block-scaled format
        // (nvfp4/mxfp4/mxfp8) is biasless.
        let mode = if biases.is_some() {
            None
        } else {
            self.mode.clone()
        };
        Ok(QLinear {
            weight: fetch(format!("{prefix}.weight"))?,
            scales,
            biases,
            group_size,
            bits,
            mode,
        })
    }
}

/// Load every shard listed in `model.safetensors.index.json` and merge into one
/// tensor map. The 26B is sharded; a single-file `model.safetensors` is also
/// accepted as a fallback.
pub(super) fn load_weights(dir: &Path) -> Result<HashMap<String, Array>> {
    let index = dir.join("model.safetensors.index.json");
    if !index.exists() {
        let single = dir.join("model.safetensors");
        return Array::load_safetensors(&single)
            .map_err(|e| anyhow!("loading {}: {e}", single.display()));
    }

    #[derive(Deserialize)]
    struct Index {
        weight_map: HashMap<String, String>,
    }
    let text =
        std::fs::read_to_string(&index).with_context(|| format!("reading {}", index.display()))?;
    let idx: Index =
        serde_json::from_str(&text).with_context(|| format!("parsing {}", index.display()))?;

    let shards: HashSet<&String> = idx.weight_map.values().collect();
    let mut weights: HashMap<String, Array> = HashMap::new();
    for shard in shards {
        let path = dir.join(shard);
        let part = Array::load_safetensors(&path)
            .map_err(|e| anyhow!("loading {}: {e}", path.display()))?;
        weights.extend(part);
    }
    Ok(weights)
}

/// Precompute the per-frequency table for the full layers' "proportional"
/// partial RoPE: the first `rotated_dims` head dims rotate at `theta`, the rest
/// get an infinite frequency (zero angle => identity / pass-through).
pub(super) fn proportional_freqs(head_dim: usize, partial_rotary_factor: f32, theta: f32) -> Array {
    let rotated = ((head_dim as f32 * partial_rotary_factor) as usize) & !1; // even
    let mut f = Vec::with_capacity(head_dim / 2);
    let mut i = 0;
    while i < rotated {
        f.push(theta.powf(i as f32 / head_dim as f32));
        i += 2;
    }
    for _ in 0..((head_dim - rotated) / 2) {
        f.push(f32::INFINITY);
    }
    Array::from_slice(&f, &[f.len() as i32])
}

/// One attention block's quantized projections and norms. `v_proj` is absent on
/// full layers (K-eq-V): values reuse the K projection there.
struct Attn {
    kind: LayerKind,
    head_dim: i32,
    n_heads: i32,
    n_kv_heads: i32,
    rope_theta: f32,
    q_proj: QLinear,
    k_proj: QLinear,
    v_proj: Option<QLinear>,
    o_proj: QLinear,
    q_norm: Array,
    k_norm: Array,
}

/// The routed-expert FFN: a router projection (plus its scales) selecting top-k
/// of `num_experts`, and the stacked SwitchGLU expert weights addressed by
/// `gather_qmm`.
struct Moe {
    router_proj: QLinear,
    router_scale: Array,
    per_expert_scale: Array,
    gate: QLinear,
    up: QLinear,
    down: QLinear,
}

/// One decoder layer: attention + (shared dense MLP || routed experts), with
/// Gemma 4's seven RMSNorms and the per-layer output scalar.
struct Layer {
    input_ln: Array,
    post_attn_ln: Array,
    pre_ff_ln: Array,
    pre_ff_ln2: Array,
    post_ff_ln1: Array,
    post_ff_ln2: Array,
    post_ff_ln: Array,
    layer_scalar: Array,
    attn: Attn,
    mlp_gate: QLinear,
    mlp_up: QLinear,
    mlp_down: QLinear,
    moe: Moe,
}

pub struct Gemma4Model {
    cfg: Gemma4Config,
    embed: QLinear,
    final_norm: Array,
    full_freqs: Array,
    layers: Vec<Layer>,
    stop: Vec<i32>,
}

impl Gemma4Model {
    pub fn load(files: &ModelFiles, tokenizer: &Tokenizer) -> Result<Self> {
        let cfg = Gemma4Config::load(&files.config_json).context("loading gemma4 config")?;
        let w = load_weights(&files.dir)?;

        // Norms and scalars in the compute dtype (the quantized tensors go
        // through QuantConfig::qlinear instead).
        let get = |name: &str| -> Result<Array> {
            w.get(name)
                .ok_or_else(|| anyhow!("missing tensor {name}"))
                .and_then(|a| Ok(a.as_dtype(COMPUTE)?))
        };
        let qcfg = QuantConfig::load(&files.config_json).context("loading gemma4 quant config")?;
        // The format/bias/scale handling lives in QuantConfig::qlinear, shared
        // with the gemma4_assistant drafter.
        let qlinear = |prefix: &str| qcfg.qlinear(&w, prefix);

        let lm = "language_model.model";
        let embed = qlinear(&format!("{lm}.embed_tokens"))?;
        let final_norm = get(&format!("{lm}.norm.weight"))?;

        let mut layers = Vec::with_capacity(cfg.num_hidden_layers);
        for i in 0..cfg.num_hidden_layers {
            let p = format!("{lm}.layers.{i}");
            let kind = cfg.kind(i);
            let (head_dim, n_kv_heads, rope) = match kind {
                LayerKind::Full => (
                    cfg.global_head_dim,
                    cfg.num_global_key_value_heads,
                    &cfg.rope_parameters.full_attention,
                ),
                LayerKind::Sliding => (
                    cfg.head_dim,
                    cfg.num_key_value_heads,
                    &cfg.rope_parameters.sliding_attention,
                ),
            };
            // K-eq-V (full layers) means the checkpoint has no v_proj: values
            // reuse the K projection at forward time.
            let v_proj = if cfg.attention_k_eq_v && kind == LayerKind::Full {
                None
            } else {
                Some(qlinear(&format!("{p}.self_attn.v_proj"))?)
            };

            let attn = Attn {
                kind,
                head_dim: head_dim as i32,
                n_heads: cfg.num_attention_heads as i32,
                n_kv_heads: n_kv_heads as i32,
                rope_theta: rope.rope_theta,
                q_proj: qlinear(&format!("{p}.self_attn.q_proj"))?,
                k_proj: qlinear(&format!("{p}.self_attn.k_proj"))?,
                v_proj,
                o_proj: qlinear(&format!("{p}.self_attn.o_proj"))?,
                q_norm: get(&format!("{p}.self_attn.q_norm.weight"))?,
                k_norm: get(&format!("{p}.self_attn.k_norm.weight"))?,
            };

            let moe = Moe {
                router_proj: qlinear(&format!("{p}.router.proj"))?,
                router_scale: get(&format!("{p}.router.scale"))?,
                per_expert_scale: get(&format!("{p}.router.per_expert_scale"))?,
                gate: qlinear(&format!("{p}.experts.switch_glu.gate_proj"))?,
                up: qlinear(&format!("{p}.experts.switch_glu.up_proj"))?,
                down: qlinear(&format!("{p}.experts.switch_glu.down_proj"))?,
            };

            layers.push(Layer {
                input_ln: get(&format!("{p}.input_layernorm.weight"))?,
                post_attn_ln: get(&format!("{p}.post_attention_layernorm.weight"))?,
                pre_ff_ln: get(&format!("{p}.pre_feedforward_layernorm.weight"))?,
                pre_ff_ln2: get(&format!("{p}.pre_feedforward_layernorm_2.weight"))?,
                post_ff_ln1: get(&format!("{p}.post_feedforward_layernorm_1.weight"))?,
                post_ff_ln2: get(&format!("{p}.post_feedforward_layernorm_2.weight"))?,
                post_ff_ln: get(&format!("{p}.post_feedforward_layernorm.weight"))?,
                layer_scalar: get(&format!("{p}.layer_scalar"))?,
                attn,
                mlp_gate: qlinear(&format!("{p}.mlp.gate_proj"))?,
                mlp_up: qlinear(&format!("{p}.mlp.up_proj"))?,
                mlp_down: qlinear(&format!("{p}.mlp.down_proj"))?,
                moe,
            });
        }

        let full_freqs = {
            let r = &cfg.rope_parameters.full_attention;
            proportional_freqs(cfg.global_head_dim, r.partial_rotary_factor, r.rope_theta)
        };

        let mut stop: Vec<i32> = cfg.eos_token_id.iter().map(|&u| u as i32).collect();
        if let Some(id) = tokenizer.token_to_id("<end_of_turn>") {
            stop.push(id as i32);
        }

        Ok(Self {
            cfg,
            embed,
            final_norm,
            full_freqs,
            layers,
            stop,
        })
    }
}

/// tanh-approximated GELU with dtype-preserving constants:
/// `0.5 * x * (1 + tanh(sqrt(2/pi) * (x + 0.044715 * x^3)))`.
///
/// Not mlx-rs's `nn::gelu_approximate`: that builds its constants as f32
/// arrays, and MLX's array-array promotion is strong, so a bf16 activation
/// is promoted to f32 *inside* the GELU. The f32 result then drags the whole
/// residual stream into f32 for the rest of the layer -- on every layer,
/// in both the shared MLP and the expert path -- which cast-stormed bf16
/// decode down to ~11 tok/s. Constants in `x`'s dtype keep the graph in the
/// compute dtype end to end. pub(super): the drafter shares it.
pub(super) fn gelu_tanh(x: &Array) -> Result<Array> {
    let dt = x.dtype();
    let c = |v: f32| Array::from_slice(&[v], &[1]).as_dtype(dt);
    let half = c(0.5)?;
    let one = c(1.0)?;
    let beta = c(0.797_884_56)?; // sqrt(2/pi)
    let alpha = c(0.044_715)?;
    let x3 = x.multiply(x)?.multiply(x)?;
    let inner = beta.multiply(&x.add(&alpha.multiply(&x3)?)?)?;
    let t = ops::tanh(&inner)?;
    half.multiply(x)?.multiply(&one.add(&t)?).map_err(Into::into)
}

/// RMSNorm with a unit (no-scale) weight -- Gemma 4's V normalization.
fn rms_no_scale(x: &Array, eps: f32) -> Result<Array> {
    let dim = *x.shape().last().unwrap();
    let ones = ops::ones_dtype(&[dim], x.dtype())?;
    Ok(fast::rms_norm(x, &ones, eps)?)
}

/// One expert projection over gathered top-k indices (SwitchGLU via
/// gather_qmm). `sorted` promises the kernel that `idx` is ascending, which
/// lets it stream each expert's weights once instead of re-fetching them in
/// routing order -- only valid on the sorted prefill path.
pub(super) fn gather(ql: &QLinear, x: &Array, idx: &Array, sorted: bool) -> Result<Array> {
    Ok(ops::gather_qmm(
        x,
        &ql.weight,
        &ql.scales,
        ql.biases.as_ref(),
        None,
        Some(idx),
        true,
        ql.group_size,
        ql.bits,
        sorted,
        ql.mode.as_deref(),
    )?)
}

impl Attn {
    /// Partial "proportional" RoPE on full layers (precomputed `full_freqs`),
    /// plain RoPE on sliding layers.
    fn apply_rope(&self, t: &Array, offset: i32, full_freqs: &Array) -> Result<Array> {
        Ok(match self.kind {
            LayerKind::Full => {
                fast::rope(t, self.head_dim, false, None, 1.0, offset, Some(full_freqs))?
            }
            LayerKind::Sliding => fast::rope(
                t,
                self.head_dim,
                false,
                Some(self.rope_theta),
                1.0,
                offset,
                None,
            )?,
        })
    }

    fn forward(
        &self,
        x: &Array,
        eps: f32,
        mask: &MaskKind,
        offset: i32,
        full_freqs: &Array,
        window: Option<i32>,
        slot: &mut KvSlot,
    ) -> Result<Array> {
        let seq = x.shape()[0];
        let (n, nkv, hd) = (self.n_heads, self.n_kv_heads, self.head_dim);

        let q = self.q_proj.forward(x)?.reshape(&[1, seq, n, hd])?;
        let q = fast::rms_norm(&q, &self.q_norm, eps)?;
        let q = q.transpose_axes(&[0, 2, 1, 3])?;
        let q = self.apply_rope(&q, offset, full_freqs)?;

        // K-eq-V (full layers): V reuses the K projection; V is always no-scale
        // RMS-normed and never RoPE'd.
        let k_raw = self.k_proj.forward(x)?.reshape(&[1, seq, nkv, hd])?;
        let v_raw = match &self.v_proj {
            Some(vp) => vp.forward(x)?.reshape(&[1, seq, nkv, hd])?,
            None => k_raw.clone(),
        };
        let k = fast::rms_norm(&k_raw, &self.k_norm, eps)?;
        let k = k.transpose_axes(&[0, 2, 1, 3])?;
        let k = self.apply_rope(&k, offset, full_freqs)?;
        let v = rms_no_scale(&v_raw, eps)?.transpose_axes(&[0, 2, 1, 3])?;

        // The slot persists this step's K/V (in place, ring-rotated on
        // sliding layers) and hands back the K/V to attend over; the mask
        // built in `step_masks` matches the slot's retention geometry.
        let (k, v) = slot.update(&k, &v, window)?;

        // Gemma 4 normalizes Q/K per head and runs SDPA at scale 1.0. The
        // mask was built once for the whole step; None and Causal run the
        // fused kernel paths with nothing materialized.
        let sdpa_mask = match mask {
            MaskKind::None => None,
            MaskKind::Causal => Some(fast::ScaledDotProductAttentionMask::Causal),
            MaskKind::Mask(m) => Some(fast::ScaledDotProductAttentionMask::Array(m)),
        };
        let o = fast::scaled_dot_product_attention(&q, &k, &v, 1.0, sdpa_mask, None)?;
        let o = o.transpose_axes(&[0, 2, 1, 3])?.reshape(&[seq, n * hd])?;
        self.o_proj.forward(&o)
    }
}

impl Moe {
    /// Routed sparse FFN: route to top-k experts, run SwitchGLU over them, and
    /// combine by the (renormalized, per-expert-scaled) routing weights.
    #[allow(clippy::too_many_arguments)]
    fn forward(
        &self,
        route_x: &Array,
        expert_x: &Array,
        num_experts: i32,
        top_k: i32,
        hidden: i32,
        eps: f32,
        seg: &mut Option<&mut [f64; 6]>,
    ) -> Result<Array> {
        let mut t0 = std::time::Instant::now();
        let seq = expert_x.shape()[0];

        // Router: rms_norm(x, scale * hidden^-0.5) -> proj -> top-k -> softmax.
        // Routed on the *raw* post-attention residual, not the pre-FF-normed
        // tensor: the router norms its input itself, and pre_ff_ln2's learned
        // per-dim weight would re-aim the vector and flip top-k expert picks
        // (the reference routes on `h` while the experts consume the normed
        // h2 -- mlx_lm gemma4_text.py DecoderLayer).
        // The scalar must sit in the compute dtype: an f32 literal would
        // promote the product and pull the router (and everything after the
        // norm) back into f32 compute.
        let root = Array::from_slice(&[(hidden as f32).powf(-0.5)], &[1]).as_dtype(COMPUTE)?;
        let rw = self.router_scale.multiply(&root)?;
        let xr = fast::rms_norm(route_x, &rw, eps)?;
        let scores = self.router_proj.forward(&xr)?; // [seq, E]

        let part = ops::argpartition_axis(&scores, -top_k, -1)?; // [seq, E]
        let last = Array::arange::<_, i32>(num_experts - top_k, num_experts, None)?;
        let idx = ops::indexing::take_axis(&part, &last, -1)?; // [seq, k] expert ids
        let sel = ops::indexing::take_along_axis(&scores, &idx, -1)?; // [seq, k]
        let weights = ops::softmax_axis(&sel, -1, None)?;
        let per_expert = ops::indexing::take(&self.per_expert_scale, &idx)?; // [seq, k]
        let weights = weights.multiply(&per_expert)?; // [seq, k]
        seg_mark(seg, 2, &weights, &mut t0)?;

        // SwitchGLU over the selected experts. With many (token, expert)
        // pairs -- prefill -- sort the pairs by expert id first so gather_qmm
        // streams each selected expert's weights once, contiguously, instead
        // of re-fetching them in routing order (mlx_lm's _gather_sort, same
        // >= 64 threshold: single-token decode, n = top_k = 8, stays on the
        // plain path and is bit-identical). Logically a permutation, but the
        // sorted kernel batches rows per expert, so accumulation order -- and
        // with it bf16 rounding -- can differ from the unsorted path at the
        // last bit; at temp 0 a borderline token may flip (3877-token A/B:
        // same summary, one synonymous phrase). Same equivalence class as
        // mlx_lm's own sorted path. Measured prefill on that A/B: 101 -> 278
        // tok/s.
        let n = seq * top_k;
        let y = if n >= 64 {
            let flat = idx.reshape(&[n])?; // [n] expert ids in routing order
            let order = ops::argsort(&flat)?;
            let inv_order = ops::argsort(&order)?;
            let sorted_idx = ops::indexing::take(&flat, &order)?; // ascending
            // order / k maps each sorted pair back to its token row; the x
            // batch stays 3-D ([n, 1, hidden]) so it broadcasts 1:1 against
            // the flat index vector.
            let rows = ops::floor_divide(&order, &Array::from_int(top_k))?;
            let xs = ops::indexing::take_axis(expert_x, &rows, 0)?
                .reshape(&[n, 1, hidden])?;
            let up = gather(&self.up, &xs, &sorted_idx, true)?;
            let gate = gather(&self.gate, &xs, &sorted_idx, true)?;
            let act = gelu_tanh(&gate)?.multiply(&up)?;
            let down = gather(&self.down, &act, &sorted_idx, true)?;
            let down = down.reshape(&[n, hidden])?;
            ops::indexing::take_axis(&down, &inv_order, 0)?
                .reshape(&[seq, top_k, hidden])?
        } else {
            let xe = ops::expand_dims_axes(expert_x, &[-2, -3])?; // [seq, 1, 1, hidden]
            let up = gather(&self.up, &xe, &idx, false)?;
            let gate = gather(&self.gate, &xe, &idx, false)?;
            let act = gelu_tanh(&gate)?.multiply(&up)?;
            let down = gather(&self.down, &act, &idx, false)?;
            down.reshape(&[seq, top_k, hidden])? // [seq, k, hidden]
        };
        seg_mark(seg, 3, &y, &mut t0)?;

        // Weighted sum over the k experts.
        let w = ops::expand_dims_axes(&weights, &[-1])?; // [seq, k, 1]
        let out = w.multiply(&y)?.sum_axes(&[-2], false)?; // [seq, hidden]
        seg_mark(seg, 4, &out, &mut t0)?;
        Ok(out)
    }
}

/// Profiler helper for OOSMLX_STEP_PROFILE: with an active segment
/// accumulator, force an eval at this boundary and book the elapsed time on
/// segment `idx`. A free function (not a closure) so the accumulator can also
/// be passed onward between marks.
fn seg_mark(
    seg: &mut Option<&mut [f64; 6]>,
    idx: usize,
    a: &Array,
    t0: &mut std::time::Instant,
) -> Result<()> {
    if let Some(seg) = seg.as_deref_mut() {
        a.eval()?;
        seg[idx] += t0.elapsed().as_secs_f64() * 1e3;
        *t0 = std::time::Instant::now();
    }
    Ok(())
}

impl Layer {
    #[allow(clippy::too_many_arguments)]
    fn forward(
        &self,
        x: &Array,
        cfg: &Gemma4Config,
        offset: i32,
        full_freqs: &Array,
        masks: &StepMasks,
        slot: &mut KvSlot,
        // Segment-time accumulator [attn, shared-mlp, moe-router, moe-gathers,
        // moe-combine, rest]; `Some` only under OOSMLX_STEP_PROFILE, forcing
        // an eval per segment.
        seg: &mut Option<&mut [f64; 6]>,
    ) -> Result<Array> {
        let mut t0 = std::time::Instant::now();

        let eps = cfg.rms_norm_eps;
        let (mask, window) = match self.attn.kind {
            LayerKind::Sliding => (&masks.sliding, Some(cfg.sliding_window as i32)),
            LayerKind::Full => (&masks.full, None),
        };

        // Attention with sandwich norm.
        let normed = fast::rms_norm(x, &self.input_ln, eps)?;
        let attn = self
            .attn
            .forward(&normed, eps, mask, offset, full_freqs, window, slot)?;
        let attn = fast::rms_norm(&attn, &self.post_attn_ln, eps)?;
        let h = x.add(&attn)?;
        seg_mark(seg, 0, &h, &mut t0)?;

        // Shared dense MLP (h1) plus routed experts (h2), each with its own
        // pre/post feedforward norms; summed, then a final feedforward norm.
        let h1 = fast::rms_norm(&h, &self.pre_ff_ln, eps)?;
        let gate = gelu_tanh(&self.mlp_gate.forward(&h1)?)?;
        let up = self.mlp_up.forward(&h1)?;
        let h1 = self.mlp_down.forward(&gate.multiply(&up)?)?;
        let h1 = fast::rms_norm(&h1, &self.post_ff_ln1, eps)?;
        seg_mark(seg, 1, &h1, &mut t0)?;

        let h2 = fast::rms_norm(&h, &self.pre_ff_ln2, eps)?;
        let h2 = self
            .moe
            .forward(
                &h,
                &h2,
                cfg.num_experts as i32,
                cfg.top_k_experts as i32,
                cfg.hidden_size as i32,
                eps,
                seg,
            )?;
        let h2 = fast::rms_norm(&h2, &self.post_ff_ln2, eps)?;

        let ff = h1.add(&h2)?;
        let ff = fast::rms_norm(&ff, &self.post_ff_ln, eps)?;
        let h = h.add(&ff)?;

        let out = h.multiply(&self.layer_scalar)?;
        seg_mark(seg, 5, &out, &mut t0)?;
        Ok(out)
    }
}

impl Gemma4Model {
    /// Look up token embeddings from the quantized embedding table: gather the
    /// packed rows for `tokens` and dequantize just those. Includes the
    /// `embed_scale`. pub(super) because the drafter embeds its draft tokens
    /// through the *target's* table (its input is backbone-width).
    pub(super) fn embed(&self, tokens: &[i32]) -> Result<Array> {
        self.embed_ids(&Array::from_slice(tokens, &[tokens.len() as i32]))
    }

    /// Array-id variant of [`Self::embed`]: the decode loop feeds the previous
    /// step's un-evaluated pick straight back in, so ids arrive as a lazy
    /// device array rather than host integers.
    fn embed_ids(&self, ids: &Array) -> Result<Array> {
        use mlx_rs::ops::indexing::IndexOp;
        let w = self.embed.weight.index(ids);
        let s = self.embed.scales.index(ids);
        let b = self.embed.biases.as_ref().map(|bz| bz.index(ids));
        // Seed the stack in the compute dtype: the quantized matmuls emit
        // their activation dtype, so the embedding decides what every layer
        // computes in. dequantize's output dtype follows the format's scales,
        // hence the explicit cast.
        let h = ops::dequantize(
            &w,
            &s,
            b.as_ref(),
            self.embed.group_size,
            self.embed.bits,
            self.embed.mode.as_deref(),
        )?
        .as_dtype(COMPUTE)?;
        let scale = Array::from_slice(&[self.cfg.embed_scale()], &[1]).as_dtype(COMPUTE)?;
        Ok(h.multiply(&scale)?)
    }

    /// The parsed text-tower config; the speculative pairing validates a
    /// drafter's backbone width and vocab against it.
    pub(super) fn config(&self) -> &Gemma4Config {
        &self.cfg
    }

    /// Run the layer stack and return the *pre-final-norm* hidden states
    /// `[seq, hidden]`. Split from logit projection because the speculative
    /// drafter recurs on exactly this hidden (mlx-vlm taps it before
    /// `model.norm`); the plain decode path composes both via `forward`.
    pub(super) fn forward_hidden(&self, tokens: &[i32], cache: &mut KvCache) -> Result<Array> {
        self.forward_hidden_ids(&Array::from_slice(tokens, &[tokens.len() as i32]), cache)
    }

    /// Array-id variant of [`Self::forward_hidden`], the actual layer-stack
    /// run; see [`Self::embed_ids`] for why ids arrive as a device array.
    fn forward_hidden_ids(&self, ids: &Array, cache: &mut KvCache) -> Result<Array> {
        let seq = ids.dim(0);
        // OOSMLX_STEP_PROFILE=1: force an eval per layer and print per-layer
        // wall times for single-token steps. Costs pipelining (absolute
        // numbers shift), but the *distribution* localizes a regression --
        // built to hunt the bf16 decode collapse.
        let profile = seq == 1 && std::env::var_os("OOSMLX_STEP_PROFILE").is_some();
        let mut h = self.embed_ids(ids)?;
        let offset = cache.offset() as i32;
        let masks = step_masks(
            offset,
            seq,
            self.cfg.sliding_window as i32,
            !cache.is_linear(),
            COMPUTE,
        )?;
        let mut acc = [0f64; 6];
        for (layer, slot) in self.layers.iter().zip(cache.slots_mut().iter_mut()) {
            let mut seg = profile.then_some(&mut acc);
            h = layer.forward(&h, &self.cfg, offset, &self.full_freqs, &masks, slot, &mut seg)?;
        }
        if profile {
            let total: f64 = acc.iter().sum();
            eprintln!(
                "step_profile total={total:.1}ms attn={:.1} mlp={:.1} \
                 router={:.1} gathers={:.1} combine={:.1} rest={:.1}",
                acc[0], acc[1], acc[2], acc[3], acc[4], acc[5]
            );
        }
        cache.advance(seq as usize);
        Ok(h)
    }

    /// Final norm + tied quantized LM head + logit softcap over pre-norm
    /// hidden states from `forward_hidden`.
    pub(super) fn project_logits(&self, h: &Array) -> Result<Array> {
        let h = fast::rms_norm(h, &self.final_norm, self.cfg.rms_norm_eps)?;
        // Tied embeddings: logits = h @ embed^T via the quantized embedding.
        let logits = self.embed.forward(&h)?;
        // Final logit softcap: tanh(logits / cap) * cap.
        let cap = self.cfg.final_logit_softcapping;
        let cap_a = Array::from_slice(&[cap], &[1]).as_dtype(COMPUTE)?;
        Ok(ops::tanh(&logits.divide(&cap_a)?)?.multiply(&cap_a)?)
    }

    /// Export the drafter's borrowed target state from the KV cache: the
    /// accumulated post-RoPE K/V of the *last* full-attention and *last*
    /// sliding-attention layers. Iteration order makes "last wins" implicit;
    /// the clones are MLX handles sharing the device buffers, not copies.
    pub(super) fn shared_kv(&self, cache: &KvCache) -> Result<SharedKv> {
        let mut full = None;
        let mut sliding = None;
        for (i, layer) in self.layers.iter().enumerate() {
            if let Some(kv) = cache.layer_kv(i)? {
                match layer.attn.kind {
                    LayerKind::Full => full = Some(kv),
                    LayerKind::Sliding => sliding = Some(kv),
                }
            }
        }
        Ok(SharedKv {
            full: full.ok_or_else(|| anyhow!("shared_kv: no full-attention layer cached"))?,
            sliding: sliding
                .ok_or_else(|| anyhow!("shared_kv: no sliding-attention layer cached"))?,
        })
    }
}

impl Model for Gemma4Model {
    fn num_layers(&self) -> usize {
        self.cfg.num_hidden_layers
    }

    fn forward_logits(&self, tokens: &Array, cache: &mut KvCache) -> Result<Array> {
        use mlx_rs::ops::indexing::IndexOp;
        // Project only the position we keep: a long prefill's [seq, vocab]
        // logits are one huge quantized matmul plus softcap of which a single
        // row survives, and MLX's laziness cannot prune inside a single node.
        // Norm, head and softcap are row-wise, so slicing first is exact.
        let h = self.forward_hidden_ids(tokens, cache)?;
        let last = h.index(tokens.dim(0) - 1).reshape(&[1, -1])?;
        Ok(self.project_logits(&last)?.index(0))
    }

    fn render_prompt(
        &self,
        messages: &[ChatMessage],
        thinking: bool,
        tools: &[oos_infer::openai::Tool],
    ) -> String {
        // Gemma 4's own turn format, from the checkpoint's chat_template.jinja
        // (NOT gemma3's <start_of_turn>): turns are `<|turn>{role}` ...
        // `<turn|>`, and model turns carry a `thought` channel. Thinking is
        // steered exactly as the template does it: enabled, the system turn
        // opens with a `<|think|>` marker (even without a system message) and
        // the model opens its own thought channel; disabled, the generation
        // prompt pre-fills an *empty* thought channel, which suppresses it.
        let mut p = String::from("<bos>");
        let mut rest = messages;
        let first_is_system = messages
            .first()
            .map(|m| m.role == "system" || m.role == "developer")
            .unwrap_or(false);
        if thinking || first_is_system || !tools.is_empty() {
            p.push_str("<|turn>system\n");
            if thinking {
                // No newline after the marker: the community checkpoint's
                // Jinja writes `<|think|>\n`, but Google's thinking docs and
                // the transformers processor write `<|think|><turn|>`, and
                // mlx_lm thinks correctly on the documented form against this
                // very checkpoint -- so the docs win over the shipped Jinja.
                p.push_str("<|think|>");
            }
            if first_is_system {
                p.push_str(messages[0].content.trim());
                rest = &messages[1..];
            }
            // Tool declarations close the system turn, after any system text.
            for tool in tools {
                p.push_str("<|tool>");
                p.push_str(toolfmt::render_declaration(&tool.function).trim());
                p.push_str("<tool|>");
            }
            p.push_str("<turn|>\n");
        }
        let mut prev_role: Option<&str> = None;
        // A model turn the template left open after tool traffic: generation
        // (or the next assistant part) continues inside it, with no new
        // `<|turn>model` and no thought-channel prefill.
        let mut open_model_turn = false;
        for (i, m) in rest.iter().enumerate() {
            if m.role == "tool" {
                // Consumed by the forward-scan of the assistant message that
                // issued the calls; a stray tool message without one is
                // dropped rather than mis-rendered as a turn.
                continue;
            }
            let role = if m.role == "assistant" {
                "model"
            } else {
                m.role.as_str()
            };
            // The template folds consecutive assistant messages into one model
            // turn: the duplicate opening marker is suppressed, each part
            // still closes with <turn|>.
            let continued = role == "model" && (prev_role == Some("model") || open_model_turn);
            if !continued {
                p.push_str("<|turn>");
                p.push_str(role);
                p.push('\n');
            }
            open_model_turn = false;
            // Tool calls, then their results (OpenAI shape: consecutive
            // role:tool messages after the assistant message that called),
            // then any content -- the template's rendering order.
            let mut rendered_calls = false;
            let mut rendered_responses = false;
            if role == "model" {
                if let Some(calls) = &m.tool_calls {
                    for call in calls {
                        p.push_str("<|tool_call>call:");
                        p.push_str(&call.function.name);
                        p.push('{');
                        p.push_str(&toolfmt::render_call_args(&call.function.arguments));
                        p.push_str("}<tool_call|>");
                    }
                    rendered_calls = !calls.is_empty();
                    for follow in rest[i + 1..].iter().take_while(|f| f.role == "tool") {
                        let name = follow
                            .tool_call_id
                            .as_deref()
                            .and_then(|id| calls.iter().find(|c| c.id == id))
                            .map(|c| c.function.name.as_str())
                            .unwrap_or("unknown");
                        p.push_str(&toolfmt::render_response_block(name, &follow.content));
                        rendered_responses = true;
                    }
                }
            }
            let content = if role == "model" {
                strip_thinking(&m.content)
            } else {
                m.content.trim().to_string()
            };
            let has_content = !content.is_empty();
            p.push_str(&content);
            // Turn close, as the template does it: a call still waiting for
            // results primes an open `<|tool_response>`; results without
            // trailing content leave the model turn open for continuation;
            // anything else closes the turn.
            if rendered_calls && !rendered_responses {
                p.push_str("<|tool_response>");
                open_model_turn = true;
            } else if rendered_responses && !has_content {
                open_model_turn = true;
            } else {
                p.push_str("<turn|>\n");
            }
            prev_role = Some(role);
        }
        if !open_model_turn {
            p.push_str("<|turn>model\n");
            if !thinking {
                p.push_str("<|channel>thought\n<channel|>");
            }
        }
        p
    }

    fn reusable_prefix_len(&self, rendered: &str) -> usize {
        // The non-thinking generation prompt ends with an empty thought-channel
        // prefill: it steers this turn but is never reproduced as history, so
        // the prefix cache must freeze before it. The `<|turn>model\n` opener
        // ahead of it does reappear as the next turn's assistant-turn head, so
        // it stays inside the reusable prefix.
        const THOUGHT_PREFILL: &str = "<|channel>thought\n<channel|>";
        rendered
            .strip_suffix(THOUGHT_PREFILL)
            .map_or(rendered.len(), |head| head.len())
    }

    fn stop_tokens(&self) -> &[i32] {
        &self.stop
    }

    fn reasoning_channel(&self) -> Option<crate::models::ReasoningChannel> {
        // <|channel> = 100, <channel|> = 101 in the gemma4 tokenizer; the
        // model opens the channel with a literal `thought` name line.
        Some(crate::models::ReasoningChannel {
            open: 100,
            close: 101,
            name: "thought",
        })
    }

    fn tool_call_markers(&self) -> Option<crate::models::ToolCallMarkers> {
        // <|tool_call> = 48, <tool_call|> = 49 in the gemma4 tokenizer
        // (the full tool token block is 46..=52).
        Some(crate::models::ToolCallMarkers {
            open: 48,
            close: Some(49),
        })
    }

    fn parse_tool_call(&self, span: &str) -> Result<(String, String)> {
        toolfmt::parse_call_span(span)
    }
}

/// Strip `<|channel>...<channel|>` segments from prior assistant content,
/// mirroring the chat template's strip_thinking macro: when a turn is
/// re-rendered into the prompt, the model must not see its own past
/// reasoning. Same split semantics as the Jinja original.
fn strip_thinking(text: &str) -> String {
    let mut result = String::new();
    for part in text.split("<channel|>") {
        match part.find("<|channel>") {
            Some(i) => result.push_str(&part[..i]),
            None => result.push_str(part),
        }
    }
    result.trim().to_string()
}

/// Format-path smoke for block-scaled quantization (mxfp4), run against a real
/// checkpoint. It deliberately does *not* run a correct forward for the model's
/// architecture: the point is that an mxfp4 weight loads through the
/// format-driven `QuantConfig`/`QLinear` and computes through the vendored
/// mlx-rs `mode` parameter -- proving the format path independently of whether
/// we can yet run that architecture end to end.
///
/// Ignored by default and gated on `OOSMLX_MXFP4_SMOKE_MODEL` (a local model
/// directory), because it needs real mxfp4 tensors on disk that CI lacks. Run:
///   cargo test -p oosmlx --features mlx -- --ignored mxfp4_format_path
#[cfg(test)]
mod format_smoke {
    use super::*;

    /// Reduce an array to its scalar sum on-device, then read back that single
    /// value -- lets us assert a large tensor is finite without copying it to
    /// the host. Single-axis reductions only, matching the ops this module
    /// already relies on elsewhere.
    fn scalar_sum(a: &Array) -> Result<f32> {
        let mut r = a.clone();
        while !r.shape().is_empty() {
            r = r.sum_axes(&[0], false)?;
        }
        // Widen before the host read; quantized ops may emit bf16.
        let r = r.as_dtype(Dtype::Float32)?;
        r.eval()?;
        Ok(r.item::<f32>())
    }

    #[test]
    #[ignore = "needs a local mxfp4 model dir in OOSMLX_MXFP4_SMOKE_MODEL"]
    fn mxfp4_format_path() -> Result<()> {
        let dir = match std::env::var("OOSMLX_MXFP4_SMOKE_MODEL") {
            Ok(d) => std::path::PathBuf::from(d),
            Err(_) => {
                eprintln!(
                    "skipping mxfp4_format_path: set OOSMLX_MXFP4_SMOKE_MODEL \
                     to a local mxfp4 model directory"
                );
                return Ok(());
            }
        };

        // The loader reads the format straight from config.json; a real mxfp4
        // checkpoint must resolve to mode=mxfp4, group 32, 4-bit.
        let qcfg = QuantConfig::load(&dir.join("config.json"))?;
        assert_eq!(qcfg.mode.as_deref(), Some("mxfp4"));
        assert_eq!((qcfg.group_size, qcfg.bits), (32, 4));

        let w = load_weights(&dir)?;

        // Pick one biasless, non-embedding quantized linear. Biaslessness is the
        // mxfp4 signature (affine ships `.biases`); the embedding table is
        // skipped only to keep the smoke small. Sorted for a deterministic pick.
        let mut prefixes: Vec<String> = w
            .keys()
            .filter_map(|k| k.strip_suffix(".scales").map(|p| p.to_string()))
            .filter(|p| {
                !p.contains("embed")
                    && w.contains_key(&format!("{p}.weight"))
                    && !w.contains_key(&format!("{p}.biases"))
            })
            .collect();
        prefixes.sort();
        let prefix = prefixes
            .first()
            .expect("no biasless quantized linear in checkpoint");

        // Build the QLinear exactly as the loader does, driven by QuantConfig:
        // detect biaslessness from the absent `.biases` tensor, carry the mode.
        let (group_size, bits) = qcfg.spec_for(prefix);
        let biases = match w.get(&format!("{prefix}.biases")) {
            Some(a) => Some(a.as_type::<f32>()?),
            None => None,
        };
        assert!(biases.is_none(), "mxfp4 linear unexpectedly carries biases");
        let qlin = QLinear {
            weight: w
                .get(&format!("{prefix}.weight"))
                .expect("weight present (filtered)")
                .clone(),
            scales: w
                .get(&format!("{prefix}.scales"))
                .expect("scales present (filtered)")
                // mxfp4 scales are uint8 (e8m0 block exponents); casting them
                // to f32 (the affine habit) makes quantized_matmul reject them.
                .clone(),
            biases,
            group_size,
            bits,
            mode: qcfg.mode.clone(),
        };

        // HF packs `weight` as [out, in/(32/bits)] u32; the unpacked input width
        // is what quantized_matmul expects on the activation side.
        let out = qlin.weight.shape()[0];
        let in_features = qlin.weight.shape()[1] * 32 / bits;

        // (1) quantized_matmul with mode=mxfp4 on the real packed weight.
        let x = Array::ones::<f32>(&[1, in_features])?;
        let y = qlin.forward(&x)?;
        assert_eq!(y.shape()[0], 1);
        assert_eq!(y.shape()[1], out);
        assert!(
            scalar_sum(&y)?.is_finite(),
            "mxfp4 matmul produced non-finite output"
        );

        // (2) dequantize roundtrip with mode=mxfp4: unpack the whole weight.
        let deq = ops::dequantize(
            &qlin.weight,
            &qlin.scales,
            qlin.biases.as_ref(),
            group_size,
            bits,
            qlin.mode.as_deref(),
        )?;
        assert_eq!(deq.shape()[0], out);
        assert_eq!(deq.shape()[1], in_features);
        let s = scalar_sum(&deq)?;
        assert!(
            s.is_finite() && s != 0.0,
            "dequantized mxfp4 weight is degenerate"
        );

        Ok(())
    }
}

/// Differential-debugging dump against a reference runtime (mlx_lm): one
/// prefill over a fixed token sequence, every per-layer hidden state saved to
/// a safetensors file for host-side comparison. Since the bf16 switch the
/// dump is in the compute dtype: run the reference in bf16 too (drop its
/// `set_dtype(float32)`) or expect bf16-rounding-sized deltas, not 1e-4s. A dump instead of asserts
/// because the interesting question is *where* the forward first diverges,
/// which one comparison over all checkpoints answers in a single run. Gated
/// on env (local 26B checkpoint, CI lacks it):
///   OOSMLX_NUMDIFF_MODEL  - local snapshot dir
///   OOSMLX_NUMDIFF_TOKENS - JSON file holding a flat array of token ids
///   OOSMLX_NUMDIFF_OUT    - output .safetensors path
///   cargo test -p oosmlx --features mlx -- --ignored dump_hidden_states
#[cfg(test)]
mod numdiff {
    use super::*;

    #[test]
    #[ignore = "needs OOSMLX_NUMDIFF_MODEL/_TOKENS/_OUT"]
    fn dump_hidden_states() -> Result<()> {
        let (dir, tokens_path, out) = match (
            std::env::var("OOSMLX_NUMDIFF_MODEL"),
            std::env::var("OOSMLX_NUMDIFF_TOKENS"),
            std::env::var("OOSMLX_NUMDIFF_OUT"),
        ) {
            (Ok(m), Ok(t), Ok(o)) => (std::path::PathBuf::from(m), t, o),
            _ => {
                eprintln!(
                    "skipping dump_hidden_states: set OOSMLX_NUMDIFF_MODEL, \
                     OOSMLX_NUMDIFF_TOKENS and OOSMLX_NUMDIFF_OUT"
                );
                return Ok(());
            }
        };

        let files = ModelFiles {
            tokenizer_json: dir.join("tokenizer.json"),
            config_json: dir.join("config.json"),
            dir,
        };
        let tokenizer = Tokenizer::from_file(&files.tokenizer_json)
            .map_err(|e| anyhow!("loading tokenizer: {e}"))?;
        let model = Gemma4Model::load(&files, &tokenizer)?;

        // The reference side tokenizes and writes this file; consuming its
        // ids verbatim keeps tokenizer differences out of the comparison.
        let ids: Vec<i32> = serde_json::from_str(&std::fs::read_to_string(&tokens_path)?)?;

        let mut dump: Vec<(String, Array)> = Vec::new();
        let mut cache = KvCache::new(model.cfg.num_hidden_layers);
        let mut h = model.embed(&ids)?;
        dump.push(("embed".to_string(), h.clone()));
        let masks = step_masks(
            0,
            ids.len() as i32,
            model.cfg.sliding_window as i32,
            !cache.is_linear(),
            COMPUTE,
        )?;
        for (i, (layer, slot)) in model
            .layers
            .iter()
            .zip(cache.slots_mut().iter_mut())
            .enumerate()
        {
            h = layer.forward(&h, &model.cfg, 0, &model.full_freqs, &masks, slot, &mut None)?;
            dump.push((format!("layer_{i:02}"), h.clone()));
        }
        let normed = fast::rms_norm(&h, &model.final_norm, model.cfg.rms_norm_eps)?;
        dump.push(("final_norm".to_string(), normed));
        dump.push(("logits".to_string(), model.project_logits(&h)?));

        for (_, a) in &dump {
            a.eval()?;
        }
        Array::save_safetensors(dump.iter().map(|(k, a)| (k.as_str(), a)), None, &out)?;
        eprintln!("dumped {} checkpoints to {out}", dump.len());
        Ok(())
    }
}
