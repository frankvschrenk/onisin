//! Model families behind the MLX backend.
//!
//! oosmlx is not tied to one model: `load` reads the architecture from the
//! checkpoint's config.json and dispatches to a family that implements the
//! `Model` trait. Gemma 3 is the first family (it was the bootstrap target for
//! bringing the engine up); adding another -- Llama, Mistral, a newer Gemma --
//! is one new module plus one match arm in `load`, with nothing above this
//! trait changing. Unknown architectures fail loudly instead of misbehaving.
//!
//! Everything here is MLX-specific (it operates on `mlx_rs::Array`), so the
//! whole module sits behind the `mlx` feature; the sampling and KV-cache parts
//! are family-agnostic and shared by every `Model`.

mod gemma3;
mod gemma4;

use std::path::Path;

use anyhow::{bail, Context, Result};
use mlx_rs::{ops, Array};
use oos_infer::openai::ChatMessage;
use oos_infer::ModelFiles;
use tokenizers::Tokenizer;

/// A loaded model family. Adding a family means adding an impl plus a dispatch
/// arm in [`load`]; the transports, API and decode loop above never change.
pub trait Model: Send {
    /// Number of transformer layers, used to size the KV cache.
    fn num_layers(&self) -> usize;

    /// Logits `[vocab]` for the next position, advancing `cache`. On the first
    /// call `tokens` is the whole prompt (prefill); on later calls it is the
    /// single most recent token.
    fn forward_logits(&self, tokens: &[i32], cache: &mut KvCache) -> Result<Array>;

    /// Render this family's chat prompt for the given messages.
    fn render_prompt(&self, messages: &[ChatMessage]) -> String;

    /// Token ids that stop generation (eos plus any turn terminator).
    fn stop_tokens(&self) -> &[i32];
}

/// Per-layer key/value cache for incremental decoding.
///
/// Each slot holds one layer's K/V post-RoPE in head-major `[1, nkv, past, hd]`;
/// a decode step appends the new position and attends over the whole history.
/// Owned by the decode loop, so a `Model` stays stateless and shareable.
pub struct KvCache {
    layers: Vec<Option<(Array, Array)>>,
    offset: usize,
}

impl KvCache {
    pub fn new(num_layers: usize) -> Self {
        Self {
            layers: (0..num_layers).map(|_| None).collect(),
            offset: 0,
        }
    }

    /// Positions already cached; also the RoPE/mask offset for the next step.
    pub fn offset(&self) -> usize {
        self.offset
    }

    /// Advance the position counter after a step processed `n` tokens.
    pub fn advance(&mut self, n: usize) {
        self.offset += n;
    }

    /// The per-layer slots, for a model to append this step's K/V into.
    pub fn slots_mut(&mut self) -> &mut [Option<(Array, Array)>] {
        &mut self.layers
    }
}

/// Choose the next token id from a logit row: greedy argmax when `temperature`
/// is non-positive (deterministic), otherwise temperature + top-p sampling.
pub fn pick(logits: &Array, temperature: f32, top_p: f32) -> Result<i32> {
    if temperature <= 0.0 {
        let next = ops::indexing::argmax(logits, false)?;
        Ok(next.item::<u32>() as i32)
    } else {
        logits.eval()?;
        Ok(sample_top_p(logits.as_slice::<f32>(), temperature, top_p))
    }
}

/// Top-p (nucleus) sampling over one logit row, with temperature.
///
/// Done on the CPU: trivial to read against a reference, and the per-token cost
/// (one softmax + one sort of the vocab) is negligible next to the forward
/// pass. On-device sampling is a later optimisation. `temperature` is > 0 here.
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

#[derive(serde::Deserialize)]
struct ArchPeek {
    #[serde(default)]
    model_type: Option<String>,
    #[serde(default)]
    architectures: Option<Vec<String>>,
}

/// Read the model family from config.json: prefer the first `architectures`
/// entry (e.g. "Gemma3ForCausalLM"), fall back to `model_type`.
fn detect_arch(config_json: &Path) -> Result<String> {
    let text = std::fs::read_to_string(config_json)
        .with_context(|| format!("reading {}", config_json.display()))?;
    let peek: ArchPeek = serde_json::from_str(&text)
        .with_context(|| format!("parsing {}", config_json.display()))?;
    peek.architectures
        .and_then(|v| v.into_iter().next())
        .or(peek.model_type)
        .context("config.json has neither `architectures` nor `model_type`")
}

/// Load the model for `files`, dispatching on its architecture.
pub fn load(files: &ModelFiles, tokenizer: &Tokenizer) -> Result<Box<dyn Model>> {
    let arch = detect_arch(&files.config_json)?;
    let lower = arch.to_lowercase();
    if lower.contains("gemma4") {
        Ok(Box::new(gemma4::Gemma4Model::load(files, tokenizer)?))
    } else if lower.contains("gemma3") {
        Ok(Box::new(gemma3::Gemma3Model::load(files, tokenizer)?))
    } else {
        bail!("unsupported model architecture: {arch} (supported: gemma3, gemma4)")
    }
}
