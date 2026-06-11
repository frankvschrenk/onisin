//! NATS Request-Reply transport -- the path other onisin services use to reach
//! the engine. The architecture is NATS-only between services, so this is the
//! internal face; the HTTP server is the external (OpenAI-compatible) one. Both
//! carry the same wire types, so callers choose a transport, not a behaviour.
//!
//! Subjects derive from a prefix (default `oos.cmd.infer`):
//!   - `{prefix}.chat`   request: ChatRequest -> reply: ChatResponse
//!   - `{prefix}.models` request: (ignored)   -> reply: ModelList
//!
//! Streaming: a chat request with `stream: true` and a `stream_subject` gets
//! chat.completion.chunk objects (the same ones the HTTP transport frames as
//! SSE) published to that subject while generation runs; the final reply is
//! still the complete ChatResponse, doubling as the completion signal. The
//! client picks the subject (typically inbox-style per request) and
//! subscribes before sending. `stream: true` without a subject falls back to
//! a plain reply, so foreign OpenAI payloads keep working.
//!
//! A failed request replies with `{"error": {"message": ...}}` rather than
//! dropping the reply, so the caller sees the failure instead of timing out.

use std::sync::Arc;

use anyhow::{Context, Result};
use futures::StreamExt;

use crate::complete;
use crate::engine::Engine;
use crate::openai::{ChatChunk, ChatMessage, ChatRequest, ChatResponse, Choice, Usage};

/// Connect to NATS and serve until the process stops.
///
/// Requests are queue-subscribed under `oos-infer` so several replicas of the
/// same model share the load. Each chat runs in its own task so a slow
/// generation doesn't stall the model listing or dispatch of the next request;
/// they still serialise on the engine's own lock, which is what we want on a
/// single accelerator.
pub async fn serve(url: String, prefix: String, engine: Arc<dyn Engine>) -> Result<()> {
    let client = async_nats::connect(&url)
        .await
        .with_context(|| format!("connecting to NATS at {url}"))?;

    let chat_subject = format!("{prefix}.chat");
    let models_subject = format!("{prefix}.models");

    // One wildcard subscription dispatched by subject keeps the wiring to a
    // single queue group and avoids racing two subscribers.
    let mut sub = client
        .queue_subscribe(format!("{prefix}.*"), "oos-infer".to_string())
        .await
        .map_err(|e| anyhow::anyhow!("subscribing to {prefix}.*: {e}"))?;

    tracing::info!(
        %url,
        chat = %chat_subject,
        models = %models_subject,
        available = engine.available_models().len(),
        "oos-infer serving NATS Request-Reply"
    );

    while let Some(msg) = sub.next().await {
        let Some(reply) = msg.reply.clone() else {
            tracing::warn!(subject = %msg.subject, "request without reply subject, ignoring");
            continue;
        };

        if msg.subject.as_str() == chat_subject {
            // Off the dispatch loop: generation is the slow part.
            let client = client.clone();
            let engine = engine.clone();
            tokio::spawn(async move {
                let body = handle_chat(client.clone(), engine, &msg.payload).await;
                if let Err(e) = client.publish(reply, body.into()).await {
                    tracing::error!(error = %e, "publishing chat reply");
                }
            });
        } else if msg.subject.as_str() == models_subject {
            let body = serde_json::to_vec(&complete::models(&engine)).unwrap_or_default();
            if let Err(e) = client.publish(reply, body.into()).await {
                tracing::error!(error = %e, "publishing models reply");
            }
        } else {
            let body = error_json(&format!("unknown subject: {}", msg.subject));
            let _ = client.publish(reply, body.into()).await;
        }
    }

    Ok(())
}

/// Run one chat request, returning the JSON reply bytes (response or error).
async fn handle_chat(
    client: async_nats::Client,
    engine: Arc<dyn Engine>,
    payload: &[u8],
) -> Vec<u8> {
    let req: ChatRequest = match serde_json::from_slice(payload).context("parsing chat request") {
        Ok(req) => req,
        Err(e) => {
            tracing::error!(error = %e, "chat request failed");
            return error_json(&e.to_string());
        }
    };
    if req.stream {
        if let Some(subject) = req.stream_subject.clone().filter(|s| !s.is_empty()) {
            return stream_chat(client, engine, req, subject).await;
        }
    }
    match complete::chat(engine, req).await {
        Ok(resp) => serde_json::to_vec(&resp).unwrap_or_else(|e| error_json(&e.to_string())),
        Err(e) => {
            tracing::error!(error = %e, "chat request failed");
            error_json(&e.to_string())
        }
    }
}

/// Forward chunks to the client's stream subject while assembling the final
/// ChatResponse for the reply from the very same chunks -- one source of
/// truth, no second accounting path.
async fn stream_chat(
    client: async_nats::Client,
    engine: Arc<dyn Engine>,
    req: ChatRequest,
    subject: String,
) -> Vec<u8> {
    let mut rx = complete::chat_stream(engine, req);
    let mut content = String::new();
    let mut reasoning = String::new();
    while let Some(item) = rx.recv().await {
        match item {
            Ok(chunk) => {
                let bytes = serde_json::to_vec(&chunk).unwrap_or_default();
                // Chunk publishes are fire-and-forget: the reply is the
                // authoritative result, a lost chunk only costs smoothness.
                if let Err(e) = client.publish(subject.clone(), bytes.into()).await {
                    tracing::warn!(error = %e, %subject, "publishing stream chunk");
                }
                let ChatChunk {
                    id,
                    created,
                    model,
                    choices,
                    usage,
                    ..
                } = chunk;
                let Some(choice) = choices.into_iter().next() else {
                    continue;
                };
                if let Some(piece) = choice.delta.content {
                    content.push_str(&piece);
                }
                if let Some(piece) = choice.delta.reasoning_content {
                    reasoning.push_str(&piece);
                }
                if let Some(finish) = choice.finish_reason {
                    let resp = ChatResponse {
                        id,
                        object: "chat.completion",
                        created,
                        model,
                        choices: vec![Choice {
                            index: 0,
                            message: ChatMessage {
                                role: "assistant".to_string(),
                                content: std::mem::take(&mut content),
                                reasoning_content: (!reasoning.is_empty())
                                    .then(|| std::mem::take(&mut reasoning)),
                                // Calls arrive whole on the final chunk; the
                                // assembled reply carries them verbatim.
                                tool_calls: choice.delta.tool_calls,
                                tool_call_id: None,
                            },
                            finish_reason: finish,
                        }],
                        usage: usage.unwrap_or(Usage {
                            prompt_tokens: 0,
                            completion_tokens: 0,
                            total_tokens: 0,
                        }),
                    };
                    return serde_json::to_vec(&resp)
                        .unwrap_or_else(|e| error_json(&e.to_string()));
                }
            }
            Err(e) => {
                tracing::error!(error = %e, "streamed chat request failed");
                let body = error_json(&e.to_string());
                // The error goes to both: subscribers of the stream subject
                // must not wait for chunks that will never come.
                let _ = client.publish(subject.clone(), body.clone().into()).await;
                return body;
            }
        }
    }
    error_json("stream ended without a finish chunk")
}

/// `{"error": {"message": msg}}` as bytes, matching the HTTP error shape.
fn error_json(msg: &str) -> Vec<u8> {
    serde_json::to_vec(&serde_json::json!({ "error": { "message": msg } })).unwrap_or_default()
}
