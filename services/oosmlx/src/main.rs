//! oosmlx — onisin inference engine, MLX backend for Apple Silicon.
//!
//! Serves an OpenAI-compatible API for a model loaded from a local directory or
//! pulled from Hugging Face. This milestone wires resolution + tokenizer +
//! server end to end behind the shared Engine trait; the MLX forward pass
//! (mlx-rs) lands next behind the same impl, and ooscuda will mirror it.
//!
//! Usage: `oosmlx <model-path-or-hf-repo[@revision]> [host:port]`

mod config;
mod engine;
#[cfg(feature = "mlx")]
mod model;

use std::net::SocketAddr;
use std::sync::Arc;

use anyhow::{Context, Result};
use oos_infer::{registry, ModelRef};

use crate::engine::MlxEngine;

#[tokio::main]
async fn main() -> Result<()> {
    tracing_subscriber::fmt()
        .with_env_filter(
            tracing_subscriber::EnvFilter::try_from_default_env()
                .unwrap_or_else(|_| tracing_subscriber::EnvFilter::new("info")),
        )
        .init();

    let mut args = std::env::args().skip(1);
    let model_arg = args
        .next()
        .context("usage: oosmlx <model-path-or-hf-repo[@revision]> [host:port]")?;
    let addr: SocketAddr = args
        .next()
        .unwrap_or_else(|| "127.0.0.1:8080".to_string())
        .parse()
        .context("invalid host:port")?;

    let model_ref = ModelRef::parse(&model_arg);
    tracing::info!(?model_ref, "resolving model");
    let files = registry::resolve(&model_ref).context("resolving model files")?;
    let engine = Arc::new(MlxEngine::load(&files, model_arg).context("loading engine")?);

    oos_infer::server::serve(addr, engine).await
}
