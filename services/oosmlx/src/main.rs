//! oosmlx -- onisin inference engine, MLX backend for Apple Silicon.
//!
//! Serves a model loaded from a local directory or pulled from Hugging Face
//! over two transports behind the shared Engine trait: an OpenAI-compatible
//! HTTP API (always on, the external face) and, when `NATS_URL` is set, a NATS
//! Request-Reply responder (the internal face other onisin services use). The
//! MLX forward pass lives in `model.rs` behind the `mlx` feature; ooscuda will
//! mirror the same impl.
//!
//! Usage: `oosmlx <model-path-or-hf-repo[@revision]> [host:port]`
//! Env:   `NATS_URL` enables NATS; `OOS_INFER_SUBJECT` sets the subject prefix
//!        (default `oos.cmd.infer`).

mod engine;
#[cfg(feature = "mlx")]
mod models;

use std::net::SocketAddr;
use std::sync::Arc;

use anyhow::{Context, Result};
use oos_infer::{registry, ModelRef};

use crate::engine::MlxEngine;

#[tokio::main]
async fn main() -> Result<()> {
    // Handle help before anything else, so `--help`/`-h` (or no args) prints
    // usage instead of being parsed as a model id and 404'ing against HF.
    let raw: Vec<String> = std::env::args().skip(1).collect();
    if raw.is_empty() || raw.iter().any(|a| a == "-h" || a == "--help") {
        print_help();
        return Ok(());
    }

    tracing_subscriber::fmt()
        .with_env_filter(
            tracing_subscriber::EnvFilter::try_from_default_env()
                .unwrap_or_else(|_| tracing_subscriber::EnvFilter::new("info")),
        )
        .init();

    let mut args = raw.into_iter();
    let model_arg = args.next().expect("args are non-empty (checked above)");
    let addr: SocketAddr = args
        .next()
        .unwrap_or_else(|| "127.0.0.1:8080".to_string())
        .parse()
        .context("invalid host:port")?;

    let model_ref = ModelRef::parse(&model_arg);
    tracing::info!(?model_ref, "resolving model");
    let files = registry::resolve(&model_ref).context("resolving model files")?;
    let engine = Arc::new(MlxEngine::load(&files, model_arg).context("loading engine")?);

    // HTTP is always served (the external OpenAI-compatible face). When NATS_URL
    // is set, also serve the internal Request-Reply transport other onisin
    // services use; standalone customers leave it unset and get HTTP only.
    let http = tokio::spawn(oos_infer::server::serve(addr, engine.clone()));

    match std::env::var("NATS_URL") {
        Ok(url) => {
            let prefix = std::env::var("OOS_INFER_SUBJECT")
                .unwrap_or_else(|_| "oos.cmd.infer".to_string());
            let nats = tokio::spawn(oos_infer::nats::serve(url, prefix, engine));
            // Either transport ending (normally an error) ends the process.
            tokio::select! {
                r = http => r.context("http task")?,
                r = nats => r.context("nats task")?,
            }
        }
        Err(_) => http.await.context("http task")?,
    }
}

/// CLI usage. Kept next to the arg parsing in `main`; `--help`/`-h` and the
/// no-args case route here instead of resolving the flag as a model.
fn print_help() {
    println!(
        "oosmlx -- onisin inference engine (MLX backend, Apple Silicon)

USAGE:
    oosmlx <model> [host:port]
    oosmlx --help

ARGS:
    <model>       Local model directory, or a Hugging Face repo id, optionally
                  pinned as repo@revision. A path that exists on disk is loaded
                  locally; anything else is fetched from Hugging Face.
    [host:port]   Address for the HTTP API (default 127.0.0.1:8080).

ENV:
    NATS_URL            If set, also serve the internal NATS Request-Reply
                        transport (e.g. nats://127.0.0.1:4222). HTTP is always on.
    OOS_INFER_SUBJECT   NATS subject prefix (default oos.cmd.infer).
    RUST_LOG            Log filter (default info).

ENDPOINTS (OpenAI-compatible, always served over HTTP):
    GET  /v1/models
    POST /v1/chat/completions
  When NATS_URL is set, the same two over NATS Request-Reply:
    oos.cmd.infer.models    oos.cmd.infer.chat

EXAMPLES:
    oosmlx ~/models/gemma-4-26b-a4b-it-4bit
    oosmlx mlx-community/gemma-3-1b-it-bf16 127.0.0.1:8088
    NATS_URL=nats://127.0.0.1:4222 oosmlx ./my-model"
    );
}
