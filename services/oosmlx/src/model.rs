//! Gemma 3 (text) forward pass in MLX, via mlx-rs.
//!
//! Correctness-first implementation: weights are cast to f32. A per-layer KV
//! cache makes decoding incremental -- the prompt is run once (prefill), then
//! each step processes only the new token and attends over the stored history,
//! instead of re-running the full prefix every token. bf16 compute is the next
//! milestone and stays behind the same `forward_argmax` entry.
//!
//! Gemma 3 specifics implemented here, read from the real checkpoint:
//! embed scale ×√hidden, RMSNorm with (1+w), QK-norm over head_dim, per-layer
//! RoPE base (global vs local), GQA via fast SDPA, GeGLU MLP, the sandwich
//! norms (post_attention / pre+post_feedforward), and the 5:1 local/global
//! sliding-window attention pattern.

use std::path::Path;

use anyhow::{anyhow, Result};
use mlx_rs::ops::indexing::IndexOp;
use mlx_rs::{fast, nn, ops, Array};

use crate::config::GemmaConfig;

/// `y = x @ W^T` for an HF-style weight stored as `[out, in]`.
fn linear(x: &Array, w: &Array) -> Result<Array> {
    Ok(x.matmul(&w.transpose()?)?)
}

/// Per-layer key/value cache for incremental decoding.
///
/// Each slot holds one layer's K/V post-RoPE in head-major `[1, nkv, past, hd]`;
/// a decode step appends the new position and attends over the whole history.
/// The cache is owned by the decode loop, so the model itself stays stateless
/// and safe to share across requests.
pub struct KvCache {
    layers: Vec<Option<(Array, Array)>>,
    /// Positions already cached; also the RoPE/mask offset for the next step.
    offset: usize,
}

impl KvCache {
    pub fn new(num_layers: usize) -> Self {
        Self {
            layers: (0..num_layers).map(|_| None).collect(),
            offset: 0,
        }
    }
}

/// Build an additive attention mask of shape `[1, 1, seq, klen]`.
///
/// Built by hand rather than via MLX's `Causal` mode because the local layers
/// need a sliding window, which the built-in causal mask can't express; one
/// explicit builder keeps global and local layers on a single auditable path.
/// Query row `qi` sits at absolute position `offset + qi`. `window` is `None`
/// for global layers and `Some(w)` for local ones. Disallowed positions get a
/// large finite negative -- effectively -inf for the softmax, but finite to
/// avoid NaN; every row keeps at least its own position, so none is fully
/// masked.
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

/// Top-p (nucleus) sampling over one logit row, with temperature.
///
/// Done on the CPU rather than on-device: it is trivial to read against a
/// reference and the per-token cost (one softmax + one sort of the vocab) is
/// negligible next to the forward pass. On-device sampling is a later
/// optimisation. `temperature` is assumed > 0 (greedy has its own path).
fn sample_top_p(logits: &[f32], temperature: f32, top_p: f32) -> i32 {
    // Temperature-scaled softmax, shifted by the max for numerical stability
    // (max of the scaled logits equals max(logits)/temperature for T > 0).
    let max = logits.iter().copied().fold(f32::NEG_INFINITY, f32::max);
    let mut probs: Vec<f32> = logits
        .iter()
        .map(|&l| ((l - max) / temperature).exp())
        .collect();
    let sum: f32 = probs.iter().sum();
    for p in &mut probs {
        *p /= sum;
    }

    // Keep the smallest set of highest-probability tokens whose mass reaches
    // top_p (the nucleus).
    let mut order: Vec<usize> = (0..probs.len()).collect();
    order.sort_unstable_by(|&a, &b| probs[b].total_cmp(&probs[a]));
    let mut cum = 0.0f32;
    let mut nucleus_end = order.len();
    for (rank, &i) in order.iter().enumerate() {
        cum += probs[i];
        if cum >= top_p {
            nucleus_end = rank + 1;
            break;
        }
    }
    let nucleus = &order[..nucleus_end];

    // Sample within the nucleus, renormalised by its mass.
    let mass: f32 = nucleus.iter().map(|&i| probs[i]).sum();
    let mut r = rand::random::<f32>() * mass;
    for &i in nucleus {
        r -= probs[i];
        if r <= 0.0 {
            return i as i32;
        }
    }
    // Floating-point slack can let the loop fall through; the last nucleus
    // token is the safe choice.
    nucleus[nucleus.len() - 1] as i32
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
        cfg: &GemmaConfig,
        offset: i32,
        cache: &mut Option<(Array, Array)>,
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

        // Append this step's K/V to the running cache, then attend over the
        // full history (stored post-RoPE in head-major [1, nkv, past, hd]).
        let (k, v) = match cache.take() {
            Some((pk, pv)) => (
                ops::concatenate_axis(&[pk, k], 2)?,
                ops::concatenate_axis(&[pv, v], 2)?,
            ),
            None => (k, v),
        };
        *cache = Some((k.clone(), v.clone()));

        let klen = k.shape()[2];
        let mask = attention_mask(offset, seq, klen, self.sliding_window);
        let mask = fast::ScaledDotProductAttentionMask::Array(&mask);
        let o = fast::scaled_dot_product_attention(&q, &k, &v, cfg.attn_scale(), Some(mask))?;

        let o = o.transpose_axes(&[0, 2, 1, 3])?.reshape(&[seq, n * hd])?;
        linear(&o, &self.o_proj)
    }

    fn forward(
        &self,
        x: &Array,
        cfg: &GemmaConfig,
        offset: i32,
        cache: &mut Option<(Array, Array)>,
    ) -> Result<Array> {
        let eps = cfg.rms_norm_eps;

        // Attention block with sandwich norm: residual + post_attn(attn(input_ln(x))).
        let normed = fast::rms_norm(x, &self.input_ln, eps)?;
        let attn = self.attention(&normed, cfg, offset, cache)?;
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
                sliding_window: if cfg.is_global_layer(i) {
                    None
                } else {
                    Some(cfg.sliding_window as i32)
                },
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

    /// Run `tokens` through the model, updating `cache`, and return logits
    /// `[tokens, vocab]`. On the first call `tokens` is the whole prompt
    /// (prefill); on later calls it is just the most recent token.
    fn forward(&self, tokens: &[i32], cache: &mut KvCache) -> Result<Array> {
        let seq = tokens.len() as i32;
        let ids = Array::from_slice(tokens, &[seq]);

        let scale = Array::from_slice(&[self.cfg.embed_scale()], &[1]);
        let mut h = self.embed.index(&ids).multiply(&scale)?;

        let offset = cache.offset as i32;
        for (layer, slot) in self.layers.iter().zip(cache.layers.iter_mut()) {
            h = layer.forward(&h, &self.cfg, offset, slot)?;
        }
        cache.offset += tokens.len();

        let h = fast::rms_norm(&h, &self.final_norm, self.cfg.rms_norm_eps)?;
        Ok(h.matmul(&self.lm_head.transpose()?)?)
    }

    /// Logits `[vocab]` for the position right after this step's last token.
    fn last_logits(&self, tokens: &[i32], cache: &mut KvCache) -> Result<Array> {
        let logits = self.forward(tokens, cache)?;
        Ok(logits.index(tokens.len() as i32 - 1))
    }

    /// Greedy next-token id for `tokens`, advancing `cache` by their count.
    pub fn forward_argmax(&self, tokens: &[i32], cache: &mut KvCache) -> Result<i32> {
        let last = self.last_logits(tokens, cache)?;
        let next = ops::indexing::argmax(&last, false)?;
        Ok(next.item::<u32>() as i32)
    }

    /// Temperature + top-p sampled next-token id, advancing `cache`. The engine
    /// routes `temperature <= 0` to `forward_argmax`, so here it is always > 0.
    pub fn forward_sample(
        &self,
        tokens: &[i32],
        cache: &mut KvCache,
        temperature: f32,
        top_p: f32,
    ) -> Result<i32> {
        let last = self.last_logits(tokens, cache)?;
        last.eval()?;
        Ok(sample_top_p(last.as_slice::<f32>(), temperature, top_p))
    }
}
