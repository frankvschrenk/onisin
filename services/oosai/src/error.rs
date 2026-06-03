//! Error types for oosai.
//!
//! Mirrors the layered thiserror style from the Tauri reference: rich
//! per-area enums with `#[from]` so `?` propagates cleanly, and a
//! transient classifier the retry loop can branch on. Display strings
//! are English because they end up in logs and on the NATS wire.

use thiserror::Error;

/// Failure of a single embedding round-trip to the OpenAI-compatible
/// endpoint.
#[derive(Debug, Error)]
pub enum EmbedError {
    /// Network-level failure (connect/timeout). Worth retrying.
    #[error("embedding endpoint unreachable: {0}")]
    Unreachable(String),

    /// Endpoint answered with a non-success status. `transient` marks
    /// 429/5xx, which the retry loop backs off on; everything else
    /// fails immediately.
    #[error("embedding endpoint returned HTTP {status}")]
    Http { status: u16, transient: bool },

    /// Endpoint answered 2xx but with no usable vector.
    #[error("empty embedding response (model={0})")]
    Empty(String),

    #[error(transparent)]
    Decode(#[from] serde_json::Error),
}

impl EmbedError {
    /// Reports whether retrying this error has any chance of succeeding.
    /// Used by `EmbedClient::embed` to decide between backing off and
    /// giving up.
    pub fn is_transient(&self) -> bool {
        match self {
            EmbedError::Unreachable(_) => true,
            EmbedError::Http { transient, .. } => *transient,
            _ => false,
        }
    }
}
