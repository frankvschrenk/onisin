//! oosmlx -- onisin inference engine, MLX backend for Apple Silicon.
//!
//! Serves models over two transports behind the shared Engine trait: an
//! OpenAI-compatible HTTP API (always on, the external face) and, when
//! `NATS_URL` is set, a NATS Request-Reply responder (the internal face other
//! onisin services use). Models are chosen per request and loaded on demand by
//! the engine (see `engine.rs`); the MLX forward pass lives in `models/` behind
//! the `mlx` feature, and ooscuda will mirror the same impl.
//!
//! Usage: `oosmlx [host:port]` (optionally `--preload <model>`); see `--help`.
//! Env:   `NATS_URL` enables NATS; `OOS_INFER_SUBJECT` sets the subject prefix
//!        (default `oos.cmd.infer`); `HF_HOME` locates the model cache.

mod engine;
#[cfg(feature = "mlx")]
mod models;

use std::net::SocketAddr;
use std::sync::Arc;

use anyhow::{Context, Result};

use crate::engine::MlxEngine;

#[tokio::main]
async fn main() -> Result<()> {
    // Handle help before anything else, so `--help`/`-h` prints usage instead
    // of being parsed as an argument.
    let raw: Vec<String> = std::env::args().skip(1).collect();
    if raw.iter().any(|a| a == "-h" || a == "--help") {
        print_help();
        return Ok(());
    }

    tracing_subscriber::fmt()
        .with_env_filter(
            tracing_subscriber::EnvFilter::try_from_default_env()
                .unwrap_or_else(|_| tracing_subscriber::EnvFilter::new("info")),
        )
        .init();

    // Models are chosen per request now, so the only positional argument is the
    // optional listen address; `--preload <model>` warms one at startup.
    let mut preload: Option<String> = None;
    let mut addr_arg: Option<String> = None;
    let mut it = raw.into_iter();
    while let Some(arg) = it.next() {
        if arg == "--preload" {
            preload = Some(it.next().context("--preload needs a model id")?);
        } else if addr_arg.is_none() {
            addr_arg = Some(arg);
        } else {
            anyhow::bail!("unexpected argument: {arg}");
        }
    }
    let addr: SocketAddr = addr_arg
        .unwrap_or_else(|| "127.0.0.1:8080".to_string())
        .parse()
        .context("invalid host:port")?;

    let engine = Arc::new(MlxEngine::new());
    if let Some(model) = &preload {
        tracing::info!(%model, "preloading model");
        engine.preload(model).context("preloading model")?;
    }

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

/// CLI usage. Kept next to the arg parsing in `main`; `--help`/`-h` routes here.
fn print_help() {
    println!(
        "oosmlx -- onisin inference engine (MLX backend, Apple Silicon)

Models are selected per request (the request's `model` field), like
OpenAI/Ollama: the requested model -- a local path or a Hugging Face repo id --
is loaded on demand and kept resident until a different one is requested.

USAGE:
    oosmlx [host:port]
    oosmlx --preload <model> [host:port]
    oosmlx --help

OPTIONS:
    --preload <model>   Load a model at startup so the first request is warm.
                        <model> is a local directory or an HF repo id
                        (optionally pinned as repo@revision).
    [host:port]         Address for the HTTP API (default 127.0.0.1:8080).

ENV:
    NATS_URL            If set, also serve the internal NATS Request-Reply
                        transport (e.g. nats://127.0.0.1:4222). HTTP is always on.
    OOS_INFER_SUBJECT   NATS subject prefix (default oos.cmd.infer).
    HF_HOME             Hugging Face cache location (default ~/.cache/huggingface);
                        its hub/ dir is the source for GET /v1/models.
    RUST_LOG            Log filter (default info).

ENDPOINTS (OpenAI-compatible, always served over HTTP):
    GET  /v1/models                lists models found in the local HF cache
    POST /v1/chat/completions      the `model` field picks/loads the model
  When NATS_URL is set, the same two over NATS Request-Reply:
    oos.cmd.infer.models    oos.cmd.infer.chat

EXAMPLES:
    oosmlx
    oosmlx 127.0.0.1:8088
    oosmlx --preload mlx-community/gemma-3-1b-it-bf16 127.0.0.1:8088"
    );
}
