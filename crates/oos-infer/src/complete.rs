//! Transport-agnostic chat completion.
//!
//! Why this lives apart from the HTTP server: the NATS responder needs the
//! exact same request-to-response path, so the mapping (ChatRequest -> Engine
//! -> ChatResponse) and the model listing live here once and both transports
//! call them. A caller picks a transport, never a different behaviour.

use std::sync::Arc;
use std::time::{SystemTime, UNIX_EPOCH};

use crate::engine::{Engine, GenParams};
use crate::openai::{ChatMessage, ChatRequest, ChatResponse, Choice, ModelCard, ModelList, Usage};

/// Run one chat completion against the engine.
///
/// Generation is blocking and compute-bound, so it runs on a blocking task to
/// keep the async runtime free while the GPU works.
pub async fn chat(engine: Arc<dyn Engine>, req: ChatRequest) -> anyhow::Result<ChatResponse> {
    let params = GenParams {
        max_tokens: req.max_tokens.unwrap_or(512),
        temperature: req.temperature.unwrap_or(0.7),
        top_p: req.top_p.unwrap_or(0.95),
    };
    let model = req.model.clone();
    let messages = req.messages;

    let generation = tokio::task::spawn_blocking(move || engine.generate(&messages, &params))
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

/// The single-model listing both transports report.
pub fn models(engine: &Arc<dyn Engine>) -> ModelList {
    ModelList {
        object: "list",
        data: vec![ModelCard {
            id: engine.model_id().to_string(),
            object: "model",
            owned_by: "onisin",
        }],
    }
}

/// Unix seconds, used for response ids and the `created` field.
pub fn now() -> u64 {
    SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .unwrap_or_default()
        .as_secs()
}
