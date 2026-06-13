//! Qwen 3.5 generation (`qwen3_5` text architecture) in MLX: the hybrid
//! GatedDeltaNet + gated-attention family, quantized (affine/mxfp4).
//!
//! This is the largest family lift so far because three of every four layers
//! are *linear attention*: a GatedDeltaNet recurrence with no growing K/V, only
//! a fixed-size convolution tail and recurrent state. Every fourth layer
//! (`(idx + 1) % full_attention_interval == 0`) is ordinary gated GQA. The
//! cache is therefore split: full layers use [`KvSlot`]s, linear layers use
//! [`DeltaState`]; one position counter advances both.
//!
//! Ported against mlx_lm's `qwen3_5.py` / `qwen3_next.py` / `gated_delta.py`.
//! The 9B dense checkpoint specifics, all confirmed from the reference and the
//! safetensors headers:
//!
//! * The checkpoint ships `conv1d.weight` already in MLX `[C, K, 1]` layout and
//!   carries no MTP head, so mlx_lm's `should_shift_norm_weights` is false:
//!   RMSNorm uses the bare weight (NO gemma-style +1 fold). We replicate the
//!   exact detection so a differently-converted checkpoint still loads right.
//! * `A_log` stays f32 (mlx_lm's `cast_predicate` excludes it); the whole
//!   delta recurrence runs in f32 (the state dominates), output cast back to
//!   bf16 -- this is the reference's precision, not a cast-storm bug.
//! * Gated attention: `q_proj` is double width (queries + an output gate);
//!   per-head QK-norm; partial RoPE over the first `head_dim * 0.25` dims;
//!   the attention output is gated by `sigmoid(gate)` before `o_proj`.
//! * Multimodal wrapper (`Qwen3_5ForConditionalGeneration`): we load only the
//!   `language_model.` text tower and ignore the vision tower.

use std::collections::HashMap;
use std::path::Path;

use anyhow::{anyhow, Context, Result};
use mlx_rs::ops::indexing::{IndexOp, TryIndexOp};
use mlx_rs::{fast, nn, ops, Array, Dtype};
use oos_infer::openai::ChatMessage;
use oos_infer::ModelFiles;
use serde::Deserialize;
use tokenizers::Tokenizer;

use super::gemma4::{gather, load_weights, QLinear, QuantConfig};
use super::{DeltaState, KvCache, KvSlot, MaskKind, Model};

/// Compute dtype: bf16 like the mlx_lm reference. The quantized matmuls emit
/// the activation dtype, so seeding the embedding as bf16 keeps the whole
/// attention/MLP stack in bf16; only the delta recurrence widens to f32.
const COMPUTE: Dtype = Dtype::Bfloat16;

/// RoPE parameters as the checkpoint nests them under `rope_parameters`. Type
/// is "default" for the text tower (the `mrope_section` only matters for image
/// position ids, which text generation never uses), so this reduces to a plain
/// partial RoPE: rotate the first `head_dim * partial_rotary_factor` dims.
#[derive(Debug, Clone, Deserialize)]
struct RopeParameters {
    #[serde(default = "default_rope_theta")]
    rope_theta: f32,
    #[serde(default = "default_partial")]
    partial_rotary_factor: f32,
}

fn default_rope_theta() -> f32 {
    1_000_000.0
}
fn default_partial() -> f32 {
    0.25
}
fn default_true() -> bool {
    true
}
fn default_sparse_step() -> usize {
    1
}

/// qwen3_5 text-tower parameters, parsed from the wrapper's `text_config`.
#[allow(dead_code)]
#[derive(Debug, Clone, Deserialize)]
struct Qwen35Config {
    hidden_size: usize,
    // Dense FFN width. MoE checkpoints (35B-A3B) carry only moe_/shared_expert_
    // intermediate sizes and omit this, so default it; the dense MLP path that
    // reads it is never taken when every layer is MoE.
    #[serde(default)]
    intermediate_size: usize,
    num_hidden_layers: usize,
    num_attention_heads: usize,
    num_key_value_heads: usize,
    head_dim: usize,
    rms_norm_eps: f32,
    vocab_size: usize,
    full_attention_interval: usize,
    linear_num_value_heads: usize,
    linear_num_key_heads: usize,
    linear_key_head_dim: usize,
    linear_value_head_dim: usize,
    linear_conv_kernel_dim: usize,
    // MoE (sparse FFN) parameters. Dense checkpoints (the 9B) omit them, so
    // every field defaults; num_experts 0 means "dense MLP on every layer".
    #[serde(default)]
    num_experts: usize,
    #[serde(default)]
    num_experts_per_tok: usize,
    #[serde(default)]
    moe_intermediate_size: usize,
    #[serde(default)]
    shared_expert_intermediate_size: usize,
    #[serde(default = "default_true")]
    norm_topk_prob: bool,
    #[serde(default = "default_sparse_step")]
    decoder_sparse_step: usize,
    #[serde(default)]
    mlp_only_layers: Vec<usize>,
    rope_parameters: RopeParameters,
    // The checkpoint may omit this or ship it as JSON null; Option tolerates
    // both, where a plain `bool` with serde(default) would reject an explicit
    // null. Our 9B is untied (None -> false).
    #[serde(default)]
    tie_word_embeddings: Option<bool>,
    #[serde(skip)]
    eos_token_id: Vec<u32>,
}

impl Qwen35Config {
    fn load(path: &Path) -> Result<Self> {
        let text =
            std::fs::read_to_string(path).with_context(|| format!("reading {}", path.display()))?;
        let root: serde_json::Value =
            serde_json::from_str(&text).with_context(|| format!("parsing {}", path.display()))?;
        // The multimodal wrapper nests the real parameters under text_config;
        // a flat (pure-text) config is accepted as a fallback.
        let tc = root
            .get("text_config")
            .cloned()
            .unwrap_or_else(|| root.clone());
        let mut cfg: Qwen35Config =
            serde_json::from_value(tc).context("parsing qwen3_5 text_config")?;
        cfg.eos_token_id = parse_eos(&root);
        Ok(cfg)
    }

    /// Whether layer `idx` is a GatedDeltaNet (linear) layer; every
    /// `full_attention_interval`-th layer is full gated attention instead.
    fn is_linear(&self, idx: usize) -> bool {
        (idx + 1) % self.full_attention_interval != 0
    }

    /// Whether layer `idx` uses the sparse MoE FFN instead of the dense MLP.
    /// Mirrors the reference dispatch: experts present, the layer is not in
    /// `mlp_only_layers`, and it sits on a `decoder_sparse_step` boundary.
    fn is_moe(&self, idx: usize) -> bool {
        self.num_experts > 0
            && !self.mlp_only_layers.contains(&idx)
            && (idx + 1) % self.decoder_sparse_step == 0
    }

    /// Rotary dims for the partial RoPE: the first `head_dim * factor` head
    /// dims rotate, the rest pass through (fast::rope rotates a prefix).
    fn rope_dims(&self) -> i32 {
        (self.head_dim as f32 * self.rope_parameters.partial_rotary_factor) as i32
    }

    /// Total key projection width across all linear key heads.
    fn key_dim(&self) -> i32 {
        (self.linear_key_head_dim * self.linear_num_key_heads) as i32
    }
    /// Total value projection width across all linear value heads.
    fn value_dim(&self) -> i32 {
        (self.linear_value_head_dim * self.linear_num_value_heads) as i32
    }
    /// Depthwise-conv channel count: q + k + v projections concatenated.
    fn conv_dim(&self) -> i32 {
        self.key_dim() * 2 + self.value_dim()
    }
}

/// Collect eos ids from both the top-level config and `text_config`, so the
/// ChatML turn terminator (`<|im_end|>`) is covered wherever the checkpoint
/// declares it.
fn parse_eos(root: &serde_json::Value) -> Vec<u32> {
    let mut out = Vec::new();
    let mut collect = |v: Option<&serde_json::Value>| match v {
        Some(serde_json::Value::Array(a)) => {
            out.extend(a.iter().filter_map(|x| x.as_u64().map(|u| u as u32)));
        }
        Some(serde_json::Value::Number(n)) => {
            if let Some(u) = n.as_u64() {
                out.push(u as u32);
            }
        }
        _ => {}
    };
    collect(root.get("eos_token_id"));
    if let Some(tc) = root.get("text_config") {
        collect(tc.get("eos_token_id"));
    }
    out
}

/// A full gated-attention block: GQA with per-head QK-norm, partial RoPE and a
/// sigmoid output gate carved out of a double-width query projection.
struct AttnMixer {
    q_proj: QLinear,
    k_proj: QLinear,
    v_proj: QLinear,
    o_proj: QLinear,
    q_norm: Array,
    k_norm: Array,
}

impl AttnMixer {
    fn forward(
        &self,
        x: &Array,
        cfg: &Qwen35Config,
        offset: i32,
        mask: &MaskKind,
        slot: &mut KvSlot,
    ) -> Result<Array> {
        let seq = x.shape()[0];
        let n = cfg.num_attention_heads as i32;
        let nkv = cfg.num_key_value_heads as i32;
        let hd = cfg.head_dim as i32;
        let rope_dims = cfg.rope_dims();
        let theta = cfg.rope_parameters.rope_theta;
        let eps = cfg.rms_norm_eps;

        // q_proj is double width: reshape to [1, seq, n, 2*hd] and split each
        // head into a query half and an output-gate half.
        let qp = self.q_proj.forward(x)?.reshape(&[1, seq, n, 2 * hd])?;
        let queries = qp.try_index((.., .., .., 0..hd))?;
        let gate = qp.try_index((.., .., .., hd..2 * hd))?.reshape(&[seq, n * hd])?;

        // QK-norm over the head dim, then transpose to head-major and RoPE at
        // the absolute position so cached and fresh tokens share a frame.
        let q = fast::rms_norm(&queries, &self.q_norm, eps)?.transpose_axes(&[0, 2, 1, 3])?;
        let q = fast::rope(&q, rope_dims, false, Some(theta), 1.0, offset, None::<&Array>)?;
        let k = self.k_proj.forward(x)?.reshape(&[1, seq, nkv, hd])?;
        let k = fast::rms_norm(&k, &self.k_norm, eps)?.transpose_axes(&[0, 2, 1, 3])?;
        let k = fast::rope(&k, rope_dims, false, Some(theta), 1.0, offset, None::<&Array>)?;
        let v = self
            .v_proj
            .forward(x)?
            .reshape(&[1, seq, nkv, hd])?
            .transpose_axes(&[0, 2, 1, 3])?;

        // Full attention on every gated-attention layer: append, no window.
        let (k, v) = slot.update(&k, &v, None)?;

        let sdpa_mask = match mask {
            MaskKind::None => None,
            MaskKind::Causal => Some(fast::ScaledDotProductAttentionMask::Causal),
            MaskKind::Mask(m) => Some(fast::ScaledDotProductAttentionMask::Array(m)),
        };
        let scale = (hd as f32).powf(-0.5);
        let o = fast::scaled_dot_product_attention(&q, &k, &v, scale, sdpa_mask, None)?;
        let o = o.transpose_axes(&[0, 2, 1, 3])?.reshape(&[seq, n * hd])?;

        // Output gate then projection: o * sigmoid(gate), all in bf16.
        let gated = o.multiply(&ops::sigmoid(&gate)?)?;
        self.o_proj.forward(&gated)
    }
}

/// A GatedDeltaNet block: a depthwise causal conv feeds a delta-rule
/// recurrence with a fixed-size state, gated by a learned decay. Holds no
/// growing K/V -- its entire past is the [`DeltaState`] (conv tail + state).
struct DeltaMixer {
    in_proj_qkv: QLinear,
    in_proj_z: QLinear,
    in_proj_b: QLinear,
    in_proj_a: QLinear,
    /// Depthwise conv weight `[conv_dim, kernel, 1]` (groups = conv_dim).
    conv_weight: Array,
    /// Per-value-head time-step bias added before the softplus, bf16.
    dt_bias: Array,
    /// Precomputed `-exp(A_log)` in f32: the decay's negative rate, so a step
    /// is `g = exp(neg_exp_a_log * softplus(a + dt_bias))`. A_log stays f32.
    neg_exp_a_log: Array,
    /// Gated-RMSNorm weight over the value head dim, bf16.
    norm_weight: Array,
    out_proj: QLinear,
}

impl DeltaMixer {
    /// `qk_ones` is a `[head_k_dim]` ones vector standing in for the
    /// reference's weightless `rms_norm(q, None, eps)` (mlx-rs requires a
    /// weight).
    fn forward(
        &self,
        x: &Array,
        cfg: &Qwen35Config,
        qk_ones: &Array,
        layer_idx: usize,
        cache: &mut KvCache,
    ) -> Result<Array> {
        let seq = x.shape()[0];
        let conv_dim = cfg.conv_dim();
        let key_dim = cfg.key_dim();
        let value_dim = cfg.value_dim();
        let n_k = cfg.linear_num_key_heads as i32;
        let n_v = cfg.linear_num_value_heads as i32;
        let dk = cfg.linear_key_head_dim as i32;
        let dv = cfg.linear_value_head_dim as i32;
        let pad = cfg.linear_conv_kernel_dim as i32 - 1;

        // Projections (bf16). qkv feeds the conv; z is the output gate; a/b are
        // the per-value-head gating logits.
        let qkv = self.in_proj_qkv.forward(x)?.reshape(&[1, seq, conv_dim])?;
        let z = self.in_proj_z.forward(x)?;
        let b_raw = self.in_proj_b.forward(x)?;
        let a_raw = self.in_proj_a.forward(x)?;

        // Causal depthwise conv across the step boundary: prepend the cached
        // last `pad` inputs, convolve (padding 0 drops exactly `pad` outputs),
        // and keep the new trailing `pad` as the next conv state.
        let prior = cache.delta_state(layer_idx);
        let conv_state = match &prior {
            Some(ds) => ds.conv.clone(),
            None => ops::zeros_dtype(&[1, pad, conv_dim], COMPUTE)?,
        };
        let conv_input = ops::concatenate_axis(&[conv_state, qkv], 1)?;
        let total = pad + seq;
        let new_conv = conv_input.try_index((.., total - pad..total, ..))?;
        let conv_out = ops::conv1d(
            &conv_input,
            &self.conv_weight,
            Some(1),
            Some(0),
            Some(1),
            Some(conv_dim),
        )?;
        // silu, built by hand to keep bf16 (no f32 scalar constants).
        let conv_out = conv_out.multiply(&ops::sigmoid(&conv_out)?)?;
        let conv_out = conv_out.reshape(&[seq, conv_dim])?;

        // Split into per-head q/k/v.
        let q = conv_out.try_index((.., 0..key_dim))?.reshape(&[seq, n_k, dk])?;
        let k = conv_out
            .try_index((.., key_dim..2 * key_dim))?
            .reshape(&[seq, n_k, dk])?;
        let v = conv_out
            .try_index((.., 2 * key_dim..conv_dim))?
            .reshape(&[seq, n_v, dv])?;

        // Weightless RMSNorm over the key head dim, scaled by inv (q twice).
        let inv = (dk as f32).powf(-0.5);
        let q2 = Array::from_slice(&[inv * inv], &[1]).as_dtype(COMPUTE)?;
        let k1 = Array::from_slice(&[inv], &[1]).as_dtype(COMPUTE)?;
        let q = fast::rms_norm(&q, qk_ones, 1e-6)?.multiply(&q2)?;
        let k = fast::rms_norm(&k, qk_ones, 1e-6)?.multiply(&k1)?;

        // Hold on to the pre-repeat q/k ([seq, n_k, dk]) for the Metal-kernel
        // path, which maps key heads to value heads on its own.
        let q_pre = q.clone();
        let k_pre = k.clone();

        // Repeat q/k from key heads to value heads, interleaved -- matches
        // mlx.repeat over the head axis (h0,h0,h1,h1,...).
        let rf = n_v / n_k;
        let q = ops::broadcast_to(&q.reshape(&[seq, n_k, 1, dk])?, &[seq, n_k, rf, dk])?
            .reshape(&[seq, n_v, dk])?;
        let k = ops::broadcast_to(&k.reshape(&[seq, n_k, 1, dk])?, &[seq, n_k, rf, dk])?
            .reshape(&[seq, n_v, dk])?;

        // Gating: beta = sigmoid(b); g = exp(-exp(A_log) * softplus(a+dt_bias)).
        // softplus stays bf16 (matches the reference), the rest promotes to f32.
        let beta = ops::sigmoid(&b_raw)?.as_dtype(Dtype::Float32)?;
        let sp = nn::softplus(&a_raw.add(&self.dt_bias)?)?;
        let g = self.neg_exp_a_log.multiply(&sp)?.exp()?;

        // Initial recurrent state, shared by both paths.
        let prior_state = match &prior {
            Some(ds) => ds.recurrent.clone(),
            None => ops::zeros_dtype(&[n_v, dv, dk], Dtype::Float32)?,
        };

        // The recurrence is the perf bottleneck: a sequential T-step graph, so
        // the single-launch Metal kernel is the default (~2.4x faster prefill
        // at 2.7k ctx). OOSMLX_GATED_DELTA=ops forces the byte-identical
        // reference recurrence (see 6af38a3 / qwen35_parity.py) for parity.
        let use_kernel = std::env::var("OOSMLX_GATED_DELTA").as_deref() != Ok("ops");
        let (y, state) = if use_kernel {
            // q/k stay bf16 (pre-repeat); the kernel widens to f32 internally
            // and maps key heads to value heads itself.
            gated_delta_kernel(
                &q_pre,
                &k_pre,
                &v,
                &g,
                &beta,
                &prior_state,
                seq,
                n_k,
                n_v,
                dk,
                dv,
            )?
        } else {
            // The delta-rule recurrence runs in f32 (the state dominates), an
            // ops reference port of gated_delta.py.
            let q = q.as_dtype(Dtype::Float32)?;
            let k = k.as_dtype(Dtype::Float32)?;
            let v = v.as_dtype(Dtype::Float32)?;
            let mut state = prior_state;
            let mut ys: Vec<Array> = Vec::with_capacity(seq as usize);
            for t in 0..seq {
                let qt = q.index(t);
                let kt = k.index(t).reshape(&[n_v, 1, dk])?;
                let vt = v.index(t);
                let decay = g.index(t).reshape(&[n_v, 1, 1])?;
                let bt = beta.index(t).reshape(&[n_v, 1])?;

                state = state.multiply(&decay)?;
                let kv_mem = state.multiply(&kt)?.sum_axes(&[-1], false)?;
                let delta = vt
                    .subtract(&kv_mem)?
                    .multiply(&bt)?
                    .reshape(&[n_v, dv, 1])?;
                state = state.add(&kt.multiply(&delta)?)?;
                let yt = state
                    .multiply(&qt.reshape(&[n_v, 1, dk])?)?
                    .sum_axes(&[-1], false)?;
                ys.push(yt.as_dtype(COMPUTE)?);
            }
            (ops::stack_axis(&ys, 0)?, state)
        };
        cache.set_delta_state(
            layer_idx,
            DeltaState {
                conv: new_conv,
                recurrent: state,
            },
        );

        // Gated RMSNorm: rms_norm(y) then silu(z) * normed, both in f32, cast
        // back to bf16 -- the reference's precise SwiGLU gate.
        let normed = fast::rms_norm(&y, &self.norm_weight, cfg.rms_norm_eps)?;
        let z = z.reshape(&[seq, n_v, dv])?.as_dtype(Dtype::Float32)?;
        let out = nn::silu(&z)?
            .multiply(&normed.as_dtype(Dtype::Float32)?)?
            .as_dtype(COMPUTE)?
            .reshape(&[seq, value_dim])?;
        self.out_proj.forward(&out)
    }
}

/// Metal source for the full GatedDeltaNet recurrence over all `T` steps,
/// ported verbatim from mlx-lm `gated_delta.py` `_make_gated_delta_kernel`
/// (scalar gating, no mask). The sequential recurrence collapses into one
/// launch: a 32-thread simd-group cooperates over the `Dk` reduction via
/// `simd_sum`, one `Dv` element per `grid.y`, one (batch, value-head) per
/// `grid.z`, and per-thread `state` registers carry across the time loop.
//
// `T` is a 0-d int32 input (MLX binds scalar inputs by value, so it is usable
// as a plain `T`); `Dk/Dv/Hk/Hv` and the `InT/StT` dtypes are template args.
const GATED_DELTA_SOURCE: &str = r#"
        auto n = thread_position_in_grid.z;
        auto b_idx = n / Hv;
        auto hv_idx = n % Hv;
        auto hk_idx = hv_idx / (Hv / Hk);
        constexpr int n_per_t = Dk / 32;

        // q, k: [B, T, Hk, Dk]
        auto q_ = q + b_idx * T * Hk * Dk + hk_idx * Dk;
        auto k_ = k + b_idx * T * Hk * Dk + hk_idx * Dk;

        // v, y: [B, T, Hv, Dv]
        auto v_ = v + b_idx * T * Hv * Dv + hv_idx * Dv;
        y += b_idx * T * Hv * Dv + hv_idx * Dv;

        auto dk_idx = thread_position_in_threadgroup.x;
        auto dv_idx = thread_position_in_grid.y;

        // state_in, state_out: [B, Hv, Dv, Dk]
        auto i_state = state_in + (n * Dv + dv_idx) * Dk;
        auto o_state = state_out + (n * Dv + dv_idx) * Dk;

        float state[n_per_t];
        for (int i = 0; i < n_per_t; ++i) {
          auto s_idx = n_per_t * dk_idx + i;
          state[i] = static_cast<float>(i_state[s_idx]);
        }

        // g: [B, T, Hv]
        auto g_ = g + b_idx * T * Hv;
        auto beta_ = beta + b_idx * T * Hv;

        for (int t = 0; t < T; ++t) {
          float kv_mem = 0.0f;
          for (int i = 0; i < n_per_t; ++i) {
            auto s_idx = n_per_t * dk_idx + i;
            state[i] = state[i] * g_[hv_idx];
            kv_mem += state[i] * k_[s_idx];
          }
          kv_mem = simd_sum(kv_mem);

          auto delta = (v_[dv_idx] - kv_mem) * beta_[hv_idx];

          float out = 0.0f;
          for (int i = 0; i < n_per_t; ++i) {
            auto s_idx = n_per_t * dk_idx + i;
            state[i] = state[i] + k_[s_idx] * delta;
            out += state[i] * q_[s_idx];
          }
          out = simd_sum(out);
          if (thread_index_in_simdgroup == 0) {
            y[dv_idx] = static_cast<InT>(out);
          }
          q_ += Hk * Dk;
          k_ += Hk * Dk;
          v_ += Hv * Dv;
          y += Hv * Dv;
          g_ += Hv;
          beta_ += Hv;
        }
        for (int i = 0; i < n_per_t; ++i) {
          auto s_idx = n_per_t * dk_idx + i;
          o_state[s_idx] = static_cast<StT>(state[i]);
        }
"#;

/// Run the GatedDeltaNet recurrence as a single Metal-kernel launch instead of
/// the sequential ops loop. `q`/`k` are pre-repeat `[seq, n_k, dk]` (the kernel
/// maps key heads to value heads itself), `v` is `[seq, n_v, dv]`, all bf16;
/// `g`/`beta` are `[seq, n_v]` f32; `state` is `[n_v, dv, dk]` f32. Returns the
/// stacked per-step output `[seq, n_v, dv]` (bf16) and the carried-forward state.
fn gated_delta_kernel(
    q: &Array,
    k: &Array,
    v: &Array,
    g: &Array,
    beta: &Array,
    state: &Array,
    seq: i32,
    n_k: i32,
    n_v: i32,
    dk: i32,
    dv: i32,
) -> Result<(Array, Array)> {
    use fast::{MetalKernel, MetalKernelConfig, MetalKernelTemplateArg};

    // Reshape to the kernel's batched layout; batch B = 1 here.
    let q = q.reshape(&[1, seq, n_k, dk])?;
    let k = k.reshape(&[1, seq, n_k, dk])?;
    let v = v.reshape(&[1, seq, n_v, dv])?;
    let g = g.reshape(&[1, seq, n_v])?;
    let beta = beta.reshape(&[1, seq, n_v])?;
    let state = state.reshape(&[1, n_v, dv, dk])?;
    // Scalar (0-d) input; bound by value inside the kernel as `T`.
    let t = Array::from_int(seq);

    let kernel = MetalKernel::new(
        "oosmlx_gated_delta_step",
        &["q", "k", "v", "g", "beta", "state_in", "T"],
        &["y", "state_out"],
        GATED_DELTA_SOURCE,
        "",
        true,
        false,
    )?;

    let y_shape: &[i32] = &[1, seq, n_v, dv];
    let state_shape: &[i32] = &[1, n_v, dv, dk];
    let config = MetalKernelConfig {
        output_shapes: &[y_shape, state_shape],
        output_dtypes: &[COMPUTE, Dtype::Float32],
        grid: (32, dv, n_v),
        thread_group: (32, 4, 1),
        template_args: &[
            ("InT", MetalKernelTemplateArg::Dtype(COMPUTE)),
            ("StT", MetalKernelTemplateArg::Dtype(Dtype::Float32)),
            ("Dk", MetalKernelTemplateArg::Int(dk)),
            ("Dv", MetalKernelTemplateArg::Int(dv)),
            ("Hk", MetalKernelTemplateArg::Int(n_k)),
            ("Hv", MetalKernelTemplateArg::Int(n_v)),
        ],
        init_value: None,
        verbose: false,
    };

    let inputs = [q, k, v, g, beta, state, t];
    let outputs = kernel.apply(&inputs, &config)?;
    let y = outputs[0].reshape(&[seq, n_v, dv])?;
    let new_state = outputs[1].reshape(&[n_v, dv, dk])?;
    Ok((y, new_state))
}

/// Either mixer kind for a decoder layer, selected by the layer index.
enum Mixer {
    Attn(AttnMixer),
    Delta(DeltaMixer),
}

/// One decoder layer: pre-norm mixer (attention or delta) plus a pre-norm
/// SwiGLU MLP, residual around each.
/// SiLU keeping the activation in its input dtype (`x * sigmoid(x)`). An f32
/// sigmoid constant would promote bf16 to f32 and cast-storm the FFN.
fn silu(x: &Array) -> Result<Array> {
    Ok(x.multiply(&ops::sigmoid(x)?)?)
}

/// Per-layer feed-forward: either a dense SwiGLU MLP or the sparse MoE block,
/// chosen at load time by `Qwen35Config::is_moe`.
enum Ffn {
    Dense {
        gate: QLinear,
        up: QLinear,
        down: QLinear,
    },
    Moe(Moe),
}

/// Sparse MoE FFN (Qwen3-Next / Qwen3.5 form): a plain top-k router over
/// `num_experts` stacked SwiGLU experts (SwitchGLU via gather_qmm) plus an
/// always-on shared expert, sigmoid-gated and summed in.
struct Moe {
    /// Router projection hidden -> num_experts (no bias, no pre-norm).
    router: QLinear,
    /// Stacked expert weights, addressed per top-k index by `gather`.
    switch_gate: QLinear,
    switch_up: QLinear,
    switch_down: QLinear,
    /// Dense shared expert applied to every token.
    shared_gate: QLinear,
    shared_up: QLinear,
    shared_down: QLinear,
    /// Scalar gate hidden -> 1 multiplying the shared expert's output.
    shared_gate_proj: QLinear,
}

impl Moe {
    /// Route to top-k experts, SwitchGLU over them, weight-combine, then add the
    /// sigmoid-gated shared expert. `x` is the post-attention-normed input.
    fn forward(
        &self,
        x: &Array,
        num_experts: i32,
        top_k: i32,
        norm_topk: bool,
        hidden: i32,
    ) -> Result<Array> {
        let seq = x.shape()[0];

        // Router: plain projection, softmax over *all* experts, then top-k --
        // the reference order (top-k of the full softmax), not softmax-of-top-k.
        let scores = self.router.forward(x)?; // [seq, E]
        let gates = ops::softmax_axis(&scores, -1, None)?;
        let part = ops::argpartition_axis(&gates, -top_k, -1)?;
        let last = Array::arange::<_, i32>(num_experts - top_k, num_experts, None)?;
        let idx = ops::indexing::take_axis(&part, &last, -1)?; // [seq, k] expert ids
        let mut weights = ops::indexing::take_along_axis(&gates, &idx, -1)?; // [seq, k]
        if norm_topk {
            let denom = weights.sum_axes(&[-1], true)?;
            weights = weights.divide(&denom)?;
        }

        // SwitchGLU over the selected experts. Prefill sorts the (token, expert)
        // pairs by expert id so gather_qmm streams each expert's weights once;
        // single-token decode (n = top_k) stays on the plain path.
        let n = seq * top_k;
        let y = if n >= 64 {
            let flat = idx.reshape(&[n])?;
            let order = ops::argsort(&flat)?;
            let inv_order = ops::argsort(&order)?;
            let sorted_idx = ops::indexing::take(&flat, &order)?;
            let rows = ops::floor_divide(&order, &Array::from_int(top_k))?;
            let xs = ops::indexing::take_axis(x, &rows, 0)?.reshape(&[n, 1, hidden])?;
            let up = gather(&self.switch_up, &xs, &sorted_idx, true)?;
            let g = gather(&self.switch_gate, &xs, &sorted_idx, true)?;
            let act = silu(&g)?.multiply(&up)?;
            let down = gather(&self.switch_down, &act, &sorted_idx, true)?.reshape(&[n, hidden])?;
            ops::indexing::take_axis(&down, &inv_order, 0)?.reshape(&[seq, top_k, hidden])?
        } else {
            let xe = ops::expand_dims_axes(x, &[-2, -3])?; // [seq, 1, 1, hidden]
            let up = gather(&self.switch_up, &xe, &idx, false)?;
            let g = gather(&self.switch_gate, &xe, &idx, false)?;
            let act = silu(&g)?.multiply(&up)?;
            gather(&self.switch_down, &act, &idx, false)?.reshape(&[seq, top_k, hidden])?
        };
        let w = ops::expand_dims_axes(&weights, &[-1])?; // [seq, k, 1]
        let routed = w.multiply(&y)?.sum_axes(&[-2], false)?; // [seq, hidden]

        // Shared expert: dense SwiGLU gated by sigmoid(shared_gate_proj(x)).
        let shared = self.shared_down.forward(
            &silu(&self.shared_gate.forward(x)?)?.multiply(&self.shared_up.forward(x)?)?,
        )?;
        let gate = ops::sigmoid(&self.shared_gate_proj.forward(x)?)?; // [seq, 1]
        let shared = gate.multiply(&shared)?;

        Ok(routed.add(&shared)?)
    }
}

struct Layer {
    input_ln: Array,
    post_attn_ln: Array,
    mixer: Mixer,
    ffn: Ffn,
}

impl Layer {
    fn forward(
        &self,
        x: &Array,
        cfg: &Qwen35Config,
        offset: i32,
        attn_mask: &MaskKind,
        qk_ones: &Array,
        layer_idx: usize,
        cache: &mut KvCache,
    ) -> Result<Array> {
        let eps = cfg.rms_norm_eps;
        let normed = fast::rms_norm(x, &self.input_ln, eps)?;
        let mixed = match &self.mixer {
            Mixer::Attn(a) => a.forward(&normed, cfg, offset, attn_mask, &mut cache.slots_mut()[layer_idx])?,
            Mixer::Delta(d) => d.forward(&normed, cfg, qk_ones, layer_idx, cache)?,
        };
        let h = x.add(&mixed)?;

        // FFN: dense SwiGLU or sparse MoE, on the post-attention-normed input.
        let normed = fast::rms_norm(&h, &self.post_attn_ln, eps)?;
        let mlp = match &self.ffn {
            Ffn::Dense { gate, up, down } => {
                let act = silu(&gate.forward(&normed)?)?;
                down.forward(&act.multiply(&up.forward(&normed)?)?)?
            }
            Ffn::Moe(m) => m.forward(
                &normed,
                cfg.num_experts as i32,
                cfg.num_experts_per_tok as i32,
                cfg.norm_topk_prob,
                cfg.hidden_size as i32,
            )?,
        };
        Ok(h.add(&mlp)?)
    }
}

pub struct Qwen35Model {
    cfg: Qwen35Config,
    embed: QLinear,
    lm_head: QLinear,
    final_norm: Array,
    layers: Vec<Layer>,
    /// Ones over the key head dim: the weightless QK-norm of the delta path.
    delta_qk_ones: Array,
    stop: Vec<i32>,
}

impl Qwen35Model {
    pub fn load(files: &ModelFiles, tokenizer: &Tokenizer) -> Result<Self> {
        let cfg = Qwen35Config::load(&files.config_json).context("loading qwen3_5 config")?;

        let quant = QuantConfig::load(&files.config_json)?;
        let raw = load_weights(&files.dir)?;

        // Strip the multimodal wrapper's `language_model.` prefix and drop any
        // vision weights a future conversion might ship.
        let mut weights: HashMap<String, Array> = HashMap::with_capacity(raw.len());
        for (k, v) in raw {
            if let Some(rest) = k.strip_prefix("language_model.") {
                weights.insert(rest.to_string(), v);
            } else if k.starts_with("vision_tower.") || k.starts_with("model.visual") {
                continue;
            } else {
                weights.insert(k, v);
            }
        }

        // Replicate mlx_lm's norm-shift detection: a +1 fold on the norm
        // weights is applied only for checkpoints that ship an MTP head or an
        // unsanitized conv1d weight (last dim != 1). Ours satisfies neither,
        // so norms are bare -- but a differently-converted checkpoint loads
        // correctly too.
        let shift = weights.keys().any(|k| k.contains("mtp."))
            || weights.iter().any(|(k, v)| {
                k.contains("conv1d.weight") && v.shape().last().copied() != Some(1)
            });

        let one = Array::from_slice(&[1.0f32], &[1]).as_dtype(COMPUTE)?;
        let bare_norm = |name: &str| -> Result<Array> {
            let a = weights
                .get(name)
                .ok_or_else(|| anyhow!("missing tensor {name}"))?;
            Ok(a.as_dtype(COMPUTE)?)
        };
        let norm = |name: &str| -> Result<Array> {
            let a = bare_norm(name)?;
            if shift {
                Ok(a.add(&one)?)
            } else {
                Ok(a)
            }
        };

        let embed = quant.qlinear(&weights, "model.embed_tokens")?;
        let lm_head = if cfg.tie_word_embeddings.unwrap_or(false) {
            quant.qlinear(&weights, "model.embed_tokens")?
        } else {
            quant.qlinear(&weights, "lm_head")?
        };
        let final_norm = norm("model.norm.weight")?;

        let neg_one = Array::from_slice(&[-1.0f32], &[1]);
        let mut layers = Vec::with_capacity(cfg.num_hidden_layers);
        for i in 0..cfg.num_hidden_layers {
            let p = format!("model.layers.{i}");
            let mixer = if cfg.is_linear(i) {
                let lp = format!("{p}.linear_attn");
                // conv1d weight: MLX wants [C, K, 1]; an unsanitized PyTorch
                // weight ships [C, 1, K] (last dim != 1) and is moved here.
                let conv_raw = weights
                    .get(&format!("{lp}.conv1d.weight"))
                    .ok_or_else(|| anyhow!("missing tensor {lp}.conv1d.weight"))?;
                let conv_weight = if conv_raw.shape().last().copied() != Some(1) {
                    conv_raw.transpose_axes(&[0, 2, 1])?
                } else {
                    conv_raw.clone()
                };
                let dt_bias = weights
                    .get(&format!("{lp}.dt_bias"))
                    .ok_or_else(|| anyhow!("missing tensor {lp}.dt_bias"))?
                    .as_dtype(COMPUTE)?;
                // A_log stays f32 (mlx_lm cast_predicate excludes it); precompute
                // the negative decay rate the recurrence multiplies in.
                let a_log = weights
                    .get(&format!("{lp}.A_log"))
                    .ok_or_else(|| anyhow!("missing tensor {lp}.A_log"))?
                    .as_dtype(Dtype::Float32)?;
                let neg_exp_a_log = a_log.exp()?.multiply(&neg_one)?;
                Mixer::Delta(DeltaMixer {
                    in_proj_qkv: quant.qlinear(&weights, &format!("{lp}.in_proj_qkv"))?,
                    in_proj_z: quant.qlinear(&weights, &format!("{lp}.in_proj_z"))?,
                    in_proj_b: quant.qlinear(&weights, &format!("{lp}.in_proj_b"))?,
                    in_proj_a: quant.qlinear(&weights, &format!("{lp}.in_proj_a"))?,
                    conv_weight: conv_weight.as_dtype(COMPUTE)?,
                    dt_bias,
                    neg_exp_a_log,
                    norm_weight: bare_norm(&format!("{lp}.norm.weight"))?,
                    out_proj: quant.qlinear(&weights, &format!("{lp}.out_proj"))?,
                })
            } else {
                let ap = format!("{p}.self_attn");
                Mixer::Attn(AttnMixer {
                    q_proj: quant.qlinear(&weights, &format!("{ap}.q_proj"))?,
                    k_proj: quant.qlinear(&weights, &format!("{ap}.k_proj"))?,
                    v_proj: quant.qlinear(&weights, &format!("{ap}.v_proj"))?,
                    o_proj: quant.qlinear(&weights, &format!("{ap}.o_proj"))?,
                    q_norm: norm(&format!("{ap}.q_norm.weight"))?,
                    k_norm: norm(&format!("{ap}.k_norm.weight"))?,
                })
            };
            // FFN: sparse MoE on MoE layers, dense SwiGLU otherwise. Names are
            // post-`language_model.`-strip: router `mlp.gate` (8-bit per the
            // quant config), stacked experts `mlp.switch_mlp.{gate,up,down}_proj`,
            // dense shared expert `mlp.shared_expert.*`, scalar `shared_expert_gate`.
            let ffn = if cfg.is_moe(i) {
                let mp = format!("{p}.mlp");
                Ffn::Moe(Moe {
                    router: quant.qlinear(&weights, &format!("{mp}.gate"))?,
                    switch_gate: quant.qlinear(&weights, &format!("{mp}.switch_mlp.gate_proj"))?,
                    switch_up: quant.qlinear(&weights, &format!("{mp}.switch_mlp.up_proj"))?,
                    switch_down: quant.qlinear(&weights, &format!("{mp}.switch_mlp.down_proj"))?,
                    shared_gate: quant
                        .qlinear(&weights, &format!("{mp}.shared_expert.gate_proj"))?,
                    shared_up: quant.qlinear(&weights, &format!("{mp}.shared_expert.up_proj"))?,
                    shared_down: quant
                        .qlinear(&weights, &format!("{mp}.shared_expert.down_proj"))?,
                    shared_gate_proj: quant
                        .qlinear(&weights, &format!("{mp}.shared_expert_gate"))?,
                })
            } else {
                Ffn::Dense {
                    gate: quant.qlinear(&weights, &format!("{p}.mlp.gate_proj"))?,
                    up: quant.qlinear(&weights, &format!("{p}.mlp.up_proj"))?,
                    down: quant.qlinear(&weights, &format!("{p}.mlp.down_proj"))?,
                }
            };
            layers.push(Layer {
                input_ln: norm(&format!("{p}.input_layernorm.weight"))?,
                post_attn_ln: norm(&format!("{p}.post_attention_layernorm.weight"))?,
                mixer,
                ffn,
            });
        }

        let delta_qk_ones =
            Array::from_slice(&vec![1.0f32; cfg.linear_key_head_dim], &[cfg.linear_key_head_dim as i32])
                .as_dtype(COMPUTE)?;

        // Stop on the config eos plus the ChatML terminators, deduped (the eos
        // id 248044 IS <|im_end|>, but we resolve both for robustness).
        let mut stop: Vec<i32> = cfg.eos_token_id.iter().map(|&u| u as i32).collect();
        for marker in ["<|im_end|>", "<|endoftext|>"] {
            if let Some(id) = tokenizer.token_to_id(marker) {
                stop.push(id as i32);
            }
        }
        stop.sort_unstable();
        stop.dedup();
        if stop.is_empty() {
            anyhow::bail!("qwen3_5: no eos token id in config or tokenizer");
        }

        Ok(Self {
            cfg,
            embed,
            lm_head,
            final_norm,
            layers,
            delta_qk_ones,
            stop,
        })
    }

    /// Run the layer stack, updating `cache`, returning the pre-final-norm
    /// hidden states `[tokens, hidden]`. Final norm and LM head live in
    /// `forward_logits`, which projects only the row it keeps.
    fn forward_hidden(&self, ids: &Array, cache: &mut KvCache) -> Result<Array> {
        let seq = ids.dim(0);
        let mut h = self.embed.dequant_rows(ids)?.as_dtype(COMPUTE)?;
        let offset = cache.offset() as i32;
        // Full-attention layers are causal for multi-token prefill, vacuous
        // for single-token decode. The delta layers need no mask at batch 1
        // (mlx_lm's create_ssm_mask is None there).
        let attn_mask = if seq <= 1 {
            MaskKind::None
        } else {
            MaskKind::Causal
        };
        // Index by layer: attention layers borrow their KvSlot, delta layers
        // borrow the whole cache for their DeltaState.
        for i in 0..self.layers.len() {
            h = self.layers[i].forward(
                &h,
                &self.cfg,
                offset,
                &attn_mask,
                &self.delta_qk_ones,
                i,
                cache,
            )?;
        }
        cache.advance(seq as usize);
        Ok(h)
    }
}

impl Model for Qwen35Model {
    fn num_layers(&self) -> usize {
        self.cfg.num_hidden_layers
    }

    fn forward_logits(&self, tokens: &Array, cache: &mut KvCache) -> Result<Array> {
        let h = self.forward_hidden(tokens, cache)?;
        let last = h.index(tokens.dim(0) - 1).reshape(&[1, -1])?;
        let last = fast::rms_norm(&last, &self.final_norm, self.cfg.rms_norm_eps)?;
        Ok(self.lm_head.forward(&last)?.index(0))
    }

    /// Qwen ChatML: `<|im_start|>{role}\n{content}<|im_end|>\n` per turn, then
    /// an open assistant turn. Non-thinking is the bring-up default: an empty
    /// `<think>\n\n</think>\n\n` block is prefilled so the model answers
    /// directly; with `thinking` the block is left open for the model to fill.
    fn render_prompt(
        &self,
        messages: &[ChatMessage],
        thinking: bool,
        _tools: &[oos_infer::openai::Tool],
    ) -> String {
        let mut p = String::new();
        for m in messages {
            match m.role.as_str() {
                "system" | "user" | "assistant" => {
                    p.push_str("<|im_start|>");
                    p.push_str(&m.role);
                    p.push('\n');
                    p.push_str(&m.content);
                    p.push_str("<|im_end|>\n");
                }
                // Tool turns are deferred to a later increment.
                _ => {}
            }
        }
        p.push_str("<|im_start|>assistant\n");
        if thinking {
            p.push_str("<think>\n");
        } else {
            p.push_str("<think>\n\n</think>\n\n");
        }
        p
    }

    fn stop_tokens(&self) -> &[i32] {
        &self.stop
    }
}
