//! Gemma 3 (text) architecture configuration, parsed from the model's
//! config.json. These are exactly the fields the forward pass needs; the
//! shapes/values were read from the real mlx-community/gemma-3-1b-it-bf16
//! checkpoint (26 layers, hidden 1152, 4 query heads / 1 kv head, head_dim 256,
//! QK-norm, sandwich norms, 5:1 local/global sliding-window attention).

use std::path::Path;

use anyhow::{Context, Result};
use serde::Deserialize;

#[derive(Debug, Clone, Deserialize)]
pub struct GemmaConfig {
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

impl GemmaConfig {
    pub fn load(path: &Path) -> Result<Self> {
        let text = std::fs::read_to_string(path)
            .with_context(|| format!("reading {}", path.display()))?;
        serde_json::from_str(&text).with_context(|| format!("parsing {}", path.display()))
    }

    /// Attention softmax scale. Gemma scales by `query_pre_attn_scalar`, so the
    /// score scale is its inverse square root (1/16 for the 1B).
    pub fn attn_scale(&self) -> f32 {
        1.0 / self.query_pre_attn_scalar.sqrt()
    }

    /// Token embeddings are multiplied by sqrt(hidden_size) before layer 0.
    pub fn embed_scale(&self) -> f32 {
        (self.hidden_size as f32).sqrt()
    }

    /// Gemma 3 alternates local sliding-window layers with periodic global
    /// (full-attention) layers: every `sliding_window_pattern`-th layer is
    /// global. (For the 1B: layers 5, 11, 17, 23.)
    pub fn is_global_layer(&self, layer_idx: usize) -> bool {
        (layer_idx + 1) % self.sliding_window_pattern == 0
    }

    /// Global and local layers use different RoPE base frequencies.
    pub fn rope_base(&self, layer_idx: usize) -> f32 {
        if self.is_global_layer(layer_idx) {
            self.rope_theta
        } else {
            self.rope_local_base_freq
        }
    }
}
