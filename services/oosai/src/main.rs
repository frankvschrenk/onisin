//! oosai — Onisin embedding service (Rust port).
//!
//! Migration slice so far: connects to NATS, reads config from the
//! JetStream KV bucket oos-ai/config, serves the generic embedding
//! subjects (oos.cmd.embed[.batch|.meta]) wire-compatibly, runs the
//! global-prompt schema backfill into pgvector, ticks a heartbeat on
//! status.oosai, and answers oos.cmd.oosai.env.show.
//!
//! Also serves the oosd designer's admin command surface (CRUD over
//! oos.domain / oos.view / event_type_grammar / event_mappings) via
//! command_handler, so the migrated Tauri oosd can talk these subjects
//! over NATS-over-WS directly.
//!
//! Not ported yet: domain/view *chunk re-embed* on save + backfill (need
//! renderLLMChunk / parseView from oos-dsls), the notify/event listeners,
//! the Hono HTTP/SSE bus, and the pipeline-runner. nodeId is empty until
//! oos-node-id-ts is ported.

mod backfill;
mod command_handler;
mod config;
mod embed;
mod error;
mod global_store;
mod nats_embed;
mod vector;

use std::sync::Arc;
use std::time::Duration;

use config::{Bootstrap, Config};
use embed::EmbedClient;
use oos_svc::env::EnvEntry;

const VERSION: &str = env!("CARGO_PKG_VERSION");

#[tokio::main]
async fn main() -> anyhow::Result<()> {
    let boot = Bootstrap::from_env();
    println!("[oosai] bootstrap: nats={} kv-bucket={}", boot.nats_url, boot.bucket);

    let client = async_nats::connect(&boot.nats_url)
        .await
        .map_err(|e| anyhow::anyhow!("NATS connection failed: {e}"))?;
    println!("[oosai] nats connected");

    // Config lives in KV; NATS must be up before we read it.
    let cfg = Config::load(&client, &boot).await;
    println!("[oosai] config loaded from kv:{}", boot.bucket);

    let embed = Arc::new(EmbedClient::new(
        &cfg.embed.base_url,
        &cfg.embed.api_key,
        &cfg.embed.model,
        Duration::from_secs(30),
    ));
    println!("[oosai] embed model = {} @ {}", embed.model(), cfg.embed.base_url);

    // One probe at startup so oos.cmd.embed.meta can serve the real
    // dimension. A failed probe is not fatal — we still serve embeddings
    // — but meta replies with vector_dim: 0 so a downstream service
    // refuses to build a vector column rather than guessing a size.
    let vector_dim = match embed.probe_dim().await {
        Ok(d) => {
            println!("[oosai] embed vector_dim = {d} (probed)");
            d
        }
        Err(e) => {
            eprintln!("[oosai] embed probe failed: {e} — vector_dim served as 0");
            0
        }
    };

    // Schema backfill (global side). Postgres is best-effort here: the
    // embedding subjects are the core service and must come up even when
    // the DB is down or the oos schema isn't installed yet.
    let pg_pool = match oos_svc::db::connect(&cfg.pg_url).await {
        Ok(pool) => {
            println!("[oosai] postgres ok");
            match backfill::run_global(&pool, &embed).await {
                Ok(c) => println!("[oosai] backfill global: embedded={} pruned={}", c.embedded, c.pruned),
                Err(e) => eprintln!("[oosai] global backfill skipped: {e}"),
            }
            Some(pool)
        }
        Err(e) => {
            eprintln!("[oosai] postgres unavailable, backfill + command handler skipped: {e}");
            None
        }
    };

    // env.show entries: config provenance plus the probed dimension.
    let mut show_entries = cfg.entries.clone();
    show_entries.push(EnvEntry::runtime("embed.vectorDim", vector_dim.to_string()));

    nats_embed::serve(client.clone(), embed, vector_dim).await?;
    // Command handler needs Postgres; when the DB is down its subjects
    // simply aren't served (callers time out) while embeddings stay up.
    if let Some(pool) = &pg_pool {
        command_handler::serve(client.clone(), pool.clone()).await?;
    }
    oos_svc::env_show::serve(client.clone(), "oosai", show_entries).await?;
    oos_svc::heartbeat::start(client.clone(), "oosai", VERSION, "");

    println!("[oosai] ready");

    // Block until interrupted, then drain so in-flight replies finish
    // before the connection closes.
    tokio::signal::ctrl_c().await?;
    println!("[oosai] shutting down, draining NATS");
    let _ = client.drain().await;
    Ok(())
}
