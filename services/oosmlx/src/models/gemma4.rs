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
use mlx_rs::{fast, nn, ops, Array};
use oos_infer::openai::ChatMessage;
use oos_infer::ModelFiles;
use serde::Deserialize;
use std::path::Path;
use tokenizers::Tokenizer;

use super::{KvCache, Model};

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
        Some(serde_json::Value::Array(a)) => {
            a.iter().filter_map(|v| v.as_u64().map(|u| u as u32)).collect()
        }
        Some(serde_json::Value::Number(n)) => {
            n.as_u64().map(|u| vec![u as u32]).unwrap_or_else(|| vec![1])
        }
        _ => vec![1],
    }
}

/// Whether a layer is local sliding-window or periodic global full attention.
/// They differ in head_dim, KV-head count, K-eq-V, and RoPE.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
enum LayerKind {
    Sliding,
    Full,
}

impl Gemma4Config {
    /// Parse `text_config` out of the multimodal config.json wrapper.
    fn load(path: &Path) -> Result<Self> {
        let text = std::fs::read_to_string(path)
            .with_context(|| format!("reading {}", path.display()))?;
        let root: serde_json::Value = serde_json::from_str(&text)
            .with_context(|| format!("parsing {}", path.display()))?;
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

/// An affine-quantized linear weight, stored exactly as the checkpoint packs
/// it: `weight` is bit-packed u32 (never cast), `scales`/`biases` carry the
/// per-group affine params. HF lays weights out as `[out, in]`, so the matmul
/// transposes.
struct QLinear {
    weight: Array,
    scales: Array,
    biases: Array,
    group_size: i32,
    bits: i32,
}

impl QLinear {
    fn forward(&self, x: &Array) -> Result<Array> {
        Ok(ops::quantized_matmul(
            x,
            &self.weight,
            &self.scales,
            Some(&self.biases),
            true,
            self.group_size,
            self.bits,
            None,
        )?)
    }
}

/// Affine group size is uniform (64); only the bit width varies per path:
/// the shared MLP projections and the router are 8-bit, everything else 4-bit.
fn bits_for(path: &str) -> i32 {
    let eight = [
        ".mlp.gate_proj",
        ".mlp.up_proj",
        ".mlp.down_proj",
        ".router.proj",
    ];
    if eight.iter().any(|s| path.contains(s)) {
        8
    } else {
        4
    }
}

/// Load every shard listed in `model.safetensors.index.json` and merge into one
/// tensor map. The 26B is sharded; a single-file `model.safetensors` is also
/// accepted as a fallback.
fn load_weights(dir: &Path) -> Result<HashMap<String, Array>> {
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
    let text = std::fs::read_to_string(&index)
        .with_context(|| format!("reading {}", index.display()))?;
    let idx: Index = serde_json::from_str(&text)
        .with_context(|| format!("parsing {}", index.display()))?;

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
fn proportional_freqs(head_dim: usize, partial_rotary_factor: f32, theta: f32) -> Array {
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

        // Plain f32 fetch (norms, scalars). Scales/biases are also pulled as
        // f32 so the quantized matmuls compute against f32 activations; only
        // the bit-packed `weight` stays in its native u32 layout.
        let get = |name: &str| -> Result<Array> {
            w.get(name)
                .ok_or_else(|| anyhow!("missing tensor {name}"))
                .and_then(|a| Ok(a.as_type::<f32>()?))
        };
        let raw = |name: &str| -> Result<Array> {
            w.get(name)
                .cloned()
                .ok_or_else(|| anyhow!("missing tensor {name}"))
        };
        let qlinear = |prefix: &str| -> Result<QLinear> {
            Ok(QLinear {
                weight: raw(&format!("{prefix}.weight"))?,
                scales: get(&format!("{prefix}.scales"))?,
                biases: get(&format!("{prefix}.biases"))?,
                group_size: 64,
                bits: bits_for(prefix),
            })
        };

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

/// Additive attention mask `[1, 1, seq, klen]`; identical builder to Gemma 3,
/// covering both global (window `None`) and local sliding layers.
fn attention_mask(offset: i32, seq: i32, klen: i32, window: Option<i32>) -> Array {
    const MASKED: f32 = -1e30;
    let mut data = vec![0.0f32; (seq * klen) as usize];
    for qi in 0..seq {
        let qpos = offset + qi;
        for kj in 0..klen {
            let causal = kj <= qpos;
            let in_window = window.map_or(true, |w| qpos - kj < w);
            if !(causal && in_window) {
                data[(qi * klen + kj) as usize] = MASKED;
            }
        }
    }
    Array::from_slice(&data, &[1, 1, seq, klen])
}

/// RMSNorm with a unit (no-scale) weight -- Gemma 4's V normalization.
fn rms_no_scale(x: &Array, eps: f32) -> Result<Array> {
    let dim = *x.shape().last().unwrap();
    let ones = Array::ones::<f32>(&[dim])?;
    Ok(fast::rms_norm(x, &ones, eps)?)
}

/// One expert projection over gathered top-k indices (SwitchGLU via gather_qmm).
fn gather(ql: &QLinear, x: &Array, idx: &Array) -> Result<Array> {
    Ok(ops::gather_qmm(
        x,
        &ql.weight,
        &ql.scales,
        Some(&ql.biases),
        None,
        Some(idx),
        true,
        ql.group_size,
        ql.bits,
        None,
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
        sliding_window: Option<i32>,
        offset: i32,
        full_freqs: &Array,
        cache: &mut Option<(Array, Array)>,
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

        let (k, v) = match cache.take() {
            Some((pk, pv)) => (
                ops::concatenate_axis(&[pk, k], 2)?,
                ops::concatenate_axis(&[pv, v], 2)?,
            ),
            None => (k, v),
        };
        *cache = Some((k.clone(), v.clone()));

        let klen = k.shape()[2];
        let mask = attention_mask(offset, seq, klen, sliding_window);
        let mask = fast::ScaledDotProductAttentionMask::Array(&mask);
        // Gemma 4 normalizes Q/K per head and runs SDPA at scale 1.0.
        let o = fast::scaled_dot_product_attention(&q, &k, &v, 1.0, Some(mask), None)?;
        let o = o.transpose_axes(&[0, 2, 1, 3])?.reshape(&[seq, n * hd])?;
        self.o_proj.forward(&o)
    }
}

impl Moe {
    /// Routed sparse FFN: route to top-k experts, run SwitchGLU over them, and
    /// combine by the (renormalized, per-expert-scaled) routing weights.
    fn forward(&self, x: &Array, num_experts: i32, top_k: i32, hidden: i32, eps: f32) -> Result<Array> {
        let seq = x.shape()[0];

        // Router: rms_norm(x, scale * hidden^-0.5) -> proj -> top-k -> softmax.
        let root = Array::from_slice(&[(hidden as f32).powf(-0.5)], &[1]);
        let rw = self.router_scale.multiply(&root)?;
        let xr = fast::rms_norm(x, &rw, eps)?;
        let scores = self.router_proj.forward(&xr)?; // [seq, E]

        let part = ops::argpartition_axis(&scores, -top_k, -1)?; // [seq, E]
        let last = Array::arange::<_, i32>(num_experts - top_k, num_experts, None)?;
        let idx = ops::indexing::take_axis(&part, &last, -1)?; // [seq, k] expert ids
        let sel = ops::indexing::take_along_axis(&scores, &idx, -1)?; // [seq, k]
        let weights = ops::softmax_axis(&sel, -1, None)?;
        let per_expert = ops::indexing::take(&self.per_expert_scale, &idx)?; // [seq, k]
        let weights = weights.multiply(&per_expert)?; // [seq, k]

        // SwitchGLU over the selected experts (sorted_indices=false: always
        // correct, just unsorted gather access).
        let xe = ops::expand_dims_axes(x, &[-2, -3])?; // [seq, 1, 1, hidden]
        let up = gather(&self.up, &xe, &idx)?;
        let gate = gather(&self.gate, &xe, &idx)?;
        let act = nn::gelu_approximate(&gate)?.multiply(&up)?;
        let down = gather(&self.down, &act, &idx)?;
        let y = down.reshape(&[seq, top_k, hidden])?; // [seq, k, hidden]

        // Weighted sum over the k experts.
        let w = ops::expand_dims_axes(&weights, &[-1])?; // [seq, k, 1]
        Ok(w.multiply(&y)?.sum_axes(&[-2], false)?) // [seq, hidden]
    }
}

impl Layer {
    fn forward(
        &self,
        x: &Array,
        cfg: &Gemma4Config,
        offset: i32,
        full_freqs: &Array,
        cache: &mut Option<(Array, Array)>,
    ) -> Result<Array> {
        let eps = cfg.rms_norm_eps;
        let window = match self.attn.kind {
            LayerKind::Sliding => Some(cfg.sliding_window as i32),
            LayerKind::Full => None,
        };

        // Attention with sandwich norm.
        let normed = fast::rms_norm(x, &self.input_ln, eps)?;
        let attn = self.attn.forward(&normed, eps, window, offset, full_freqs, cache)?;
        let attn = fast::rms_norm(&attn, &self.post_attn_ln, eps)?;
        let h = x.add(&attn)?;

        // Shared dense MLP (h1) plus routed experts (h2), each with its own
        // pre/post feedforward norms; summed, then a final feedforward norm.
        let h1 = fast::rms_norm(&h, &self.pre_ff_ln, eps)?;
        let gate = nn::gelu_approximate(&self.mlp_gate.forward(&h1)?)?;
        let up = self.mlp_up.forward(&h1)?;
        let h1 = self.mlp_down.forward(&gate.multiply(&up)?)?;
        let h1 = fast::rms_norm(&h1, &self.post_ff_ln1, eps)?;

        let h2 = fast::rms_norm(&h, &self.pre_ff_ln2, eps)?;
        let h2 = self.moe.forward(
            &h2,
            cfg.num_experts as i32,
            cfg.top_k_experts as i32,
            cfg.hidden_size as i32,
            eps,
        )?;
        let h2 = fast::rms_norm(&h2, &self.post_ff_ln2, eps)?;

        let ff = h1.add(&h2)?;
        let ff = fast::rms_norm(&ff, &self.post_ff_ln, eps)?;
        let h = h.add(&ff)?;

        Ok(h.multiply(&self.layer_scalar)?)
    }
}

impl Gemma4Model {
    /// Look up token embeddings from the quantized embedding table: gather the
    /// packed rows for `tokens` and dequantize just those.
    fn embed(&self, tokens: &[i32]) -> Result<Array> {
        use mlx_rs::ops::indexing::IndexOp;
        let ids = Array::from_slice(tokens, &[tokens.len() as i32]);
        let w = self.embed.weight.index(&ids);
        let s = self.embed.scales.index(&ids);
        let b = self.embed.biases.index(&ids);
        let h = ops::dequantize(&w, &s, Some(&b), self.embed.group_size, self.embed.bits, None)?;
        let scale = Array::from_slice(&[self.cfg.embed_scale()], &[1]);
        Ok(h.multiply(&scale)?)
    }

    fn forward(&self, tokens: &[i32], cache: &mut KvCache) -> Result<Array> {
        let mut h = self.embed(tokens)?;
        let offset = cache.offset() as i32;
        for (layer, slot) in self.layers.iter().zip(cache.slots_mut().iter_mut()) {
            h = layer.forward(&h, &self.cfg, offset, &self.full_freqs, slot)?;
        }
        cache.advance(tokens.len());

        let h = fast::rms_norm(&h, &self.final_norm, self.cfg.rms_norm_eps)?;
        // Tied embeddings: logits = h @ embed^T via the quantized embedding.
        let logits = self.embed.forward(&h)?;
        // Final logit softcap: tanh(logits / cap) * cap.
        let cap = self.cfg.final_logit_softcapping;
        let cap_a = Array::from_slice(&[cap], &[1]);
        let logits = ops::tanh(&logits.divide(&cap_a)?)?.multiply(&cap_a)?;
        Ok(logits)
    }
}

impl Model for Gemma4Model {
    fn num_layers(&self) -> usize {
        self.cfg.num_hidden_layers
    }

    fn forward_logits(&self, tokens: &[i32], cache: &mut KvCache) -> Result<Array> {
        use mlx_rs::ops::indexing::IndexOp;
        let logits = self.forward(tokens, cache)?;
        Ok(logits.index(tokens.len() as i32 - 1))
    }

    fn render_prompt(&self, messages: &[ChatMessage]) -> String {
        let user = messages
            .iter()
            .rev()
            .find(|m| m.role == "user")
            .map(|m| m.content.as_str())
            .unwrap_or("");
        format!("<bos><start_of_turn>user\n{user}<end_of_turn>\n<start_of_turn>model\n")
    }

    fn stop_tokens(&self) -> &[i32] {
        &self.stop
    }
}
