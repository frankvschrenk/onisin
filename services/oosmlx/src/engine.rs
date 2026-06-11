//! The MLX-backed Engine -- a lazy, single-resident model manager.
//!
//! Why a manager and not one fixed model: OpenAI/Ollama clients pick a model
//! per request, so the engine loads the requested model on demand and keeps it
//! resident, swapping when a different one is asked for. Only one model is held
//! at a time because these are large (a 26B is ~15 GB); a keep-alive or
//! N-resident policy can layer on later. With `--features mlx` this runs a real
//! forward pass; without it, it loads nothing and returns a placeholder so
//! non-Apple/CI builds stay green. The API and the ooscuda contract are the
//! same either way.

use anyhow::Result;
use oos_infer::engine::{Engine, GenParams, Generation};
use oos_infer::openai::ChatMessage;
use oos_infer::registry;

#[cfg(feature = "mlx")]
use anyhow::{anyhow, Context};
#[cfg(feature = "mlx")]
use oos_infer::ModelRef;
#[cfg(feature = "mlx")]
use tokenizers::Tokenizer;

pub struct MlxEngine {
    // The one resident model, loaded on first use and swapped on a different
    // request id. Behind a mutex so generations (and swaps) serialise, which is
    // what we want on a single accelerator.
    #[cfg(feature = "mlx")]
    resident: std::sync::Mutex<Option<Resident>>,
}

#[cfg(feature = "mlx")]
struct Resident {
    id: String,
    tokenizer: Tokenizer,
    model: Box<dyn crate::models::Model>,
}

impl MlxEngine {
    /// A fresh engine with nothing loaded; models load on first request.
    pub fn new() -> Self {
        Self {
            #[cfg(feature = "mlx")]
            resident: std::sync::Mutex::new(None),
        }
    }

    /// Eagerly load a model so the first request is warm (used by `--preload`).
    #[cfg(feature = "mlx")]
    pub fn preload(&self, model: &str) -> Result<()> {
        let resident = load_resident(model)?;
        *self
            .resident
            .lock()
            .map_err(|_| anyhow!("resident mutex poisoned"))? = Some(resident);
        Ok(())
    }

    #[cfg(not(feature = "mlx"))]
    pub fn preload(&self, _model: &str) -> Result<()> {
        Ok(())
    }
}

impl Default for MlxEngine {
    fn default() -> Self {
        Self::new()
    }
}

/// Resolve a model id (HF repo or local path), then load its tokenizer and the
/// architecture-dispatched MLX model.
#[cfg(feature = "mlx")]
fn load_resident(id: &str) -> Result<Resident> {
    let model_ref = ModelRef::parse(id);
    tracing::info!(?model_ref, "loading model");
    let files = registry::resolve(&model_ref).context("resolving model files")?;
    let tokenizer = Tokenizer::from_file(&files.tokenizer_json)
        .map_err(|e| anyhow!("loading tokenizer {}: {e}", files.tokenizer_json.display()))?;
    let model = crate::models::load(&files, &tokenizer)?;
    tracing::info!(model = id, layers = model.num_layers(), "model loaded");
    Ok(Resident {
        id: id.to_string(),
        tokenizer,
        model,
    })
}

impl Engine for MlxEngine {
    fn available_models(&self) -> Vec<String> {
        let mut ids = registry::list_hf_cache_models();
        // A resident model loaded from a local path won't be in the HF cache;
        // surface it too.
        #[cfg(feature = "mlx")]
        if let Ok(guard) = self.resident.lock() {
            if let Some(resident) = guard.as_ref() {
                if !ids.contains(&resident.id) {
                    ids.push(resident.id.clone());
                }
            }
        }
        ids.sort();
        ids.dedup();
        ids
    }

    #[cfg(feature = "mlx")]
    fn generate(
        &self,
        model: &str,
        messages: &[ChatMessage],
        params: &GenParams,
    ) -> Result<Generation> {
        run_generation(self, model, messages, params, None)
    }

    #[cfg(feature = "mlx")]
    fn generate_streamed(
        &self,
        model: &str,
        messages: &[ChatMessage],
        params: &GenParams,
        emit: &mut (dyn FnMut(&str, bool) + Send),
    ) -> Result<Generation> {
        run_generation(self, model, messages, params, Some(emit))
    }

    #[cfg(not(feature = "mlx"))]
    fn generate(
        &self,
        model: &str,
        messages: &[ChatMessage],
        _params: &GenParams,
    ) -> Result<Generation> {
        // The trait's generate_streamed default emits this placeholder as one
        // chunk, so the non-MLX build streams correctly too.
        let user = messages
            .iter()
            .rev()
            .find(|m| m.role == "user")
            .map(|m| m.content.as_str())
            .unwrap_or("");
        let prompt_tokens = user.split_whitespace().count();
        let text = format!(
            "[oosmlx] built without the `mlx` feature: requested model `{model}` was not run \
             (the MLX forward pass is not compiled in). Rebuild with `--features mlx`."
        );
        Ok(Generation {
            text,
            reasoning: None,
            prompt_tokens,
            completion_tokens: 0,
            finish: "stop".to_string(),
        })
    }
}

/// The one generation path behind both Engine entry points: load or swap the
/// resident model, render and encode the prompt, decode, and -- when `emit`
/// is given -- stream incremental text along the way.
#[cfg(feature = "mlx")]
fn run_generation(
    engine: &MlxEngine,
    model: &str,
    messages: &[ChatMessage],
    params: &GenParams,
    mut emit: Option<&mut (dyn FnMut(&str, bool) + Send)>,
) -> Result<Generation> {
    let mut guard = engine
        .resident
        .lock()
        .map_err(|_| anyhow!("resident mutex poisoned"))?;

    // Load on demand. On a different id, drop the current model *before*
    // loading the next so we never hold two large models at once (at the
    // cost of losing the warm one if the new load fails).
    let needs_load = guard.as_ref().map(|r| r.id != model).unwrap_or(true);
    if needs_load {
        *guard = None;
        *guard = Some(load_resident(model)?);
    }
    let resident = guard.as_ref().expect("resident set above");
    let model = &resident.model;
    let tokenizer = &resident.tokenizer;

    let prompt = model.render_prompt(messages, params.thinking);
    let encoding = tokenizer
        .encode(prompt, false)
        .map_err(|e| anyhow!("tokenize: {e}"))?;
    let prompt_ids: Vec<i32> = encoding.get_ids().iter().map(|&u| u as i32).collect();
    let prompt_tokens = prompt_ids.len();

    let channel = model.reasoning_channel();
    // The `{name}\n` line the model opens its channel with; stripped from
    // streamed reasoning the same way the final split strips it.
    let name_line = channel.map(|ch| format!("{}\n", ch.name));

    // Greedy requests take a model-specific accelerated path when the
    // family provides one (speculative decoding); `None` falls back to
    // the generic per-token loop below. Streamed requests always take
    // the generic loop: the accelerated path returns its tokens only as
    // a whole, and per-block emission is a later refinement of what is
    // an opt-in feature anyway.
    let mut emitted_content = 0usize;
    let mut emitted_reasoning = 0usize;
    // Streaming views of the two channels: deltas must decode per channel,
    // so answer and reasoning tokens are routed apart as they appear, while
    // `out` keeps the raw sequence (markers included) for token accounting
    // and the final split.
    let mut content_view: Vec<u32> = Vec::new();
    let mut reasoning_view: Vec<u32> = Vec::new();
    let accelerated = if params.temperature <= 0.0 && emit.is_none() {
        model.generate_greedy(&prompt_ids, params.max_tokens)?
    } else {
        None
    };
    let out: Vec<u32> = match accelerated {
        Some(tokens) => tokens,
        None => {
            let mut cache = crate::models::KvCache::new(model.num_layers());
            let stop = model.stop_tokens();
            let mut step = prompt_ids;
            let mut out: Vec<u32> = Vec::new();
            let mut in_reasoning = false;
            for _ in 0..params.max_tokens {
                let logits = model.forward_logits(&step, &mut cache)?;
                let next = crate::models::pick(&logits, params.temperature, params.top_p)?;
                if stop.contains(&next) {
                    break;
                }
                out.push(next as u32);
                step = vec![next];
                match channel {
                    Some(ch) if next == ch.open => in_reasoning = true,
                    Some(ch) if next == ch.close => in_reasoning = false,
                    _ => {
                        if let Some(emit) = emit.as_deref_mut() {
                            if in_reasoning {
                                reasoning_view.push(next as u32);
                                emitted_reasoning = stream_delta(
                                    tokenizer,
                                    &reasoning_view,
                                    emitted_reasoning,
                                    name_line.as_deref(),
                                    true,
                                    emit,
                                )?;
                            } else {
                                content_view.push(next as u32);
                                emitted_content = stream_delta(
                                    tokenizer,
                                    &content_view,
                                    emitted_content,
                                    None,
                                    false,
                                    emit,
                                )?;
                            }
                        }
                    }
                }
            }
            out
        }
    };

    // Final flushes: one more delta per channel releases anything held back
    // mid-stream (a tail that only became a complete character at the end).
    if let Some(emit) = emit.as_deref_mut() {
        if !reasoning_view.is_empty() {
            stream_delta(
                tokenizer,
                &reasoning_view,
                emitted_reasoning,
                name_line.as_deref(),
                true,
                emit,
            )?;
        }
        stream_delta(tokenizer, &content_view, emitted_content, None, false, emit)?;
    }

    // The answer and the reasoning decode from the split streams; `out`
    // (markers and channel-name line included) stays the accounting basis.
    let decode = |toks: &[u32]| {
        tokenizer
            .decode(toks, true)
            .map_err(|e| anyhow!("detokenize: {e}"))
    };
    let (text, reasoning) = match channel {
        Some(ch) => {
            let (content_toks, reasoning_toks) = split_channels(&out, ch);
            let text = decode(&content_toks)?;
            let reasoning = if reasoning_toks.is_empty() {
                None
            } else {
                let raw = decode(&reasoning_toks)?;
                let stripped = name_line
                    .as_deref()
                    .and_then(|p| raw.strip_prefix(p))
                    .unwrap_or(&raw)
                    .trim()
                    .to_string();
                (!stripped.is_empty()).then_some(stripped)
            };
            (text, reasoning)
        }
        None => (decode(&out)?, None),
    };

    // "length" when the token budget ran out, "stop" when a stop token ended
    // the sequence early. The uniform rule on the output length covers both
    // the generic loop and the accelerated greedy path: neither emits the
    // stop token itself, so a full budget means no stop token was seen.
    let finish = if out.len() < params.max_tokens {
        "stop"
    } else {
        "length"
    };

    Ok(Generation {
        text,
        reasoning,
        prompt_tokens,
        completion_tokens: out.len(),
        finish: finish.to_string(),
    })
}

/// Split a raw completion into (content, reasoning) token streams along the
/// channel markers, the markers themselves belonging to neither.
#[cfg(feature = "mlx")]
fn split_channels(out: &[u32], ch: crate::models::ReasoningChannel) -> (Vec<u32>, Vec<u32>) {
    let mut content = Vec::with_capacity(out.len());
    let mut reasoning = Vec::new();
    let mut in_reasoning = false;
    for &tok in out {
        if tok as i32 == ch.open {
            in_reasoning = true;
        } else if tok as i32 == ch.close {
            in_reasoning = false;
        } else if in_reasoning {
            reasoning.push(tok);
        } else {
            content.push(tok);
        }
    }
    (content, reasoning)
}

/// Decode the accumulated tokens and emit the not-yet-emitted suffix.
///
/// Deltas come from re-decoding the whole sequence because BPE pieces do not
/// map 1:1 to characters: a multi-byte character can span tokens, and the
/// tokenizer renders an incomplete tail as U+FFFD. Such a tail (and the rare
/// non-prefix re-decode) is held back; the next token, or the final flush in
/// the caller, completes it. Re-decoding is O(n) per token but microseconds
/// against a ~40ms forward pass, so a stateful detokenizer is not worth its
/// complexity here.
#[cfg(feature = "mlx")]
fn stream_delta(
    tokenizer: &Tokenizer,
    out: &[u32],
    mut emitted: usize,
    skip_prefix: Option<&str>,
    reasoning: bool,
    emit: &mut (dyn FnMut(&str, bool) + Send),
) -> Result<usize> {
    let text = tokenizer
        .decode(out, true)
        .map_err(|e| anyhow!("detokenize: {e}"))?;
    // A channel opens with its `{name}\n` line; that line is structure, not
    // reasoning, so it is skipped once complete. Until then (or if the model
    // deviates from the format) nothing special happens.
    if emitted == 0 {
        if let Some(prefix) = skip_prefix {
            if text.len() < prefix.len() && prefix.starts_with(text.as_str()) {
                return Ok(0);
            }
            if text.starts_with(prefix) {
                emitted = prefix.len();
            }
        }
    }
    if text.len() <= emitted || !text.is_char_boundary(emitted) {
        return Ok(emitted);
    }
    let delta = &text[emitted..];
    if delta.ends_with('\u{FFFD}') {
        return Ok(emitted);
    }
    emit(delta, reasoning);
    Ok(text.len())
}
