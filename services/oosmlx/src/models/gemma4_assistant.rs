//! Gemma 4 "assistant" drafter: Google's Multi-Token Prediction (MTP) head
//! for speculative decoding (`Gemma4AssistantForCausalLM`).
//!
//! This is not a standalone model. The drafter is a 4-layer dense Gemma 4
//! text stack whose attention has only Q and O projections: every layer
//! cross-attends over K/V *borrowed from the target model* -- the post-RoPE
//! keys/values of the target's last full-attention and last sliding-attention
//! layers. It keeps no KV cache of its own; the only recurrent state is the
//! previous hidden, fed back through `post_projection`. Each draft step
//! consumes `concat(target_embed(token), last_hidden)` projected by
//! `pre_projection`, with queries RoPE-rotated at one constant absolute
//! position for the whole draft block, attending bidirectionally over the
//! shared K/V.
//!
//! Ported against Blaizzy/mlx-vlm `speculative/drafters/gemma4_assistant/`
//! (the only complete implementation; cloned to ~/tmp/mlx-vlm). Only the tied
//! dense LM head (26B/31B drafters) is implemented; the centroid-routed
//! sparse head of the E2B/E4B drafters (`use_ordered_embeddings`) is not.
//!
//! Wiring into generation -- the target-side shared-K/V export and the
//! speculative round-loop in the engine -- comes separately; until then the
//! fake-target smoke test below is the only consumer, hence the module-wide
//! dead_code allowance.
#![allow(dead_code)]

use std::path::Path;

use anyhow::{anyhow, bail, Context, Result};
use mlx_rs::{fast, nn, Array};
use oos_infer::ModelFiles;
use serde::Deserialize;

use super::gemma4::{load_weights, proportional_freqs, LayerKind, QLinear, QuantConfig, RopeParameters};

/// Drafter text-tower parameters from config.json's `text_config`. Same field
/// names as Gemma 4 but a different subset: dense layers only (no MoE or
/// per-layer-input fields), and the KV head counts describe the *target's*
/// shared K/V geometry -- the drafter projects no K/V of its own.
#[derive(Debug, Clone, Deserialize)]
pub struct AssistantTextConfig {
    pub hidden_size: usize,
    pub num_hidden_layers: usize,
    pub num_attention_heads: usize,
    pub head_dim: usize,
    pub global_head_dim: usize,
    pub num_key_value_heads: usize,
    pub num_global_key_value_heads: usize,
    pub rms_norm_eps: f32,
    pub vocab_size: usize,
    pub sliding_window: usize,
    pub layer_types: Vec<String>,
    pub rope_parameters: RopeParameters,
}

/// Top-level drafter config. The backbone width is the *target's* hidden size
/// (26B: 2816); it sizes the pre/post projections and the per-step input.
pub struct Gemma4AssistantConfig {
    pub backbone_hidden_size: usize,
    pub text: AssistantTextConfig,
}

impl Gemma4AssistantConfig {
    /// pub(super): the speculative pairing reads the config alone to validate
    /// a candidate against the target before loading any weights.
    pub(super) fn load(path: &Path) -> Result<Self> {
        #[derive(Deserialize)]
        struct Root {
            backbone_hidden_size: usize,
            #[serde(default)]
            tie_word_embeddings: bool,
            #[serde(default)]
            use_ordered_embeddings: bool,
            text_config: AssistantTextConfig,
        }
        let text = std::fs::read_to_string(path)
            .with_context(|| format!("reading {}", path.display()))?;
        let root: Root = serde_json::from_str(&text)
            .with_context(|| format!("parsing {}", path.display()))?;
        // E2B/E4B drafters route logits through a centroid-based sparse head
        // instead of the tied embedding; that head is not implemented, so
        // fail at load instead of computing wrong logits.
        if root.use_ordered_embeddings || !root.tie_word_embeddings {
            bail!(
                "gemma4_assistant: only tied dense LM heads are supported \
                 (26B/31B drafters)"
            );
        }
        Ok(Self {
            backbone_hidden_size: root.backbone_hidden_size,
            text: root.text_config,
        })
    }

    fn kind(&self, layer_idx: usize) -> LayerKind {
        match self.text.layer_types.get(layer_idx).map(String::as_str) {
            Some("full_attention") => LayerKind::Full,
            _ => LayerKind::Sliding,
        }
    }
}

/// Borrowed target state for one draft block: the post-RoPE K/V of the
/// target's last full-attention and last sliding-attention layers, head-major
/// `[1, nkv, klen, head_dim]` exactly as the target's KV cache stores them.
pub struct SharedKv {
    pub full: (Array, Array),
    pub sliding: (Array, Array),
}

impl SharedKv {
    fn for_kind(&self, kind: LayerKind) -> &(Array, Array) {
        match kind {
            LayerKind::Full => &self.full,
            LayerKind::Sliding => &self.sliding,
        }
    }
}

const MASKED: f32 = -1e30;

/// Additive bidirectional mask `[1, 1, seq, klen]` for draft queries over the
/// target's K/V. Draft positions sit past the end of the cache and may attend
/// to *every* key -- the K/V is a fixed snapshot, so there is no causality to
/// enforce; only the sliding window restricts sliding layers to
/// `|qpos - kpos| < window`. Returns `None` when every pair is in range. Key
/// positions are absolute (`0..klen`) because our target cache concatenates
/// instead of rotating; mlx-vlm's local-window remapping is a rotating-cache
/// artifact we don't have.
fn bidirectional_mask(position: i32, seq: i32, klen: i32, window: Option<i32>) -> Option<Array> {
    let w = match window {
        None => return None,
        Some(w) => w,
    };
    if position + seq - 1 < w && klen - 1 - position < w {
        return None;
    }
    let mut data = vec![0.0f32; (seq * klen) as usize];
    for qi in 0..seq {
        let qpos = position + qi;
        for kj in 0..klen {
            let dist = qpos - kj;
            if !(-w < dist && dist < w) {
                data[(qi * klen + kj) as usize] = MASKED;
            }
        }
    }
    Some(Array::from_slice(&data, &[1, 1, seq, klen]))
}

/// One drafter attention block: Q/O projections and the Q norm only. K and V
/// never exist here -- they are the target's, handed in per layer kind.
struct DraftAttn {
    kind: LayerKind,
    head_dim: i32,
    n_heads: i32,
    rope_theta: f32,
    q_proj: QLinear,
    o_proj: QLinear,
    q_norm: Array,
}

impl DraftAttn {
    /// Same RoPE split as the target: precomputed proportional freqs on full
    /// layers, plain RoPE on sliding layers. `position` is the constant
    /// absolute query position for the whole draft block.
    fn apply_rope(&self, t: &Array, position: i32, full_freqs: &Array) -> Result<Array> {
        Ok(match self.kind {
            LayerKind::Full => {
                fast::rope(t, self.head_dim, false, None, 1.0, position, Some(full_freqs))?
            }
            LayerKind::Sliding => fast::rope(
                t,
                self.head_dim,
                false,
                Some(self.rope_theta),
                1.0,
                position,
                None,
            )?,
        })
    }

    fn forward(
        &self,
        x: &Array,
        eps: f32,
        window: Option<i32>,
        position: i32,
        full_freqs: &Array,
        kv: &(Array, Array),
    ) -> Result<Array> {
        let seq = x.shape()[0];
        let (n, hd) = (self.n_heads, self.head_dim);

        let q = self.q_proj.forward(x)?.reshape(&[1, seq, n, hd])?;
        let q = fast::rms_norm(&q, &self.q_norm, eps)?;
        let q = q.transpose_axes(&[0, 2, 1, 3])?;
        let q = self.apply_rope(&q, position, full_freqs)?;

        let (k, v) = kv;
        let klen = k.shape()[2];
        // Gemma 4 normalizes Q per head and runs SDPA at scale 1.0; the
        // borrowed K/V is already normed and RoPE'd by the target.
        let o = match bidirectional_mask(position, seq, klen, window) {
            Some(m) => {
                let mask = fast::ScaledDotProductAttentionMask::Array(&m);
                fast::scaled_dot_product_attention(&q, k, v, 1.0, Some(mask), None)?
            }
            None => fast::scaled_dot_product_attention(&q, k, v, 1.0, None, None)?,
        };
        let o = o.transpose_axes(&[0, 2, 1, 3])?.reshape(&[seq, n * hd])?;
        self.o_proj.forward(&o)
    }
}

/// One drafter decoder layer: sandwich-normed cross-attention plus a dense
/// GeGLU MLP (the drafter has no MoE), scaled by the per-layer scalar.
struct DraftLayer {
    input_ln: Array,
    post_attn_ln: Array,
    pre_ff_ln: Array,
    post_ff_ln: Array,
    layer_scalar: Array,
    attn: DraftAttn,
    mlp_gate: QLinear,
    mlp_up: QLinear,
    mlp_down: QLinear,
}

impl DraftLayer {
    fn forward(
        &self,
        x: &Array,
        eps: f32,
        window: Option<i32>,
        position: i32,
        full_freqs: &Array,
        kv: &(Array, Array),
    ) -> Result<Array> {
        let normed = fast::rms_norm(x, &self.input_ln, eps)?;
        let attn = self
            .attn
            .forward(&normed, eps, window, position, full_freqs, kv)?;
        let attn = fast::rms_norm(&attn, &self.post_attn_ln, eps)?;
        let h = x.add(&attn)?;

        let ff = fast::rms_norm(&h, &self.pre_ff_ln, eps)?;
        let gate = nn::gelu_approximate(&self.mlp_gate.forward(&ff)?)?;
        let up = self.mlp_up.forward(&ff)?;
        let ff = self.mlp_down.forward(&gate.multiply(&up)?)?;
        let ff = fast::rms_norm(&ff, &self.post_ff_ln, eps)?;
        let h = h.add(&ff)?;

        Ok(h.multiply(&self.layer_scalar)?)
    }
}

pub struct Gemma4AssistantModel {
    cfg: Gemma4AssistantConfig,
    /// The drafter's own quantized embedding table; with tied weights it is
    /// the LM head. Per-step *input* embeddings come from the target's table,
    /// not this one.
    embed: QLinear,
    final_norm: Array,
    full_freqs: Array,
    pre_projection: QLinear,
    post_projection: QLinear,
    layers: Vec<DraftLayer>,
}

impl Gemma4AssistantModel {
    pub fn load(files: &ModelFiles) -> Result<Self> {
        let cfg = Gemma4AssistantConfig::load(&files.config_json)
            .context("loading gemma4_assistant config")?;
        let w = load_weights(&files.dir)?;
        let qcfg = QuantConfig::load(&files.config_json)
            .context("loading gemma4_assistant quant config")?;

        // Plain f32 fetch for norms and scalars (checkpoint ships them bf16).
        let get = |name: &str| -> Result<Array> {
            w.get(name)
                .ok_or_else(|| anyhow!("missing tensor {name}"))
                .and_then(|a| Ok(a.as_type::<f32>()?))
        };

        // Unlike the multimodal 26B target, the drafter checkpoint is bare:
        // tensors live under `model.*` with the projections at top level.
        let embed = qcfg.qlinear(&w, "model.embed_tokens")?;
        let final_norm = get("model.norm.weight")?;
        let pre_projection = qcfg.qlinear(&w, "pre_projection")?;
        let post_projection = qcfg.qlinear(&w, "post_projection")?;

        let tc = &cfg.text;
        let mut layers = Vec::with_capacity(tc.num_hidden_layers);
        for i in 0..tc.num_hidden_layers {
            let p = format!("model.layers.{i}");
            let kind = cfg.kind(i);
            let (head_dim, rope) = match kind {
                LayerKind::Full => (tc.global_head_dim, &tc.rope_parameters.full_attention),
                LayerKind::Sliding => (tc.head_dim, &tc.rope_parameters.sliding_attention),
            };
            let attn = DraftAttn {
                kind,
                head_dim: head_dim as i32,
                n_heads: tc.num_attention_heads as i32,
                rope_theta: rope.rope_theta,
                q_proj: qcfg.qlinear(&w, &format!("{p}.self_attn.q_proj"))?,
                o_proj: qcfg.qlinear(&w, &format!("{p}.self_attn.o_proj"))?,
                q_norm: get(&format!("{p}.self_attn.q_norm.weight"))?,
            };
            layers.push(DraftLayer {
                input_ln: get(&format!("{p}.input_layernorm.weight"))?,
                post_attn_ln: get(&format!("{p}.post_attention_layernorm.weight"))?,
                pre_ff_ln: get(&format!("{p}.pre_feedforward_layernorm.weight"))?,
                post_ff_ln: get(&format!("{p}.post_feedforward_layernorm.weight"))?,
                layer_scalar: get(&format!("{p}.layer_scalar"))?,
                attn,
                mlp_gate: qcfg.qlinear(&w, &format!("{p}.mlp.gate_proj"))?,
                mlp_up: qcfg.qlinear(&w, &format!("{p}.mlp.up_proj"))?,
                mlp_down: qcfg.qlinear(&w, &format!("{p}.mlp.down_proj"))?,
            });
        }

        let full_freqs = {
            let r = &cfg.text.rope_parameters.full_attention;
            proportional_freqs(cfg.text.global_head_dim, r.partial_rotary_factor, r.rope_theta)
        };

        Ok(Self {
            cfg,
            embed,
            final_norm,
            full_freqs,
            pre_projection,
            post_projection,
            layers,
        })
    }

    /// One draft step.
    ///
    /// `inputs_embeds` is `[seq, 2 * backbone_hidden]` -- per position the
    /// target's token embedding concatenated with the previous step's hidden.
    /// `position` is the bonus token's absolute position, constant for every
    /// step of a draft block. Returns `(last_hidden [seq, backbone_hidden],
    /// logits [seq, vocab])`; `last_hidden` feeds the next step's input.
    pub fn forward(
        &self,
        inputs_embeds: &Array,
        kv: &SharedKv,
        position: i32,
    ) -> Result<(Array, Array)> {
        let eps = self.cfg.text.rms_norm_eps;
        let mut h = self.pre_projection.forward(inputs_embeds)?;
        for layer in &self.layers {
            let window = match layer.attn.kind {
                LayerKind::Sliding => Some(self.cfg.text.sliding_window as i32),
                LayerKind::Full => None,
            };
            h = layer.forward(
                &h,
                eps,
                window,
                position,
                &self.full_freqs,
                kv.for_kind(layer.attn.kind),
            )?;
        }
        let h = fast::rms_norm(&h, &self.final_norm, eps)?;
        let last_hidden = self.post_projection.forward(&h)?;
        // Tied dense LM head; the assistant config carries no logit softcap.
        let logits = self.embed.forward(&h)?;
        Ok((last_hidden, logits))
    }
}

/// Fake-target smoke for the drafter forward, modelled on mlx-vlm's
/// `parity_check.py`: real mxfp4 drafter weights, synthetic target state
/// (shared K/V, hidden, token embeddings). It cannot prove numerical parity
/// without a real target, but it proves the whole drafter stack -- mxfp4
/// loading, Q/O-only attention over borrowed K/V, the projection sandwich and
/// the multi-step recurrence -- runs shape-correct and finite end to end.
///
/// Ignored by default and gated on `OOSMLX_ASSISTANT_SMOKE_MODEL` (a local
/// model directory), because it needs the real drafter tensors on disk. Run:
///   cargo test -p oosmlx --features mlx -- --ignored assistant_fake_target
#[cfg(test)]
mod fake_target_smoke {
    use super::*;
    use mlx_rs::ops;

    /// Deterministic small pseudo-noise (LCG) -- keeps the smoke reproducible
    /// without pulling a random API into the test.
    fn pseudo(shape: &[i32], seed: u64) -> Array {
        let n: i32 = shape.iter().product();
        let mut s = seed ^ 0x9E3779B97F4A7C15;
        let mut v = Vec::with_capacity(n as usize);
        for _ in 0..n {
            s = s.wrapping_mul(6364136223846793005).wrapping_add(1442695040888963407);
            let u = ((s >> 40) & 0xFFFFFF) as f32 / 16_777_216.0;
            v.push((u - 0.5) * 0.04);
        }
        Array::from_slice(&v, shape)
    }

    /// Reduce to a scalar on-device and read back one value, to assert a
    /// large tensor is finite without copying it to the host.
    fn finite_sum(a: &Array) -> Result<f32> {
        let mut r = a.clone();
        while !r.shape().is_empty() {
            r = r.sum_axes(&[0], false)?;
        }
        r.eval()?;
        Ok(r.item::<f32>())
    }

    #[test]
    #[ignore = "needs a local gemma4_assistant model dir in OOSMLX_ASSISTANT_SMOKE_MODEL"]
    fn assistant_fake_target() -> Result<()> {
        let dir = match std::env::var("OOSMLX_ASSISTANT_SMOKE_MODEL") {
            Ok(d) => std::path::PathBuf::from(d),
            Err(_) => {
                eprintln!(
                    "skipping assistant_fake_target: set OOSMLX_ASSISTANT_SMOKE_MODEL \
                     to a local gemma4_assistant model directory"
                );
                return Ok(());
            }
        };
        let files = ModelFiles {
            dir: dir.clone(),
            tokenizer_json: dir.join("tokenizer.json"),
            config_json: dir.join("config.json"),
        };
        let model = Gemma4AssistantModel::load(&files)?;
        let tc = &model.cfg.text;
        let backbone = model.cfg.backbone_hidden_size as i32;

        // Synthetic target state: K/V shaped exactly as the target cache
        // hands them over ([1, nkv, klen, head_dim], full vs sliding
        // geometry), placed kv_len positions deep.
        let kv_len = 32;
        let full_shape = [
            1,
            tc.num_global_key_value_heads as i32,
            kv_len,
            tc.global_head_dim as i32,
        ];
        let sliding_shape = [1, tc.num_key_value_heads as i32, kv_len, tc.head_dim as i32];
        let kv = SharedKv {
            full: (pseudo(&full_shape, 1), pseudo(&full_shape, 2)),
            sliding: (pseudo(&sliding_shape, 3), pseudo(&sliding_shape, 4)),
        };
        let position = kv_len;

        // Multi-step recurrence: fake target token embed plus the previous
        // hidden, concatenated and projected -- exactly the draft-block shape.
        let mut h_prev = pseudo(&[1, backbone], 5);
        let mut tok: i32 = 42;
        for step in 0..3u64 {
            let tok_embed = pseudo(&[1, backbone], 100 + tok as u64);
            let inputs = ops::concatenate_axis(&[tok_embed, h_prev.clone()], 1)?;
            assert_eq!(inputs.shape()[1], 2 * backbone);

            let (hidden, logits) = model.forward(&inputs, &kv, position)?;
            assert_eq!(hidden.shape()[0], 1);
            assert_eq!(hidden.shape()[1], backbone);
            assert_eq!(logits.shape()[0], 1);
            assert_eq!(logits.shape()[1], tc.vocab_size as i32);
            assert!(
                finite_sum(&logits)?.is_finite(),
                "step {step}: non-finite logits"
            );

            let next = ops::indexing::argmax(&logits, false)?;
            next.eval()?;
            tok = next.item::<u32>() as i32;
            assert!(
                (tok as usize) < tc.vocab_size,
                "step {step}: token out of vocab"
            );
            h_prev = hidden;
        }

        // Sliding mask path: push the query position beyond the window so the
        // explicit bidirectional bias (not the None short-circuit) runs.
        let far = tc.sliding_window as i32 + 8;
        let inputs = ops::concatenate_axis(&[pseudo(&[1, backbone], 7), pseudo(&[1, backbone], 8)], 1)?;
        let (_, logits) = model.forward(&inputs, &kv, far)?;
        assert!(
            finite_sum(&logits)?.is_finite(),
            "masked path: non-finite logits"
        );

        Ok(())
    }
}

/// Real-target integration smoke for the target-side export hooks: prefill
/// the genuine 26B target, export shared K/V + pre-norm hidden + embeddings
/// through `Gemma4Model::{forward_hidden, project_logits, shared_kv, embed}`,
/// and run real draft steps on them. Everything the drafter consumes is
/// genuine target state -- only the verify/accept round-loop is absent, so
/// the drafted continuation is printed for eyeballing (--nocapture), not
/// asserted against a baseline.
///
/// Run:
///   OOSMLX_SPEC_TARGET_MODEL=<26b snapshot dir> \
///   OOSMLX_ASSISTANT_SMOKE_MODEL=<drafter snapshot dir> \
///     cargo test -p oosmlx --features mlx -- --ignored --nocapture real_target_drafting
#[cfg(test)]
mod real_target_smoke {
    use super::*;
    use anyhow::anyhow;
    use crate::models::gemma4::Gemma4Model;
    use crate::models::{KvCache, Model};
    use mlx_rs::ops;
    use mlx_rs::ops::indexing::IndexOp;
    use oos_infer::openai::ChatMessage;
    use tokenizers::Tokenizer;

    fn files_for(dir: std::path::PathBuf) -> ModelFiles {
        ModelFiles {
            tokenizer_json: dir.join("tokenizer.json"),
            config_json: dir.join("config.json"),
            dir,
        }
    }

    #[test]
    #[ignore = "needs OOSMLX_SPEC_TARGET_MODEL and OOSMLX_ASSISTANT_SMOKE_MODEL model dirs"]
    fn real_target_drafting() -> Result<()> {
        let (tdir, ddir) = match (
            std::env::var("OOSMLX_SPEC_TARGET_MODEL"),
            std::env::var("OOSMLX_ASSISTANT_SMOKE_MODEL"),
        ) {
            (Ok(t), Ok(d)) => (std::path::PathBuf::from(t), std::path::PathBuf::from(d)),
            _ => {
                eprintln!("skipping: OOSMLX_SPEC_TARGET_MODEL / OOSMLX_ASSISTANT_SMOKE_MODEL unset");
                return Ok(());
            }
        };

        let tfiles = files_for(tdir);
        let tokenizer = Tokenizer::from_file(&tfiles.tokenizer_json)
            .map_err(|e| anyhow!("loading tokenizer: {e}"))?;
        let target = Gemma4Model::load(&tfiles, &tokenizer)?;
        let drafter = Gemma4AssistantModel::load(&files_for(ddir))?;

        // Target prefill through the export hooks (Model::forward_logits
        // discards the hidden states the drafter needs).
        let prompt = target.render_prompt(&[ChatMessage {
            role: "user".into(),
            content: "Was ist die Hauptstadt von Frankreich?".into(),
        }]);
        let enc = tokenizer
            .encode(prompt, false)
            .map_err(|e| anyhow!("encode: {e}"))?;
        let ids: Vec<i32> = enc.get_ids().iter().map(|&t| t as i32).collect();
        let klen = ids.len() as i32;

        let mut cache = KvCache::new(target.num_layers());
        let hidden = target.forward_hidden(&ids, &mut cache)?;
        // Pre-norm hidden in backbone width (26B: 2816).
        assert_eq!(hidden.shape().to_vec(), vec![klen, 2816]);

        // Shared K/V geometry of the 26B: full layers carry 2 KV heads at
        // head_dim 512, sliding layers 8 at 256; klen positions each.
        let shared = target.shared_kv(&cache)?;
        assert_eq!(shared.full.0.shape().to_vec(), vec![1, 2, klen, 512]);
        assert_eq!(shared.full.1.shape().to_vec(), vec![1, 2, klen, 512]);
        assert_eq!(shared.sliding.0.shape().to_vec(), vec![1, 8, klen, 256]);
        assert_eq!(shared.sliding.1.shape().to_vec(), vec![1, 8, klen, 256]);

        // Block seed: the target's greedy bonus token plus the hidden at the
        // last prompt position.
        let logits = target.project_logits(&hidden)?;
        let next = ops::indexing::argmax(&logits.index(klen - 1), false)?;
        next.eval()?;
        let bonus = next.item::<u32>();
        let mut h_prev = hidden.index(klen - 1).reshape(&[1, 2816])?;
        let mut tok = bonus as i32;
        let mut drafted = vec![bonus];

        // Draft block: constant absolute position klen, three steps.
        for step in 0..3u32 {
            let emb = target.embed(&[tok])?; // [1, 2816], embed_scale applied
            let inputs = ops::concatenate_axis(&[emb, h_prev.clone()], 1)?;
            let (hid, logits) = drafter.forward(&inputs, &shared, klen)?;
            assert_eq!(hid.shape().to_vec(), vec![1, 2816]);
            let next = ops::indexing::argmax(&logits, false)?;
            next.eval()?;
            let id = next.item::<u32>();
            assert!(
                (id as usize) < drafter.cfg.text.vocab_size,
                "step {step}: token out of vocab"
            );
            drafted.push(id);
            tok = id as i32;
            h_prev = hid;
        }

        let text = tokenizer
            .decode(&drafted, false)
            .map_err(|e| anyhow!("decode: {e}"))?;
        eprintln!("target bonus + drafted continuation: {text:?}");
        Ok(())
    }
}
