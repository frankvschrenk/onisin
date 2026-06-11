//! NATS Request-Reply transport -- the path other onisin services use to reach
//! the engine. The architecture is NATS-only between services, so this is the
//! internal face; the HTTP server is the external (OpenAI-compatible) one. Both
//! carry the same wire types, so callers choose a transport, not a behaviour.
//!
//! Subjects derive from a prefix (default `oos.cmd.infer`):
//!   - `{prefix}.chat`   request: ChatRequest -> reply: ChatResponse
//!   - `{prefix}.models` request: (ignored)   -> reply: ModelList
//!
//! A failed request replies with `{"error": {"message": ...}}` rather than
//! dropping the reply, so the caller sees the failure instead of timing out.

use std::sync::Arc;

use anyhow::{Context, Result};
use futures::StreamExt;

use crate::complete;
use crate::engine::Engine;
use crate::openai::ChatRequest;

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
                let body = handle_chat(engine, &msg.payload).await;
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
async fn handle_chat(engine: Arc<dyn Engine>, payload: &[u8]) -> Vec<u8> {
    let result = async {
        let req: ChatRequest =
            serde_json::from_slice(payload).context("parsing chat request")?;
        complete::chat(engine, req).await
    }
    .await;

    match result {
        Ok(resp) => serde_json::to_vec(&resp).unwrap_or_else(|e| error_json(&e.to_string())),
        Err(e) => {
            tracing::error!(error = %e, "chat request failed");
            error_json(&e.to_string())
        }
    }
}

/// `{"error": {"message": msg}}` as bytes, matching the HTTP error shape.
fn error_json(msg: &str) -> Vec<u8> {
    serde_json::to_vec(&serde_json::json!({ "error": { "message": msg } })).unwrap_or_default()
}
