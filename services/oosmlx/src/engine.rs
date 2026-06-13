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
#[cfg(feature = "mlx")]
use oos_infer::openai::{ToolCall, ToolCallFunction};
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
    // Prompt-prefix KV cache from this model's last generic-loop request,
    // frozen at the prompt end. Interior mutability so it can be swapped while
    // `model` and `tokenizer` are borrowed for the same generation. Reset
    // whenever the model is (re)loaded, so a snapshot never crosses models.
    prefix: std::cell::RefCell<Option<PrefixCache>>,
}

/// A previous request's KV cache frozen at its prompt end, with the exact
/// token ids it covers. The next request resumes from it when its prompt
/// extends these ids verbatim, prefilling only the new tail; see
/// [`run_generation`].
#[cfg(feature = "mlx")]
struct PrefixCache {
    prompt_ids: Vec<i32>,
    cache: crate::models::KvCache,
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
        prefix: std::cell::RefCell::new(None),
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
            tool_calls: Vec::new(),
        })
    }
}

/// Whether prompt-prefix cache reuse is on (the default) or disabled via
/// `OOSMLX_PREFIX_CACHE=0|off|false|no`. An escape hatch: it isolates the
/// feature for an A/B against the no-reuse path, and disables it outright
/// should a model ever prove the snapshot unsafe.
#[cfg(feature = "mlx")]
fn prefix_cache_enabled() -> bool {
    !matches!(
        std::env::var("OOSMLX_PREFIX_CACHE").ok().as_deref(),
        Some("0" | "off" | "false" | "no")
    )
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

    let prompt = model.render_prompt(messages, params.thinking, &params.tools);
    // Reusable-prefix boundary for the prompt cache: the leading bytes a later
    // turn reproduces verbatim as history. Any remainder is generation-only
    // scaffolding (e.g. gemma4's empty thought-channel prefill); because it
    // begins at a special-token boundary it tokenizes independently, so its
    // token count subtracts cleanly from the prompt to give the freeze point.
    let reuse_bytes = model.reusable_prefix_len(&prompt);
    let nonreusable_suffix =
        (reuse_bytes < prompt.len()).then(|| prompt[reuse_bytes..].to_string());
    let encoding = tokenizer
        .encode(prompt, false)
        .map_err(|e| anyhow!("tokenize: {e}"))?;
    let prompt_ids: Vec<i32> = encoding.get_ids().iter().map(|&u| u as i32).collect();
    let prompt_tokens = prompt_ids.len();
    let reuse_boundary = match &nonreusable_suffix {
        Some(suffix) => {
            let suf = tokenizer
                .encode(suffix.as_str(), false)
                .map_err(|e| anyhow!("tokenize suffix: {e}"))?;
            prompt_ids.len().saturating_sub(suf.get_ids().len())
        }
        None => prompt_ids.len(),
    };

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
    // Tool calls force the generic loop: the accelerated path returns its
    // tokens only as a whole, while call capture needs the per-token stream
    // (and the stop-after-calls rule below).
    let accelerated = if params.temperature <= 0.0 && emit.is_none() && params.tools.is_empty() {
        model.generate_greedy(&prompt_ids, params.max_tokens)?
    } else {
        None
    };
    let toolmark = model.tool_call_markers();
    // Captured `open..close` token spans, markers excluded; parsed into
    // structured calls after the loop.
    let mut tool_spans: Vec<Vec<u32>> = Vec::new();
    let mut in_tool_call = false;
    let out: Vec<u32> = match accelerated {
        Some(tokens) => tokens,
        None => {
            // Prefix reuse: a previous generic-loop request on this model left
            // its prompt-end KV snapshot here. When this prompt extends that
            // one verbatim, resume from the snapshot and prefill only the new
            // tail -- agent and chat loops re-send a growing prompt whose head
            // is unchanged, and re-prefilling it dominates per-turn latency.
            // Reuse only ever extends, never rewinds: a sliding-window
            // family's rotating slots resume forward from the frozen end,
            // sidestepping the prefix-trim a wrapped ring cannot do (and that
            // mlx-lm leaves unsolved for hybrid models, #980).
            let prefix_enabled = prefix_cache_enabled();
            let prior = if prefix_enabled {
                resident.prefix.borrow_mut().take()
            } else {
                None
            };
            let (mut cache, mut start) = match prior {
                Some(p)
                    if p.prompt_ids.len() < prompt_ids.len()
                        && prompt_ids[..p.prompt_ids.len()] == p.prompt_ids[..] =>
                {
                    let at = p.prompt_ids.len();
                    (p.cache, at)
                }
                _ => (crate::models::KvCache::new(model.num_layers()), 0usize),
            };
            let reused = start;
            let stop = model.stop_tokens();
            let mut out: Vec<u32> = Vec::new();
            let mut in_reasoning = false;
            // Phase timing: prefill ends when the first token is synced to
            // the host, everything after is decode. Logged per request so
            // prefill and decode throughput stay separately comparable
            // against other runtimes.
            let t0 = std::time::Instant::now();
            let mut prefill: Option<std::time::Duration> = None;
            // Chunked prefill: a long prompt runs through the layers in
            // fixed-size pieces, the last piece staying with the loop so its
            // logits feed the first pick. Per-chunk sliding masks stay small
            // ([chunk, window+chunk] instead of [prompt, prompt]) and the
            // eval at each boundary bounds the graph and peak memory; mlx_lm
            // prefills the same way (prefill_step_size). Each chunk's single
            // logit row is one cheap qmv -- the price of needing no extra
            // trait surface.
            let chunk = std::env::var("OOSMLX_PREFILL_CHUNK")
                .ok()
                .and_then(|v| v.parse().ok())
                .unwrap_or(2048usize)
                .max(1);
            while reuse_boundary - start > chunk {
                let piece =
                    mlx_rs::Array::from_slice(&prompt_ids[start..start + chunk], &[chunk as i32]);
                model.forward_logits(&piece, &mut cache)?.eval()?;
                start += chunk;
            }
            // Pipelined decode in the mlx_lm shape: the pick stays a lazy
            // device array, and step n+1's graph is built and kicked with
            // async_eval *before* step n's token is synced to the host -- the
            // GPU starts n+1 while the host routes n instead of idling on the
            // per-token sync. The lookahead past a stop token costs one
            // speculative forward, amortized over the whole completion; the
            // cache it touches is request-local.
            //
            // The cache is frozen for the next request at the reusable
            // boundary, before decode (or the generation-only scaffolding past
            // it) mutates it -- a shallow clone the first later write
            // copy-on-writes around. When the boundary is the whole prompt the
            // freeze sits after the last prompt token and that forward yields
            // the first-pick logits; otherwise the scaffolding tail past the
            // boundary yields them.
            let logits = if reuse_boundary == prompt_ids.len() {
                let tail = mlx_rs::Array::from_slice(
                    &prompt_ids[start..],
                    &[(prompt_ids.len() - start) as i32],
                );
                let logits = model.forward_logits(&tail, &mut cache)?;
                if prefix_enabled {
                    *resident.prefix.borrow_mut() = Some(PrefixCache {
                        prompt_ids: prompt_ids[..reuse_boundary].to_vec(),
                        cache: cache.snapshot(),
                    });
                }
                logits
            } else {
                if reuse_boundary > start {
                    let piece = mlx_rs::Array::from_slice(
                        &prompt_ids[start..reuse_boundary],
                        &[(reuse_boundary - start) as i32],
                    );
                    model.forward_logits(&piece, &mut cache)?.eval()?;
                }
                if prefix_enabled {
                    *resident.prefix.borrow_mut() = Some(PrefixCache {
                        prompt_ids: prompt_ids[..reuse_boundary].to_vec(),
                        cache: cache.snapshot(),
                    });
                }
                let tail = mlx_rs::Array::from_slice(
                    &prompt_ids[reuse_boundary..],
                    &[(prompt_ids.len() - reuse_boundary) as i32],
                );
                model.forward_logits(&tail, &mut cache)?
            };
            let mut cur = crate::models::pick(&logits, params.temperature, params.top_p)?;
            if params.max_tokens > 0 {
                mlx_rs::transforms::async_eval([&cur])?;
            }
            for n in 0..params.max_tokens {
                let ahead = if n + 1 < params.max_tokens {
                    let logits = model.forward_logits(&cur, &mut cache)?;
                    let t = crate::models::pick(&logits, params.temperature, params.top_p)?;
                    mlx_rs::transforms::async_eval([&t])?;
                    Some(t)
                } else {
                    None
                };
                let next = cur.item::<i32>();
                if prefill.is_none() {
                    prefill = Some(t0.elapsed());
                }
                // Once the model has issued calls and continues with anything
                // that is not another call, the turn is the runtime's: stop
                // and hand the calls back. The dangling token -- a primed
                // <|tool_response>, a turn end or eos -- is discarded.
                let opens_call = toolmark.map(|t| next == t.open).unwrap_or(false);
                if !in_tool_call && !tool_spans.is_empty() && !opens_call {
                    break;
                }
                if stop.contains(&next) {
                    break;
                }
                out.push(next as u32);
                if let Some(t) = ahead {
                    cur = t;
                }
                // Token routing; `break 'route` is what `continue` was before
                // the pipelined rewrite (the loop tail is empty either way).
                'route: {
                    if let Some(t) = toolmark {
                        if next == t.open {
                            in_tool_call = true;
                            tool_spans.push(Vec::new());
                            break 'route;
                        }
                        if in_tool_call {
                            if t.close == Some(next) {
                                in_tool_call = false;
                            } else if let Some(span) = tool_spans.last_mut() {
                                span.push(next as u32);
                            }
                            break 'route;
                        }
                    }
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
            }
            let total = t0.elapsed();
            let prefill = prefill.unwrap_or(total);
            let decoded = out.len().saturating_sub(1).max(1) as f64;
            tracing::info!(
                prompt_tokens,
                cached_prefix = reused,
                completion_tokens = out.len(),
                prefill_ms = prefill.as_millis() as u64,
                prefill_tps =
                    (prompt_tokens as f64 / prefill.as_secs_f64().max(1e-9)).round() as u64,
                decode_tps = (decoded / total.saturating_sub(prefill).as_secs_f64().max(1e-9))
                    .round() as u64,
                "generation phases"
            );
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

    // Parse the captured call spans into structured calls. For a family with
    // an explicit close token, a span still open at the end was truncated by
    // the token budget and is dropped (finish then reports "length"). For a
    // family whose calls run to the turn's end (close = None, mistral), the
    // final open span is that last complete call and is kept. A completed span
    // that fails to parse is a model-side glitch surfaced as an error, since
    // silently dropping a call would derail an agent loop.
    let drop_truncated = in_tool_call && toolmark.and_then(|t| t.close).is_some();
    let complete_spans = if drop_truncated {
        &tool_spans[..tool_spans.len() - 1]
    } else {
        &tool_spans[..]
    };
    let mut tool_calls: Vec<ToolCall> = Vec::with_capacity(complete_spans.len());
    for (i, span) in complete_spans.iter().enumerate() {
        // Special tokens kept: the grammar's <|\"|> quote marker must survive
        // decoding for the parser to see string boundaries.
        let raw = tokenizer
            .decode(span, false)
            .map_err(|e| anyhow!("detokenize tool call: {e}"))?;
        let (name, arguments) = model.parse_tool_call(&raw)?;
        tool_calls.push(ToolCall {
            id: format!("call_{i}"),
            kind: "function".to_string(),
            function: ToolCallFunction { name, arguments },
        });
    }
    // Tool blocks travel as structured calls, not as text: strip them from
    // the raw stream before the content/reasoning split.
    let text_toks = match toolmark {
        Some(t) if !tool_spans.is_empty() => {
            strip_tool_spans(&out, t.open as u32, t.close.map(|c| c as u32))
        }
        _ => out.clone(),
    };

    // The answer and the reasoning decode from the split streams; `out`
    // (markers and channel-name line included) stays the accounting basis.
    let decode = |toks: &[u32]| {
        tokenizer
            .decode(toks, true)
            .map_err(|e| anyhow!("detokenize: {e}"))
    };
    let (text, reasoning) = match channel {
        Some(ch) => {
            let (content_toks, reasoning_toks) = split_channels(&text_toks, ch);
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
        None => (decode(&text_toks)?, None),
    };

    // "tool_calls" when the model requested tools; otherwise "length" when
    // the token budget ran out and "stop" when a stop token ended the
    // sequence early. The length rule on the output covers both the generic
    // loop and the accelerated greedy path: neither emits the stop token
    // itself, so a full budget means no stop token was seen.
    let finish = if !tool_calls.is_empty() {
        "tool_calls"
    } else if out.len() < params.max_tokens {
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
        tool_calls,
    })
}

/// Remove `open..close` tool-call blocks (markers included) from a raw
/// completion; the calls travel separately as structured ToolCalls, so
/// nothing of them belongs in the decoded text.
#[cfg(feature = "mlx")]
fn strip_tool_spans(out: &[u32], open: u32, close: Option<u32>) -> Vec<u32> {
    let mut kept = Vec::with_capacity(out.len());
    let mut inside = false;
    for &tok in out {
        if tok == open {
            inside = true;
        } else if inside {
            // With a close token each block ends at it; without one (mistral),
            // the calls run to the turn's end, so once inside we stay inside.
            if close == Some(tok) {
                inside = false;
            }
        } else {
            kept.push(tok);
        }
    }
    kept
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
