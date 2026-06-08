//! oos.cmd.search — schema RAG retrieval for the oos agent.
//!
//! Embeds the query and returns the nearest oos_domain_schema chunks by
//! cosine distance. This backs the agent's `oos_schema_search` tool: the
//! domain index (oos.cmd.domains) only tells the LLM that a domain
//! exists; this hands back the full rendered chunk on demand, keeping
//! the system prompt lean.
//!
//! Why its own module rather than a command_handler arm: search needs
//! the embed client, not just the pool — the same split that keeps
//! nats_embed independent. It still joins the oosai-cmd queue group so a
//! request is handled exactly once across processes.

use std::sync::Arc;

use async_nats::Client;
use futures::StreamExt;
use serde_json::{json, Value};
use sqlx::{PgPool, Row};

use anyhow::{anyhow, Result};

use crate::embed::EmbedClient;
use crate::vector::format_vector;

/// Shared queue group, same as command_handler.
const QUEUE: &str = "oosai-cmd";

/// Hit-count default and ceiling, matching the Bun handler.
const DEFAULT_LIMIT: i64 = 5;
const MAX_LIMIT: i64 = 50;

/// Subscribes oos.cmd.search in the queue group and serves it until the
/// process exits. Each request is dispatched in its own task so a slow
/// embed call never stalls the subscription.
pub async fn serve(client: Client, pool: PgPool, embed: Arc<EmbedClient>) -> Result<()> {
    let mut sub = client
        .queue_subscribe("oos.cmd.search".to_string(), QUEUE.to_string())
        .await?;
    tokio::spawn(async move {
        while let Some(msg) = sub.next().await {
            let Some(reply) = msg.reply.clone() else { continue };
            let client = client.clone();
            let pool = pool.clone();
            let embed = embed.clone();
            tokio::spawn(async move {
                let value = match handle(&pool, &embed, &msg.payload).await {
                    Ok(v) => v,
                    Err(e) => json!({ "ok": false, "error": e.to_string() }),
                };
                if let Ok(bytes) = serde_json::to_vec(&value) {
                    let _ = client.publish(reply, bytes.into()).await;
                }
            });
        }
    });
    println!("[oosai] listening on oos.cmd.search (queue {QUEUE})");
    Ok(())
}

/// Embeds the query and returns the nearest chunks, nearest-first:
/// {hits:[{context_name, chunk, distance}]}.
async fn handle(pool: &PgPool, embed: &Arc<EmbedClient>, payload: &[u8]) -> Result<Value> {
    let body: Value = if payload.is_empty() {
        json!({})
    } else {
        serde_json::from_slice(payload)?
    };

    let query = body.get("query").and_then(Value::as_str).unwrap_or("").trim();
    if query.is_empty() {
        return Err(anyhow!("query required"));
    }
    // Default 5, clamped into [1, 50] like the Bun handler.
    let n = body
        .get("limit")
        .and_then(Value::as_i64)
        .unwrap_or(DEFAULT_LIMIT)
        .clamp(1, MAX_LIMIT);

    let vec = embed.embed(query).await?;
    let vec_text = format_vector(&vec)?;

    // $1 (the query vector) is referenced twice — in the distance
    // projection and the ORDER BY — so the planner sees one literal. The
    // ::vector cast is server-side; there's no native binding for the
    // pgvector type. <=> is cosine distance, smaller = closer.
    let rows = sqlx::query(
        "SELECT context_name, chunk, embedding <=> $1::vector AS distance
         FROM oos.oos_domain_schema
         WHERE embedding IS NOT NULL
         ORDER BY embedding <=> $1::vector
         LIMIT $2",
    )
    .bind(&vec_text)
    .bind(n)
    .fetch_all(pool)
    .await?;

    let mut hits = Vec::with_capacity(rows.len());
    for r in &rows {
        hits.push(json!({
            "context_name": r.try_get::<String, _>("context_name")?,
            "chunk":        r.try_get::<String, _>("chunk")?,
            "distance":     r.try_get::<f64, _>("distance")?,
        }));
    }
    Ok(json!({ "hits": hits }))
}
