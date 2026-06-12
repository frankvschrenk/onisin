//! Gemma 3 (text) model family: config, weights and forward pass in MLX.
//!
//! Correctness-first: weights are cast to f32. A per-layer KV cache makes
//! decoding incremental -- the prompt runs once (prefill), then each step
//! processes only the new token and attends over the stored history. bf16
//! compute is a later milestone behind the same `Model` impl.
//!
//! Gemma 3 specifics, read from the real checkpoint: embed scale ×√hidden,
//! RMSNorm with (1+w), QK-norm over head_dim, per-layer RoPE base (global vs
//! local), GQA via fast SDPA, GeGLU MLP, sandwich norms, and the 5:1
//! local/global sliding-window attention pattern.

use anyhow::{anyhow, Context, Result};
use mlx_rs::ops::indexing::IndexOp;
use mlx_rs::{fast, nn, Array};
use oos_infer::openai::ChatMessage;
use oos_infer::ModelFiles;
use serde::Deserialize;
use std::path::Path;
use tokenizers::Tokenizer;

use super::{step_masks, KvCache, KvSlot, MaskKind, Model, StepMasks};

/// Gemma 3 architecture parameters, parsed from config.json.
///
/// A few fields (the softcapping values, vocab/intermediate sizes) aren't read
/// by the current forward pass; they are kept to mirror the checkpoint config
/// and for validation and other Gemma variants later.
#[allow(dead_code)]
#[derive(Debug, Clone, Deserialize)]
pub struct Gemma3Config {
    pub hidden_size: usize,
    pub intermediate_size: usize,
    pub num_hidden_layers: usize,
    pub num_attention_heads: usize,
    pub num_key_value_heads: usize,
    pub head_dim: usize,
    pub vocab_size: usize,
    pub rms_norm_eps: f32,
    pub rope_theta: f32,
    pub rope_local_base_freq: f32,
    pub sliding_window: usize,
    pub sliding_window_pattern: usize,
    pub query_pre_attn_scalar: f32,
    #[serde(default)]
    pub attn_logit_softcapping: Option<f32>,
    #[serde(default)]
    pub final_logit_softcapping: Option<f32>,
    #[serde(default = "default_eos")]
    pub eos_token_id: u32,
}

fn default_eos() -> u32 {
    1
}

impl Gemma3Config {
    fn load(path: &Path) -> Result<Self> {
        let text =
            std::fs::read_to_string(path).with_context(|| format!("reading {}", path.display()))?;
        serde_json::from_str(&text).with_context(|| format!("parsing {}", path.display()))
    }

    /// Attention softmax scale. Gemma scales by `query_pre_attn_scalar`, so the
    /// score scale is its inverse square root (1/16 for the 1B).
    fn attn_scale(&self) -> f32 {
        1.0 / self.query_pre_attn_scalar.sqrt()
    }

    /// Token embeddings are multiplied by sqrt(hidden_size) before layer 0.
    fn embed_scale(&self) -> f32 {
        (self.hidden_size as f32).sqrt()
    }

    /// Gemma 3 alternates local sliding-window layers with periodic global
    /// (full-attention) layers: every `sliding_window_pattern`-th layer is
    /// global. (For the 1B: layers 5, 11, 17, 23.)
    fn is_global_layer(&self, layer_idx: usize) -> bool {
        (layer_idx + 1) % self.sliding_window_pattern == 0
    }

    /// Global and local layers use different RoPE base frequencies.
    fn rope_base(&self, layer_idx: usize) -> f32 {
        if self.is_global_layer(layer_idx) {
            self.rope_theta
        } else {
            self.rope_local_base_freq
        }
    }
}

/// `y = x @ W^T` for an HF-style weight stored as `[out, in]`.
fn linear(x: &Array, w: &Array) -> Result<Array> {
    Ok(x.matmul(&w.transpose()?)?)
}

/// One transformer block's weights (all f32).
struct Layer {
    input_ln: Array,
    post_attn_ln: Array,
    pre_ff_ln: Array,
    post_ff_ln: Array,
    q_proj: Array,
    k_proj: Array,
    v_proj: Array,
    o_proj: Array,
    q_norm: Array,
    k_norm: Array,
    gate: Array,
    up: Array,
    down: Array,
    rope_base: f32,
    /// `Some(window)` for local sliding-window layers, `None` for global ones.
    sliding_window: Option<i32>,
}

impl Layer {
    fn attention(
        &self,
        x: &Array,
        cfg: &Gemma3Config,
        offset: i32,
        mask: &MaskKind,
        slot: &mut KvSlot,
    ) -> Result<Array> {
        let seq = x.shape()[0];
        let n = cfg.num_attention_heads as i32;
        let nkv = cfg.num_key_value_heads as i32;
        let hd = cfg.head_dim as i32;
        let eps = cfg.rms_norm_eps;

        // Project, split into heads, QK-norm over head_dim, then RoPE at the
        // absolute position `offset` so cached and fresh tokens share a frame.
        let q = linear(x, &self.q_proj)?.reshape(&[1, seq, n, hd])?;
        let q = fast::rms_norm(&q, &self.q_norm, eps)?;
        let q = q.transpose_axes(&[0, 2, 1, 3])?;
        let q = fast::rope(&q, hd, false, Some(self.rope_base), 1.0, offset, None)?;

        let k = linear(x, &self.k_proj)?.reshape(&[1, seq, nkv, hd])?;
        let k = fast::rms_norm(&k, &self.k_norm, eps)?;
        let k = k.transpose_axes(&[0, 2, 1, 3])?;
        let k = fast::rope(&k, hd, false, Some(self.rope_base), 1.0, offset, None)?;

        let v = linear(x, &self.v_proj)?.reshape(&[1, seq, nkv, hd])?;
        let v = v.transpose_axes(&[0, 2, 1, 3])?;

        // The slot persists this step's K/V in place (post-RoPE, head-major
        // [1, nkv, past, hd]; ring-rotated on local layers) and hands back
        // the K/V to attend over; the mask built in `step_masks` matches the
        // slot's retention geometry.
        let (k, v) = slot.update(&k, &v, self.sliding_window)?;

        let sdpa_mask = match mask {
            MaskKind::None => None,
            MaskKind::Causal => Some(fast::ScaledDotProductAttentionMask::Causal),
            MaskKind::Mask(m) => Some(fast::ScaledDotProductAttentionMask::Array(m)),
        };
        let o = fast::scaled_dot_product_attention(&q, &k, &v, cfg.attn_scale(), sdpa_mask, None)?;

        let o = o.transpose_axes(&[0, 2, 1, 3])?.reshape(&[seq, n * hd])?;
        linear(&o, &self.o_proj)
    }

    fn forward(
        &self,
        x: &Array,
        cfg: &Gemma3Config,
        offset: i32,
        masks: &StepMasks,
        slot: &mut KvSlot,
    ) -> Result<Array> {
        let eps = cfg.rms_norm_eps;
        let mask = if self.sliding_window.is_some() {
            &masks.sliding
        } else {
            &masks.full
        };

        // Attention block with sandwich norm: residual + post_attn(attn(input_ln(x))).
        let normed = fast::rms_norm(x, &self.input_ln, eps)?;
        let attn = self.attention(&normed, cfg, offset, mask, slot)?;
        let attn = fast::rms_norm(&attn, &self.post_attn_ln, eps)?;
        let h = x.add(&attn)?;

        // MLP block: residual + post_ff(GeGLU(pre_ff(h))).
        let normed = fast::rms_norm(&h, &self.pre_ff_ln, eps)?;
        let gate = nn::gelu_approximate(&linear(&normed, &self.gate)?)?;
        let up = linear(&normed, &self.up)?;
        let mlp = linear(&gate.multiply(&up)?, &self.down)?;
        let mlp = fast::rms_norm(&mlp, &self.post_ff_ln, eps)?;
        Ok(h.add(&mlp)?)
    }
}

pub struct Gemma3Model {
    cfg: Gemma3Config,
    embed: Array,
    lm_head: Array,
    final_norm: Array,
    layers: Vec<Layer>,
    stop: Vec<i32>,
}

impl Gemma3Model {
    pub fn load(files: &ModelFiles, tokenizer: &Tokenizer) -> Result<Self> {
        let cfg = Gemma3Config::load(&files.config_json).context("loading gemma3 config")?;

        let path = files.dir.join("model.safetensors");
        let weights = Array::load_safetensors(&path)
            .map_err(|e| anyhow!("loading {}: {e}", path.display()))?;

        // Every tensor is cast to f32 for this correctness-first pass.
        let get = |name: &str| -> Result<Array> {
            let a = weights
                .get(name)
                .ok_or_else(|| anyhow!("missing tensor {name}"))?;
            Ok(a.as_type::<f32>()?)
        };
        // Gemma RMSNorm scales by (1 + weight); fold the +1 in once at load.
        let one = Array::from_slice(&[1.0f32], &[1]);
        let norm = |name: &str| -> Result<Array> { Ok(get(name)?.add(&one)?) };

        let embed = get("model.embed_tokens.weight")?;
        let lm_head = get("lm_head.weight")?;
        let final_norm = norm("model.norm.weight")?;

        let mut layers = Vec::with_capacity(cfg.num_hidden_layers);
        for i in 0..cfg.num_hidden_layers {
            let p = format!("model.layers.{i}");
            layers.push(Layer {
                input_ln: norm(&format!("{p}.input_layernorm.weight"))?,
                post_attn_ln: norm(&format!("{p}.post_attention_layernorm.weight"))?,
                pre_ff_ln: norm(&format!("{p}.pre_feedforward_layernorm.weight"))?,
                post_ff_ln: norm(&format!("{p}.post_feedforward_layernorm.weight"))?,
                q_proj: get(&format!("{p}.self_attn.q_proj.weight"))?,
                k_proj: get(&format!("{p}.self_attn.k_proj.weight"))?,
                v_proj: get(&format!("{p}.self_attn.v_proj.weight"))?,
                o_proj: get(&format!("{p}.self_attn.o_proj.weight"))?,
                q_norm: norm(&format!("{p}.self_attn.q_norm.weight"))?,
                k_norm: norm(&format!("{p}.self_attn.k_norm.weight"))?,
                gate: get(&format!("{p}.mlp.gate_proj.weight"))?,
                up: get(&format!("{p}.mlp.up_proj.weight"))?,
                down: get(&format!("{p}.mlp.down_proj.weight"))?,
                rope_base: cfg.rope_base(i),
                sliding_window: if cfg.is_global_layer(i) {
                    None
                } else {
                    Some(cfg.sliding_window as i32)
                },
            });
        }

        // Stop generation on eos and Gemma's end-of-turn marker.
        let mut stop = vec![cfg.eos_token_id as i32];
        if let Some(id) = tokenizer.token_to_id("<end_of_turn>") {
            stop.push(id as i32);
        }

        Ok(Self {
            cfg,
            embed,
            lm_head,
            final_norm,
            layers,
            stop,
        })
    }

    /// Run `tokens` through the model, updating `cache`, returning logits
    /// `[tokens, vocab]`.
    fn forward(&self, tokens: &[i32], cache: &mut KvCache) -> Result<Array> {
        let seq = tokens.len() as i32;
        let ids = Array::from_slice(tokens, &[seq]);

        let scale = Array::from_slice(&[self.cfg.embed_scale()], &[1]);
        let mut h = self.embed.index(&ids).multiply(&scale)?;

        let offset = cache.offset() as i32;
        let masks = step_masks(
            offset,
            seq,
            self.cfg.sliding_window as i32,
            !cache.is_linear(),
        )?;
        for (layer, slot) in self.layers.iter().zip(cache.slots_mut().iter_mut()) {
            h = layer.forward(&h, &self.cfg, offset, &masks, slot)?;
        }
        cache.advance(tokens.len());

        let h = fast::rms_norm(&h, &self.final_norm, self.cfg.rms_norm_eps)?;
        Ok(h.matmul(&self.lm_head.transpose()?)?)
    }
}

impl Model for Gemma3Model {
    fn num_layers(&self) -> usize {
        self.cfg.num_hidden_layers
    }

    fn forward_logits(&self, tokens: &[i32], cache: &mut KvCache) -> Result<Array> {
        let logits = self.forward(tokens, cache)?;
        Ok(logits.index(tokens.len() as i32 - 1))
    }

    /// Gemma chat format for the last user turn. The turn markers are added
    /// tokens in Gemma's tokenizer, so we encode them literally. gemma3 has
    /// no thinking channel, so the flag is ignored.
    fn render_prompt(
        &self,
        messages: &[ChatMessage],
        _thinking: bool,
        _tools: &[oos_infer::openai::Tool],
    ) -> String {
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
