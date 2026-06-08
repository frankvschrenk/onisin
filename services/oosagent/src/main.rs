//! oosagent — Onisin chat-agent service (Rust port of the Bun agent loop).
//!
//! Migration slice so far: connects to NATS and serves the turn subjects
//! oos.cmd.turn.{ask,translate,chat,cancel}. The LLM connection and
//! per-model tuning travel in each turn payload (AgentSettings/
//! AgentTuning from the webview), so there is no model config to read
//! from KV — only natsUrl is bootstrapped. Heartbeat ticks on
//! status.oosagent and oos.cmd.oosagent.env.show reports provenance.
//!
//! Now also serves oos.cmd.turn.chat — the ReAct loop with the
//! oos_schema_search / oos_query tools and AgentEvent streaming on
//! oos.agent.event.<turnId> — plus oos.cmd.turn.cancel. Still a
//! webview-side stub: oos.cmd.turn.event (gated on the event subsystem).

mod chat;
mod config;
mod llm;
mod prompt;
mod tools;
mod turn;

use config::Bootstrap;

const VERSION: &str = env!("CARGO_PKG_VERSION");

#[tokio::main]
async fn main() -> anyhow::Result<()> {
    let boot = Bootstrap::from_env();
    println!("[oosagent] bootstrap: nats={}", boot.nats_url);

    let client = async_nats::connect(&boot.nats_url)
        .await
        .map_err(|e| anyhow::anyhow!("NATS connection failed: {e}"))?;
    println!("[oosagent] nats connected");

    turn::serve(client.clone()).await?;
    oos_svc::env_show::serve(client.clone(), "oosagent", boot.entries).await?;
    oos_svc::heartbeat::start(client.clone(), "oosagent", VERSION, "");

    println!("[oosagent] ready");

    // Block until interrupted, then drain so in-flight replies finish
    // before the connection closes.
    tokio::signal::ctrl_c().await?;
    println!("[oosagent] shutting down, draining NATS");
    let _ = client.drain().await;
    Ok(())
}
