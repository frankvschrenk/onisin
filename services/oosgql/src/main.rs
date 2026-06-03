//! oosgql — Onisin GraphQL service (Rust port).
//!
//! Migration slice so far (Path A): connects to NATS, reads config from
//! the JetStream KV bucket oos-gql/config, opens a best-effort Postgres
//! pool, ticks a heartbeat on status.oosgql, answers oos.cmd.oosgql.env
//! .show, and serves the two DSL-free oos.cmd.gql subjects (view +
//! domain) as real source passthroughs.
//!
//! permissions is now real too: a DomainStore loads every oos.domain
//! row, parses it with the oos-dsls domain parser, and caches the
//! DomainDefs; oos.domain.changed hot-reloads single rows. Still not
//! ported: oos.cmd.gql.{query,mutation} — they need a dynamic GraphQL
//! engine built from those DomainDefs, so they reply with an explicit
//! not-yet-migrated error until then. nodeId is empty until
//! oos-node-id-ts is ported (ooso degrades to host+pid).

mod command_handler;
mod config;
mod data;
mod domain_store;
mod notify;

use std::sync::Arc;

use config::{Bootstrap, Config};
use domain_store::DomainStore;

const VERSION: &str = env!("CARGO_PKG_VERSION");

#[tokio::main]
async fn main() -> anyhow::Result<()> {
    let boot = Bootstrap::from_env();
    println!("[oosgql] bootstrap: nats={} kv-bucket={}", boot.nats_url, boot.bucket);

    let client = async_nats::connect(&boot.nats_url)
        .await
        .map_err(|e| anyhow::anyhow!("NATS connection failed: {e}"))?;
    println!("[oosgql] nats connected");

    // Config lives in KV; NATS must be up before we read it.
    let cfg = Config::load(&client, &boot).await;
    println!(
        "[oosgql] config loaded from kv:{} (host={} port={})",
        boot.bucket, cfg.host, cfg.port
    );

    // Postgres is best-effort: the service must still come up so the
    // heartbeat and env.show work when the DB is down or the oos schema
    // is not installed yet. view/domain then reply with an explicit
    // error per request rather than the whole service refusing to boot
    // (the Bun version crashed on a failed ping).
    let pool = match oos_svc::db::connect(&cfg.pg_url).await {
        Ok(p) => {
            println!("[oosgql] postgres ok");
            Some(p)
        }
        Err(e) => {
            eprintln!("[oosgql] postgres unavailable, view/domain will error: {e}");
            None
        }
    };

    // Domain cache for the permissions handler (and the future GraphQL
    // schema). Load is best-effort: a missing oos schema leaves it empty
    // rather than blocking boot.
    let store = Arc::new(DomainStore::new(pool.clone()));
    match store.load_all().await {
        Ok(()) => println!("[oosgql] domains loaded: {}", store.snapshot().len()),
        Err(e) => eprintln!("[oosgql] initial domain load skipped: {e}"),
    }

    command_handler::serve(client.clone(), pool, store.clone()).await?;
    notify::start(client.clone(), store).await?;
    oos_svc::env_show::serve(client.clone(), "oosgql", cfg.entries).await?;
    oos_svc::heartbeat::start(client.clone(), "oosgql", VERSION, "");

    println!("[oosgql] ready");

    // Block until interrupted, then drain so in-flight replies finish
    // before the connection closes.
    tokio::signal::ctrl_c().await?;
    println!("[oosgql] shutting down, draining NATS");
    let _ = client.drain().await;
    Ok(())
}
