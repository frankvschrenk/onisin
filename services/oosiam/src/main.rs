//! oosiam — Onisin built-in IAM (Rust port).
//!
//! OAuth2 authorization-code + PKCE authorization server with RS256
//! tokens and a published JWKS — the batteries-included default IdP for
//! users who do not bring their own enterprise IAM (Keycloak stays the
//! bring-your-own option). Faithful port of the Bun oosiam.
//!
//! Boot sequence:
//!   1. Connect to NATS (control plane for oosd management commands).
//!   2. Open the Postgres pool and ping it.
//!   3. Ensure the iam schema; seed an admin on first start.
//!   4. Load (or generate) the RS256 signing key.
//!   5. Start the NATS management handler (oosd admin panel).
//!   6. Start the HTTP server (OAuth2 + PKCE endpoints).
//!   7. Publish connection config to the oos-iam KV bucket.
//!   8. Heartbeat + env.show for the ooso operator console.

mod command_handler;
mod config;
mod jwt;
mod keys;
mod kv_config;
mod pkce;
mod server;
mod store;

use config::Config;
use oos_svc::env::EnvEntry;

const VERSION: &str = env!("CARGO_PKG_VERSION");

#[tokio::main]
async fn main() -> anyhow::Result<()> {
    let cfg = Config::load();
    println!("[oosiam] starting on {}:{}", cfg.host, cfg.port);

    let client = async_nats::connect(&cfg.nats_url)
        .await
        .map_err(|e| anyhow::anyhow!("NATS connection failed: {e}"))?;
    println!("[oosiam] nats connected");

    // Postgres is required: oosiam's whole job is the user store. Unlike
    // the embedding service it has nothing useful to do without it, so a
    // failed connect is fatal here rather than best-effort.
    let pool = oos_svc::db::connect(&cfg.pg_url).await?;
    println!("[oosiam] postgres ok");

    store::ensure_schema(&pool).await?;

    // First-run bootstrap: seed one admin so a solo installer can sign
    // in. The password is printed to stdout ONLY (never over NATS).
    if let Some((email, password)) = store::seed_admin_if_empty(&pool).await? {
        println!("\n[oosiam] ⚠️  first-run admin: {email} / {password}");
        println!("[oosiam] this password is shown ONCE — sign in and change it.\n");
    }

    let key = keys::load_or_generate(&cfg.key_path)?;
    let kid = key.kid.clone();
    println!("[oosiam] RS256 signing key ready (kid={kid})");

    // NATS management handler (oosd admin panel talks here).
    command_handler::serve(client.clone(), pool.clone()).await?;

    // HTTP server (OAuth2 + PKCE endpoints). issuer drives discovery,
    // token `iss`, and jwks_uri. Spawned so this task can go on to the
    // KV publish + heartbeat and then block on ctrl_c.
    let issuer = format!("http://{}:{}", cfg.host, cfg.port);
    {
        let (host, port, issuer, ttl, cttl) =
            (cfg.host.clone(), cfg.port, issuer.clone(), cfg.token_ttl_sec, cfg.code_ttl_sec);
        tokio::spawn(async move {
            if let Err(e) = server::serve(host, port, pool, key, issuer, ttl, cttl).await {
                eprintln!("[oosiam] http server error: {e}");
            }
        });
    }

    // Publish connection config to KV so a client bootstrapped with only
    // the NATS URL can find the issuer + client defaults. Non-fatal: the
    // HTTP and NATS faces stay up regardless of KV availability.
    match kv_config::publish_config(
        &client,
        &kv_config::IamClientConfig {
            issuer_url: issuer.clone(),
            client_id: cfg.client_id.clone(),
            redirect_uri: cfg.redirect_uri.clone(),
        },
    )
    .await
    {
        Ok(()) => println!("[oosiam] published connection config to KV bucket 'oos-iam'"),
        Err(e) => eprintln!("[oosiam] KV config publish failed (non-fatal): {e}"),
    }

    // env.show entries: config provenance plus the signing-key id.
    let mut show_entries = cfg.entries.clone();
    show_entries.push(EnvEntry::runtime("signing.kid", kid));
    oos_svc::env_show::serve(client.clone(), "oosiam", show_entries).await?;
    oos_svc::heartbeat::start(client.clone(), "oosiam", VERSION, "");

    println!("[oosiam] ready on {issuer}");

    // Block until interrupted, then drain so in-flight replies finish.
    tokio::signal::ctrl_c().await?;
    println!("[oosiam] shutting down, draining NATS");
    let _ = client.drain().await;
    Ok(())
}
