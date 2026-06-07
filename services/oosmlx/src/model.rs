//! Gemma 3 (text) forward pass in MLX, via mlx-rs.
//!
//! Correctness-first implementation: weights are cast to f32 and there is no KV
//! cache yet (each step re-runs the full prefix). This keeps the arithmetic
//! easy to validate against the mlx-lm reference; the KV cache and bf16 compute
//! are the next milestone, and stay behind the same `forward_argmax` entry.
//!
//! Gemma 3 specifics implemented here, read from the real checkpoint:
//! embed scale ×√hidden, RMSNorm with (1+w), QK-norm over head_dim, per-layer
//! RoPE base (global vs local), GQA via fast SDPA, GeGLU MLP, and the sandwich
//! norms (post_attention / pre+post_feedforward).

use std::path::Path;

use anyhow::{anyhow, Result};
use mlx_rs::ops::indexing::IndexOp;
use mlx_rs::{fast, nn, ops, Array};

use crate::config::GemmaConfig;

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
}

impl Layer {
    fn attention(&self, x: &Array, cfg: &GemmaConfig) -> Result<Array> {
        let seq = x.shape()[0];
        let n = cfg.num_attention_heads as i32;
        let nkv = cfg.num_key_value_heads as i32;
        let hd = cfg.head_dim as i32;
        let eps = cfg.rms_norm_eps;

        // Project, split into heads, QK-norm over head_dim, RoPE, then GQA SDPA.
        let q = linear(x, &self.q_proj)?.reshape(&[1, seq, n, hd])?;
        let q = fast::rms_norm(&q, &self.q_norm, eps)?;
        let q = q.transpose_axes(&[0, 2, 1, 3])?;
        let q = fast::rope(&q, hd, false, Some(self.rope_base), 1.0, 0, None)?;

        let k = linear(x, &self.k_proj)?.reshape(&[1, seq, nkv, hd])?;
        let k = fast::rms_norm(&k, &self.k_norm, eps)?;
        let k = k.transpose_axes(&[0, 2, 1, 3])?;
        let k = fast::rope(&k, hd, false, Some(self.rope_base), 1.0, 0, None)?;

        let v = linear(x, &self.v_proj)?.reshape(&[1, seq, nkv, hd])?;
        let v = v.transpose_axes(&[0, 2, 1, 3])?;

        let mask = fast::ScaledDotProductAttentionMask::Causal;
        let o = fast::scaled_dot_product_attention(&q, &k, &v, cfg.attn_scale(), Some(mask))?;

        let o = o.transpose_axes(&[0, 2, 1, 3])?.reshape(&[seq, n * hd])?;
        linear(&o, &self.o_proj)
    }

    fn forward(&self, x: &Array, cfg: &GemmaConfig) -> Result<Array> {
        let eps = cfg.rms_norm_eps;

        // Attention block with sandwich norm: residual + post_attn(attn(input_ln(x))).
        let normed = fast::rms_norm(x, &self.input_ln, eps)?;
        let attn = self.attention(&normed, cfg)?;
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

pub struct GemmaModel {
    cfg: GemmaConfig,
    embed: Array,
    lm_head: Array,
    final_norm: Array,
    layers: Vec<Layer>,
}

impl GemmaModel {
    pub fn load(dir: &Path, cfg: &GemmaConfig) -> Result<Self> {
        let path = dir.join("model.safetensors");
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
            });
        }

        Ok(Self {
            cfg: cfg.clone(),
            embed,
            lm_head,
            final_norm,
            layers,
        })
    }

    /// Run the full prefix and return logits `[seq, vocab]`.
    fn forward(&self, tokens: &[i32]) -> Result<Array> {
        let seq = tokens.len() as i32;
        let ids = Array::from_slice(tokens, &[seq]);

        let scale = Array::from_slice(&[self.cfg.embed_scale()], &[1]);
        let mut h = self.embed.index(&ids).multiply(&scale)?;
        for layer in &self.layers {
            h = layer.forward(&h, &self.cfg)?;
        }
        let h = fast::rms_norm(&h, &self.final_norm, self.cfg.rms_norm_eps)?;
        Ok(h.matmul(&self.lm_head.transpose()?)?)
    }

    /// Greedy next-token id for the given prefix.
    pub fn forward_argmax(&self, tokens: &[i32]) -> Result<i32> {
        let logits = self.forward(tokens)?;
        let last = logits.index(tokens.len() as i32 - 1);
        let next = ops::indexing::argmax(&last, false)?;
        Ok(next.item::<u32>() as i32)
    }
}
