//! NATS request-reply handlers for the generic embedding service.
//!
//! Wire-compatible with the Bun original so existing consumers
//! (oosmem, oosduck) keep working unchanged:
//!
//!   oos.cmd.embed        {text}    -> {vector}        | {error}
//!   oos.cmd.embed.batch  {texts}   -> {vectors}        | {error}
//!   oos.cmd.embed.meta   {}        -> {model, vector_dim}
//!
//! Each subject runs in its own task, and each *message* is handled in
//! a further spawned task: a slow embed round-trip must never block the
//! subscription loop or the other subjects. (Same lesson as the old
//! bench dispatcher, where a sequential `for await` stalled everything
//! behind one slow handler.)

use std::sync::Arc;

use async_nats::{Client, Subscriber};
use futures::StreamExt;
use serde::Deserialize;
use serde_json::json;

use crate::embed::EmbedClient;

#[derive(Deserialize)]
struct EmbedReq {
    text: String,
}

#[derive(Deserialize)]
struct BatchReq {
    texts: Vec<String>,
}

/// Subscribes to the three embedding subjects and spawns their serving
/// tasks. Returns once the subscriptions are live; the tasks run until
/// the process exits.
pub async fn serve(client: Client, embed: Arc<EmbedClient>, vector_dim: usize) -> anyhow::Result<()> {
    let single = client.subscribe("oos.cmd.embed").await?;
    let batch = client.subscribe("oos.cmd.embed.batch").await?;
    let meta = client.subscribe("oos.cmd.embed.meta").await?;

    tokio::spawn(handle_single(client.clone(), embed.clone(), single));
    tokio::spawn(handle_batch(client.clone(), embed.clone(), batch));
    tokio::spawn(handle_meta(client.clone(), meta, vector_dim, embed.model().to_string()));

    println!("[oosai] listening on oos.cmd.embed + .batch + .meta");
    Ok(())
}

async fn handle_single(client: Client, embed: Arc<EmbedClient>, mut sub: Subscriber) {
    while let Some(msg) = sub.next().await {
        let Some(reply) = msg.reply.clone() else { continue };
        let client = client.clone();
        let embed = embed.clone();
        tokio::spawn(async move {
            let payload = match serde_json::from_slice::<EmbedReq>(&msg.payload) {
                Ok(req) if !req.text.is_empty() => match embed.embed(&req.text).await {
                    Ok(vector) => json!({ "vector": vector }),
                    Err(e) => json!({ "error": e.to_string() }),
                },
                Ok(_) => json!({ "error": "text (string) is required" }),
                Err(e) => json!({ "error": format!("bad request: {e}") }),
            };
            reply_json(&client, reply, &payload).await;
        });
    }
}

async fn handle_batch(client: Client, embed: Arc<EmbedClient>, mut sub: Subscriber) {
    while let Some(msg) = sub.next().await {
        let Some(reply) = msg.reply.clone() else { continue };
        let client = client.clone();
        let embed = embed.clone();
        tokio::spawn(async move {
            let payload = match serde_json::from_slice::<BatchReq>(&msg.payload) {
                Ok(req) if !req.texts.is_empty() => {
                    // Sequential: Ollama is single-threaded per model anyway.
                    let mut vectors: Vec<Vec<f32>> = Vec::with_capacity(req.texts.len());
                    let mut failed: Option<String> = None;
                    for text in &req.texts {
                        match embed.embed(text).await {
                            Ok(v) => vectors.push(v),
                            Err(e) => {
                                failed = Some(e.to_string());
                                break;
                            }
                        }
                    }
                    match failed {
                        Some(e) => json!({ "error": e }),
                        None => json!({ "vectors": vectors }),
                    }
                }
                Ok(_) => json!({ "error": "texts (string[]) is required" }),
                Err(e) => json!({ "error": format!("bad request: {e}") }),
            };
            reply_json(&client, reply, &payload).await;
        });
    }
}

async fn handle_meta(client: Client, mut sub: Subscriber, vector_dim: usize, model: String) {
    while let Some(msg) = sub.next().await {
        let Some(reply) = msg.reply.clone() else { continue };
        let payload = json!({ "model": model, "vector_dim": vector_dim });
        reply_json(&client, reply, &payload).await;
    }
}

/// Encodes `payload` as JSON and publishes it to the reply subject.
/// Encoding failures are swallowed after logging — there is nothing
/// useful to send back if our own serialization breaks.
async fn reply_json(client: &Client, reply: async_nats::Subject, payload: &serde_json::Value) {
    match serde_json::to_vec(payload) {
        Ok(bytes) => {
            let _ = client.publish(reply, bytes.into()).await;
        }
        Err(e) => eprintln!("[oosai] reply encode failed: {e}"),
    }
}
