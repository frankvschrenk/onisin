//! Runtime types for a parsed `.domain` file.
//!
//! These mirror the oos-dsls-ts `DomainDef` one-to-one, including the
//! JSON field names: serde is configured so a serialized DomainDef is
//! byte-compatible with what the Bun mapper produced (camelCase keys,
//! lowercase/snake_case enum literals). That keeps the door open for
//! sending these over NATS to oosd/oos without a translation shim.
//!
//! Field modifiers are pre-bucketed (read_only / filterable /
//! options_ref) rather than kept as a heterogenous list, so consumers
//! (the LLM-chunk renderer, the GraphQL builder) read them directly.

use serde::{Deserialize, Serialize};
use thiserror::Error;

/// A parse failure with the 1-based source position it occurred at.
/// Carrying line/col lets oosd surface the same kind of underline the
/// Langium LSP did, without shipping a whole language server.
#[derive(Debug, Clone, PartialEq, Eq, Error)]
#[error("{message} (at {line}:{col})")]
pub struct ParseError {
    pub line: usize,
    pub col: usize,
    pub message: String,
}

/// Column type literal accepted by the grammar.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "lowercase")]
pub enum FieldType {
    Int,
    Float,
    String,
    Text,
    Bool,
    Date,
    Datetime,
}

/// Operator on a filter example.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "lowercase")]
pub enum ExampleOp {
    Eq,
    Ne,
    Lt,
    Le,
    Gt,
    Ge,
    Like,
    In,
}

/// Action a role may take on a domain.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "lowercase")]
pub enum PermissionAction {
    Read,
    Write,
    Delete,
}

/// Cardinality of a relation to another domain.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum RelationKind {
    HasMany,
    HasOne,
    BelongsTo,
}

/// One author-supplied filter example for an LLM. The value is always
/// stringified; `value_is_string` records whether the source literal
/// was quoted, which the renderer needs for quoting hints.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct ExampleDef {
    pub op: ExampleOp,
    pub value: String,
    pub value_is_string: bool,
    pub description: String,
}

/// Role-scoped permission entry on a domain.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct PermissionDef {
    pub role: String,
    pub actions: Vec<PermissionAction>,
}

/// A relation from this domain to another, with the binding columns.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct RelationDef {
    pub name: String,
    pub kind: RelationKind,
    pub target: String,
    pub local_field: String,
    pub foreign_field: String,
}

/// A lookup source for dropdowns: which table/columns supply the
/// value/label pairs, optionally ordered and on a non-default dsn.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct MetaDef {
    pub name: String,
    pub table: String,
    pub value_field: String,
    pub label_field: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub order_by: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub dsn: Option<String>,
}

/// One field of a domain with its modifiers pre-bucketed.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct DomainFieldDef {
    pub name: String,
    /// Renamed to `type` on the wire to match the TS interface; `type`
    /// is a reserved word in Rust so the field itself is `field_type`.
    #[serde(rename = "type")]
    pub field_type: FieldType,
    pub read_only: bool,
    pub filterable: bool,
    /// Name of the Meta this field draws options from, if any. Stored
    /// by name only — resolution against the domain's metas happens in
    /// the renderer, so a dangling reference does not abort the parse.
    #[serde(skip_serializing_if = "Option::is_none")]
    pub options_ref: Option<String>,
    pub examples: Vec<ExampleDef>,
}

/// Free-form AI hint (name + body), kept in declaration order so the
/// author controls how they render into the embedded LLM chunk.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct AiHintDef {
    pub name: String,
    pub body: String,
}

/// The full runtime shape of a `domain { ... }` block.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct DomainDef {
    pub name: String,
    pub source: String,
    pub dsn: String,
    pub permissions: Vec<PermissionDef>,
    pub fields: Vec<DomainFieldDef>,
    pub relations: Vec<RelationDef>,
    pub metas: Vec<MetaDef>,
    pub ai_hints: Vec<AiHintDef>,
    /// Author-supplied alternative names, concatenated across all
    /// `aliases [...]` clauses in declaration order; blanks dropped.
    pub aliases: Vec<String>,
}
