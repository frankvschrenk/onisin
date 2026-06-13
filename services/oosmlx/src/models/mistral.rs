//! Mistral 3 generation (`ministral3` text architecture) in MLX: Devstral
//! Small 2, Ministral 3 Instruct and friends, quantized (mxfp4/affine).
//!
//! The checkpoints ship as `Mistral3ForConditionalGeneration` -- a multimodal
//! wrapper -- but the mlx-lm conversions carry no vision weights: every tensor
//! sits under a `language_model.` prefix and the real hyperparameters under
//! `text_config`. The text tower is llama-vanilla dense (pre-norm blocks,
//! GQA, SwiGLU, untied LM head, full attention everywhere) with two ministral3
//! specifics, both mirrored from mlx_lm's reference `ministral3.py`:
//!
//! * YaRN RoPE: a precomputed per-frequency table (NTK-by-parts interpolation
//!   between extrapolated and `factor`-scaled frequencies) fed to `fast::rope`
//!   via its `freqs` override. With `mscale == mscale_all_dim` the query/key
//!   pre-scale ratio is exactly 1 (our checkpoints), but it is computed, not
//!   assumed.
//! * A "llama 4" attention temperature: queries are scaled by
//!   `1 + beta * ln(1 + floor(pos / original_max_position))` after RoPE. Below
//!   `original_max_position` (8k/16k) the factor is identically 1.0, so the
//!   common case skips the multiply entirely.
//!
//! Deliberately NOT here, unlike the gemma families: no embed scale, no
//! QK-norm, no logit softcap, and RMSNorm uses the bare weight (gemma folds
//! a +1 in at load -- copying that here would be silently wrong everywhere).

use anyhow::{anyhow, bail, Context, Result};
use mlx_rs::ops::indexing::IndexOp;
use mlx_rs::{fast, ops, Array, Dtype};
use oos_infer::openai::ChatMessage;
use oos_infer::ModelFiles;
use serde::Deserialize;
use std::collections::HashMap;
use std::path::Path;
use tokenizers::Tokenizer;

use super::gemma4::{load_weights, QLinear, QuantConfig};
use super::{KvCache, KvSlot, MaskKind, Model};

/// All quantized matmuls emit this dtype; the embedding seeds the stack with
/// it, so every layer computes in bf16 end to end.
const COMPUTE: Dtype = Dtype::Bfloat16;

/// YaRN parameters as the checkpoint ships them in `rope_parameters`.
#[derive(Debug, Clone, Deserialize)]
pub struct RopeParameters {
    pub rope_theta: f32,
    // The checkpoints ship the rope type under BOTH keys ("rope_type" and
    // "type") with the same value; a serde alias would see a duplicate field
    // and fail the whole parse, so they are two fields coalesced in
    // [`Self::rope_type`].
    #[serde(default)]
    pub rope_type: Option<String>,
    #[serde(default, rename = "type")]
    pub rope_type_legacy: Option<String>,
    #[serde(default = "one")]
    pub factor: f32,
    #[serde(default = "beta_fast_default")]
    pub beta_fast: f32,
    #[serde(default = "one")]
    pub beta_slow: f32,
    #[serde(default = "one")]
    pub mscale: f32,
    #[serde(default)]
    pub mscale_all_dim: f32,
    #[serde(default = "orig_default")]
    pub original_max_position_embeddings: f32,
    #[serde(default)]
    pub llama_4_scaling_beta: Option<f32>,
}

impl RopeParameters {
    /// The effective rope type, coalescing the duplicated config keys.
    fn rope_type(&self) -> &str {
        self.rope_type
            .as_deref()
            .or(self.rope_type_legacy.as_deref())
            .unwrap_or("default")
    }
}

fn one() -> f32 {
    1.0
}
fn beta_fast_default() -> f32 {
    32.0
}
fn orig_default() -> f32 {
    4096.0
}

/// ministral3 text-tower parameters, parsed from the wrapper's `text_config`
/// (or a flat config for pure-text checkpoints).
#[allow(dead_code)]
#[derive(Debug, Clone, Deserialize)]
pub struct MistralConfig {
    pub hidden_size: usize,
    pub intermediate_size: usize,
    pub num_hidden_layers: usize,
    pub num_attention_heads: usize,
    pub num_key_value_heads: usize,
    #[serde(default)]
    pub head_dim: Option<usize>,
    pub vocab_size: usize,
    pub rms_norm_eps: f32,
    pub rope_parameters: RopeParameters,
    #[serde(default)]
    pub sliding_window: Option<usize>,
    #[serde(default)]
    pub layer_types: Option<Vec<String>>,
    #[serde(default)]
    pub tie_word_embeddings: bool,
}

impl MistralConfig {
    fn load(path: &Path) -> Result<Self> {
        let text =
            std::fs::read_to_string(path).with_context(|| format!("reading {}", path.display()))?;
        let root: serde_json::Value =
            serde_json::from_str(&text).with_context(|| format!("parsing {}", path.display()))?;
        // The multimodal wrapper nests the real parameters; pure-text
        // checkpoints (Ministral3ForCausalLM) carry them flat.
        let cfg = root.get("text_config").unwrap_or(&root);
        serde_json::from_value(cfg.clone())
            .with_context(|| format!("parsing text_config in {}", path.display()))
    }

    fn head_dim(&self) -> usize {
        self.head_dim
            .unwrap_or(self.hidden_size / self.num_attention_heads)
    }
}

/// The YaRN frequency table, mirroring mlx_lm `rope_utils.YarnRoPE` exactly:
/// per half-dim frequency, interpolate between the extrapolated base frequency
/// and the `factor`-scaled one along a linear ramp between the correction
/// dims for `beta_fast` and `beta_slow`. Host-side f32, computed once at load.
fn yarn_freqs(dims: usize, p: &RopeParameters) -> Array {
    let d = dims as f32;
    let base = p.rope_theta;
    let correction_dim = |rotations: f32| {
        (d * (p.original_max_position_embeddings / (rotations * 2.0 * std::f32::consts::PI)).ln())
            / (2.0 * base.ln())
    };
    let low = correction_dim(p.beta_fast).floor().max(0.0);
    let high = correction_dim(p.beta_slow).ceil().min(d - 1.0);
    // The reference nudges a degenerate range instead of dividing by zero.
    let span = if high == low { 0.001 } else { high - low };

    let half = dims / 2;
    let mut freqs = Vec::with_capacity(half);
    for i in 0..half {
        let extra = base.powf((2 * i) as f32 / d);
        let inter = p.factor * extra;
        let mask = 1.0 - ((i as f32 - low) / span).clamp(0.0, 1.0);
        freqs.push((inter * extra) / (inter * mask + extra * (1.0 - mask)));
    }
    Array::from_slice(&freqs, &[half as i32])
}

/// The YaRN query/key pre-scale ratio. `mscale == mscale_all_dim` (our
/// checkpoints) makes it exactly 1.0 and the apply path skips the multiply.
fn yarn_mscale(p: &RopeParameters) -> f32 {
    let get = |m: f32| {
        if p.factor <= 1.0 {
            1.0
        } else {
            0.1 * m * p.factor.ln() + 1.0
        }
    };
    get(p.mscale) / get(p.mscale_all_dim)
}

/// One transformer block: pre-norm attention and SwiGLU MLP, all projections
/// quantized.
struct Layer {
    input_ln: Array,
    post_attn_ln: Array,
    q_proj: QLinear,
    k_proj: QLinear,
    v_proj: QLinear,
    o_proj: QLinear,
    gate: QLinear,
    up: QLinear,
    down: QLinear,
}

impl Layer {
    #[allow(clippy::too_many_arguments)]
    fn attention(
        &self,
        x: &Array,
        cfg: &MistralConfig,
        offset: i32,
        mask: &MaskKind,
        attn_scale: Option<&Array>,
        freqs: &Array,
        mscale: f32,
        slot: &mut KvSlot,
    ) -> Result<Array> {
        let seq = x.shape()[0];
        let n = cfg.num_attention_heads as i32;
        let nkv = cfg.num_key_value_heads as i32;
        let hd = cfg.head_dim() as i32;

        // Project, split into heads, then YaRN RoPE at the absolute position
        // `offset` so cached and fresh tokens share a frame. The precomputed
        // freqs table replaces the base/scale pair.
        let rope = |a: &Array| -> Result<Array> {
            let a = if (mscale - 1.0).abs() > 1e-6 {
                let s = Array::from_slice(&[mscale], &[1]).as_dtype(a.dtype())?;
                a.multiply(&s)?
            } else {
                a.clone()
            };
            Ok(fast::rope(&a, hd, false, None, 1.0, offset, Some(freqs))?)
        };

        let q = self.q_proj.forward(x)?.reshape(&[1, seq, n, hd])?;
        let q = rope(&q.transpose_axes(&[0, 2, 1, 3])?)?;
        let k = self.k_proj.forward(x)?.reshape(&[1, seq, nkv, hd])?;
        let k = rope(&k.transpose_axes(&[0, 2, 1, 3])?)?;
        let v = self
            .v_proj
            .forward(x)?
            .reshape(&[1, seq, nkv, hd])?
            .transpose_axes(&[0, 2, 1, 3])?;

        // Full attention on every layer: the slot appends (no ring window).
        let (k, v) = slot.update(&k, &v, None)?;

        // The "llama 4" attention temperature on the queries, post-RoPE;
        // `None` below the original context length, where it is exactly 1.
        let q = match attn_scale {
            Some(s) => q.multiply(s)?,
            None => q,
        };

        let sdpa_mask = match mask {
            MaskKind::None => None,
            MaskKind::Causal => Some(fast::ScaledDotProductAttentionMask::Causal),
            MaskKind::Mask(m) => Some(fast::ScaledDotProductAttentionMask::Array(m)),
        };
        let scale = (cfg.head_dim() as f32).powf(-0.5);
        let o = fast::scaled_dot_product_attention(&q, &k, &v, scale, sdpa_mask, None)?;

        let o = o.transpose_axes(&[0, 2, 1, 3])?.reshape(&[seq, n * hd])?;
        self.o_proj.forward(&o)
    }

    #[allow(clippy::too_many_arguments)]
    fn forward(
        &self,
        x: &Array,
        cfg: &MistralConfig,
        offset: i32,
        mask: &MaskKind,
        attn_scale: Option<&Array>,
        freqs: &Array,
        mscale: f32,
        slot: &mut KvSlot,
    ) -> Result<Array> {
        let eps = cfg.rms_norm_eps;

        // Pre-norm blocks, bare RMSNorm weight (no gemma +1 fold).
        let normed = fast::rms_norm(x, &self.input_ln, eps)?;
        let attn = self.attention(&normed, cfg, offset, mask, attn_scale, freqs, mscale, slot)?;
        let h = x.add(&attn)?;

        // SwiGLU: silu(gate) * up, kept in the compute dtype (no f32
        // constants -- see the gemma4 gelu cast-storm).
        let normed = fast::rms_norm(&h, &self.post_attn_ln, eps)?;
        let g = self.gate.forward(&normed)?;
        let act = g.multiply(&ops::sigmoid(&g)?)?;
        let mlp = self
            .down
            .forward(&act.multiply(&self.up.forward(&normed)?)?)?;
        Ok(h.add(&mlp)?)
    }
}

pub struct MistralModel {
    cfg: MistralConfig,
    embed: QLinear,
    lm_head: QLinear,
    final_norm: Array,
    layers: Vec<Layer>,
    yarn_freqs: Array,
    yarn_mscale: f32,
    stop: Vec<i32>,
    /// `[TOOL_CALLS]` token id when the tokenizer defines it; gates native
    /// tool calling for this family. `None` runs the model without tools.
    tool_calls_tok: Option<i32>,
}

impl MistralModel {
    pub fn load(files: &ModelFiles, tokenizer: &Tokenizer) -> Result<Self> {
        let cfg = MistralConfig::load(&files.config_json).context("loading ministral3 config")?;

        // Sliding-attention variants exist in this family; refusing beats
        // running them with silently wrong (full) attention geometry.
        let has_sliding = cfg
            .layer_types
            .as_ref()
            .map(|t| t.iter().any(|l| l == "sliding_attention"))
            .unwrap_or(false);
        if has_sliding {
            bail!("ministral3 sliding-attention checkpoints are not supported yet");
        }
        if cfg.tie_word_embeddings {
            bail!("ministral3 tied-embedding checkpoints are not supported yet");
        }
        // The freqs table below is YaRN math; running a default/llama3-rope
        // checkpoint through it would be silently wrong at every position.
        let rope_type = cfg.rope_parameters.rope_type();
        if rope_type != "yarn" {
            bail!("ministral3 rope_type {rope_type:?} is not supported yet (only yarn)");
        }

        let quant = QuantConfig::load(&files.config_json)?;
        let raw = load_weights(&files.dir)?;

        // Strip the multimodal wrapper's `language_model.` prefix and drop
        // vision weights should a future conversion ship them.
        let mut weights: HashMap<String, Array> = HashMap::with_capacity(raw.len());
        for (k, v) in raw {
            if let Some(rest) = k.strip_prefix("language_model.") {
                weights.insert(rest.to_string(), v);
            } else if k.starts_with("vision_tower.") || k.starts_with("multi_modal_projector.") {
                continue;
            } else {
                weights.insert(k, v);
            }
        }

        let norm = |name: &str| -> Result<Array> {
            let a = weights
                .get(name)
                .ok_or_else(|| anyhow!("missing tensor {name}"))?;
            Ok(a.as_dtype(COMPUTE)?)
        };

        let embed = quant.qlinear(&weights, "model.embed_tokens")?;
        let lm_head = quant.qlinear(&weights, "lm_head")?;
        let final_norm = norm("model.norm.weight")?;

        let mut layers = Vec::with_capacity(cfg.num_hidden_layers);
        for i in 0..cfg.num_hidden_layers {
            let p = format!("model.layers.{i}");
            layers.push(Layer {
                input_ln: norm(&format!("{p}.input_layernorm.weight"))?,
                post_attn_ln: norm(&format!("{p}.post_attention_layernorm.weight"))?,
                q_proj: quant.qlinear(&weights, &format!("{p}.self_attn.q_proj"))?,
                k_proj: quant.qlinear(&weights, &format!("{p}.self_attn.k_proj"))?,
                v_proj: quant.qlinear(&weights, &format!("{p}.self_attn.v_proj"))?,
                o_proj: quant.qlinear(&weights, &format!("{p}.self_attn.o_proj"))?,
                gate: quant.qlinear(&weights, &format!("{p}.mlp.gate_proj"))?,
                up: quant.qlinear(&weights, &format!("{p}.mlp.up_proj"))?,
                down: quant.qlinear(&weights, &format!("{p}.mlp.down_proj"))?,
            });
        }

        let yarn_freqs = yarn_freqs(cfg.head_dim(), &cfg.rope_parameters);
        let yarn_mscale = yarn_mscale(&cfg.rope_parameters);

        // eos from generation_config.json (id 2, </s>), with the tokenizer as
        // fallback; the chat template terminates assistant turns with it.
        let mut stop = Vec::new();
        if let Ok(text) = std::fs::read_to_string(files.dir.join("generation_config.json")) {
            if let Ok(v) = serde_json::from_str::<serde_json::Value>(&text) {
                match v.get("eos_token_id") {
                    Some(serde_json::Value::Number(n)) => {
                        if let Some(id) = n.as_i64() {
                            stop.push(id as i32);
                        }
                    }
                    Some(serde_json::Value::Array(ids)) => {
                        stop.extend(ids.iter().filter_map(|n| n.as_i64()).map(|id| id as i32));
                    }
                    _ => {}
                }
            }
        }
        if stop.is_empty() {
            if let Some(id) = tokenizer.token_to_id("</s>") {
                stop.push(id as i32);
            }
        }
        if stop.is_empty() {
            bail!("ministral3: no eos token id in generation_config.json or tokenizer");
        }

        // `[TOOL_CALLS]` marks a call in this family's grammar (Mistral v13);
        // resolve it from the tokenizer so the decode loop can capture calls.
        let tool_calls_tok = tokenizer.token_to_id("[TOOL_CALLS]").map(|id| id as i32);

        Ok(Self {
            cfg,
            embed,
            lm_head,
            final_norm,
            layers,
            yarn_freqs,
            yarn_mscale,
            stop,
            tool_calls_tok,
        })
    }

    /// The per-position "llama 4" query temperature for this step, or `None`
    /// while every position is below `original_max_position_embeddings` --
    /// there the factor is identically 1.0 and the multiply would be waste.
    /// Host-built: `offset` and `seq` are host integers anyway.
    fn llama4_attn_scale(&self, offset: i32, seq: i32) -> Result<Option<Array>> {
        let p = &self.cfg.rope_parameters;
        let Some(beta) = p.llama_4_scaling_beta else {
            return Ok(None);
        };
        let orig = p.original_max_position_embeddings as i32;
        if offset + seq <= orig {
            return Ok(None);
        }
        let scales: Vec<f32> = (0..seq)
            .map(|i| 1.0 + beta * (1.0 + ((offset + i) / orig) as f32).ln())
            .collect();
        let a = Array::from_slice(&scales, &[1, 1, seq, 1]).as_dtype(COMPUTE)?;
        Ok(Some(a))
    }

    /// Run the layer stack, updating `cache`, returning the *pre-final-norm*
    /// hidden states `[tokens, hidden]`. Final norm and LM head live in
    /// `forward_logits`, which projects only the row it keeps.
    fn forward_hidden(&self, ids: &Array, cache: &mut KvCache) -> Result<Array> {
        let seq = ids.dim(0);
        let mut h = self.embed.dequant_rows(ids)?.as_dtype(COMPUTE)?;

        let offset = cache.offset() as i32;
        // Full attention everywhere: causal for multi-token steps, vacuous
        // for single-token decode. No sliding mask geometry in this family.
        let mask = if seq <= 1 {
            MaskKind::None
        } else {
            MaskKind::Causal
        };
        let attn_scale = self.llama4_attn_scale(offset, seq)?;

        for (layer, slot) in self.layers.iter().zip(cache.slots_mut().iter_mut()) {
            h = layer.forward(
                &h,
                &self.cfg,
                offset,
                &mask,
                attn_scale.as_ref(),
                &self.yarn_freqs,
                self.yarn_mscale,
                slot,
            )?;
        }
        cache.advance(seq as usize);
        Ok(h)
    }
}

impl Model for MistralModel {
    fn num_layers(&self) -> usize {
        self.cfg.num_hidden_layers
    }

    fn forward_logits(&self, tokens: &Array, cache: &mut KvCache) -> Result<Array> {
        // Project only the position we keep: a prefill's [seq, vocab] logits
        // are one large quantized matmul of which a single row survives, and
        // MLX's laziness cannot prune inside a single node. Norm and head are
        // row-wise, so slicing first is exact.
        let h = self.forward_hidden(tokens, cache)?;
        let last = h.index(tokens.dim(0) - 1).reshape(&[1, -1])?;
        let last = fast::rms_norm(&last, &self.final_norm, self.cfg.rms_norm_eps)?;
        Ok(self.lm_head.forward(&last)?.index(0))
    }

    /// Mistral v13 chat format, from the checkpoint's chat_template.jinja:
    /// BOS, an optional [SYSTEM_PROMPT] block, the [AVAILABLE_TOOLS] block (the
    /// OpenAI tools array verbatim) before the conversation, then
    /// [INST]-wrapped user turns. Assistant turns render content then any tool
    /// calls as `[TOOL_CALLS]name[ARGS]args`, then </s>; tool results return as
    /// [TOOL_RESULTS]..[/TOOL_RESULTS]. The generation prompt simply ends after
    /// the last block. The family has no thinking mode.
    fn render_prompt(
        &self,
        messages: &[ChatMessage],
        _thinking: bool,
        tools: &[oos_infer::openai::Tool],
    ) -> String {
        let mut p = String::from("<s>");
        let mut rest = messages;
        if let Some(first) = messages.first() {
            if first.role == "system" {
                p.push_str("[SYSTEM_PROMPT]");
                p.push_str(&first.content);
                p.push_str("[/SYSTEM_PROMPT]");
                rest = &messages[1..];
            }
        }
        // Tool declarations precede the conversation: the whole OpenAI tools
        // array serialized as-is is the JSON shape the model was trained to
        // read inside [AVAILABLE_TOOLS].
        if !tools.is_empty() {
            if let Ok(json) = serde_json::to_string(tools) {
                p.push_str("[AVAILABLE_TOOLS]");
                p.push_str(&json);
                p.push_str("[/AVAILABLE_TOOLS]");
            }
        }
        for m in rest {
            match m.role.as_str() {
                "user" => {
                    p.push_str("[INST]");
                    p.push_str(&m.content);
                    p.push_str("[/INST]");
                }
                "assistant" => {
                    // Content, then each call as [TOOL_CALLS]name[ARGS]args,
                    // then the turn-terminating eos -- the template's order.
                    p.push_str(&m.content);
                    if let Some(calls) = &m.tool_calls {
                        for call in calls {
                            p.push_str("[TOOL_CALLS]");
                            p.push_str(&call.function.name);
                            p.push_str("[ARGS]");
                            p.push_str(&call.function.arguments);
                        }
                    }
                    p.push_str("</s>");
                }
                "tool" => {
                    p.push_str("[TOOL_RESULTS]");
                    p.push_str(&m.content);
                    p.push_str("[/TOOL_RESULTS]");
                }
                _ => {}
            }
        }
        p
    }

    fn tool_call_markers(&self) -> Option<crate::models::ToolCallMarkers> {
        // [TOOL_CALLS] opens a call; there is no per-call close (close = None),
        // so a call runs until the next [TOOL_CALLS] or the turn's eos.
        self.tool_calls_tok.map(|open| crate::models::ToolCallMarkers {
            open,
            close: None,
        })
    }

    fn parse_tool_call(&self, span: &str) -> Result<(String, String)> {
        // The captured span (special tokens kept) is `name[ARGS]{json}`; the
        // arguments are already JSON, validated here so a malformed call fails
        // loudly rather than reaching the agent.
        let (name, args) = span
            .split_once("[ARGS]")
            .ok_or_else(|| anyhow!("mistral tool call has no [ARGS] separator: {span:?}"))?;
        let args = args.trim();
        serde_json::from_str::<serde_json::Value>(args)
            .map_err(|e| anyhow!("mistral tool call arguments are not valid JSON: {e}"))?;
        Ok((name.trim().to_string(), args.to_string()))
    }

    fn stop_tokens(&self) -> &[i32] {
        &self.stop
    }
}
