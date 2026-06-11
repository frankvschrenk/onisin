//! Transport-agnostic chat completion.
//!
//! Why this lives apart from the HTTP server: the NATS responder needs the
//! exact same request-to-response path, so the mapping (ChatRequest -> Engine
//! -> ChatResponse) and the model listing live here once and both transports
//! call them. A caller picks a transport, never a different behaviour.

use std::sync::Arc;
use std::time::{SystemTime, UNIX_EPOCH};

use crate::engine::{Engine, GenParams};
use crate::openai::{
    ChatChunk, ChatMessage, ChatRequest, ChatResponse, Choice, ChunkChoice, Delta, ModelCard,
    ModelList, Usage,
};

/// Run one chat completion against the engine.
///
/// Generation is blocking and compute-bound, so it runs on a blocking task to
/// keep the async runtime free while the GPU works.
pub async fn chat(engine: Arc<dyn Engine>, req: ChatRequest) -> anyhow::Result<ChatResponse> {
    let params = GenParams {
        max_tokens: req.max_tokens.unwrap_or(512),
        temperature: req.temperature.unwrap_or(0.7),
        top_p: req.top_p.unwrap_or(0.95),
        thinking: req.enable_thinking,
    };
    let model = req.model.clone();
    let messages = req.messages;

    // The request's model field is the selector; the engine loads it on demand.
    let selected = model.clone();
    let generation =
        tokio::task::spawn_blocking(move || engine.generate(&selected, &messages, &params))
            .await
            .map_err(|e| anyhow::anyhow!("generation task failed: {e}"))??;

    Ok(ChatResponse {
        id: format!("chatcmpl-{}", now()),
        object: "chat.completion",
        created: now(),
        model,
        choices: vec![Choice {
            index: 0,
            message: ChatMessage {
                role: "assistant".to_string(),
                content: generation.text,
                reasoning_content: generation.reasoning,
            },
            finish_reason: "stop".to_string(),
        }],
        usage: Usage {
            prompt_tokens: generation.prompt_tokens,
            completion_tokens: generation.completion_tokens,
            total_tokens: generation.prompt_tokens + generation.completion_tokens,
        },
    })
}

/// Run one chat completion as a stream of OpenAI chat.completion.chunk
/// objects: a role preamble, content deltas as the backend produces them,
/// and a final chunk carrying finish_reason plus usage.
///
/// Chunks are built here, once, so both transports forward them verbatim --
/// SSE frames on HTTP, messages on NATS -- and behave identically. Errors
/// travel in-band on the channel because a stream may fail after it has
/// started, when an HTTP status is no longer available to report it.
pub fn chat_stream(
    engine: Arc<dyn Engine>,
    req: ChatRequest,
) -> tokio::sync::mpsc::Receiver<Result<ChatChunk, anyhow::Error>> {
    // Small buffer: the GPU outruns any consumer rarely, and when a slow
    // consumer fills it, blocking_send simply paces the decode loop.
    let (tx, rx) = tokio::sync::mpsc::channel(32);
    let params = GenParams {
        max_tokens: req.max_tokens.unwrap_or(512),
        temperature: req.temperature.unwrap_or(0.7),
        top_p: req.top_p.unwrap_or(0.95),
        thinking: req.enable_thinking,
    };
    let id = format!("chatcmpl-{}", now());
    let created = now();
    let model = req.model;
    let messages = req.messages;

    tokio::task::spawn_blocking(move || {
        let chunk = |delta: Delta, finish: Option<String>, usage: Option<Usage>| ChatChunk {
            id: id.clone(),
            object: "chat.completion.chunk",
            created,
            model: model.clone(),
            choices: vec![ChunkChoice {
                index: 0,
                delta,
                finish_reason: finish,
            }],
            usage,
        };

        // Role preamble first, as OpenAI streams it. Send errors mean the
        // consumer is gone; generation still runs to completion (request
        // cancellation is a later refinement), so they are ignored.
        let _ = tx.blocking_send(Ok(chunk(
            Delta {
                role: Some("assistant".to_string()),
                ..Delta::default()
            },
            None,
            None,
        )));

        let mut emit = |piece: &str, reasoning: bool| {
            let delta = if reasoning {
                Delta {
                    reasoning_content: Some(piece.to_string()),
                    ..Delta::default()
                }
            } else {
                Delta {
                    content: Some(piece.to_string()),
                    ..Delta::default()
                }
            };
            let _ = tx.blocking_send(Ok(chunk(delta, None, None)));
        };
        match engine.generate_streamed(&model, &messages, &params, &mut emit) {
            Ok(generation) => {
                let _ = tx.blocking_send(Ok(chunk(
                    Delta::default(),
                    Some("stop".to_string()),
                    Some(Usage {
                        prompt_tokens: generation.prompt_tokens,
                        completion_tokens: generation.completion_tokens,
                        total_tokens: generation.prompt_tokens + generation.completion_tokens,
                    }),
                )));
            }
            Err(e) => {
                let _ = tx.blocking_send(Err(e));
            }
        }
    });

    rx
}

/// The model listing both transports report -- whatever the engine has
/// available locally (the local Hugging Face cache, by default).
pub fn models(engine: &Arc<dyn Engine>) -> ModelList {
    ModelList {
        object: "list",
        data: engine
            .available_models()
            .into_iter()
            .map(|id| ModelCard {
                id,
                object: "model",
                owned_by: "onisin",
            })
            .collect(),
    }
}

/// Unix seconds, used for response ids and the `created` field.
pub fn now() -> u64 {
    SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .unwrap_or_default()
        .as_secs()
}
