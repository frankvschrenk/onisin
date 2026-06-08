//! Runtime types for a parsed `.view` file.
//!
//! Deliberately a *minimal* shape, not a full mirror of the Bun
//! `ViewDef`. The Rust view parser exists only for the two backend
//! consumers — the view RAG chunk (header + verbatim source) and the
//! view index (name/title/domain/default + displayed columns). The full
//! body (widgets, layout, toolbar, rich text) is parsed by the
//! frontend's own TS Langium parser for live rendering and Monaco, so
//! re-modelling every body element here would be dead weight on the
//! server. We keep only what the chunk and index actually read.

use serde::{Deserialize, Serialize};
use thiserror::Error;

/// A parse failure with the 1-based source position it occurred at.
/// Mirrors the domain parser's ParseError; kept view-local so the view
/// module stays independent of domain internals (the two DSLs share no
/// code, only this tiny shape).
#[derive(Debug, Clone, PartialEq, Eq, Error)]
#[error("{message} (at {line}:{col})")]
pub struct ParseError {
    pub line: usize,
    pub col: usize,
    pub message: String,
}

/// One domain bound by a view's `over <name>[(<alias>)]` clause. A
/// missing alias falls back to the name, so single-domain views read as
/// `over person` with alias == name. The first binding is the primary
/// one (save/delete target and the domain the index keys on).
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct ViewDomainBinding {
    pub name: String,
    pub alias: String,
    pub primary: bool,
}

/// The backend-facing runtime shape of a `view { ... }` block.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct ViewDef {
    pub name: String,
    pub title: String,
    pub domains: Vec<ViewDomainBinding>,
    pub default: bool,
    /// Field names referenced by table columns, de-duplicated in
    /// first-seen (source) order. This is the *only* body content the
    /// backend needs: the view index hands this column list to the agent
    /// so it fetches only displayed columns, and detail-style views with
    /// no table produce an empty list (the resolver then falls back to
    /// the full domain). The `id` hoist policy lives in the index
    /// builder, not here, so this stays the raw parse result.
    pub table_fields: Vec<String>,
}
