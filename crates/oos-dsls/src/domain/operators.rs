//! Type-driven filter-operator catalog (Rust port of oos-dsls-ts
//! renderer/operators.ts, retargeted from GraphQL to oos.cmd.data.query).
//!
//! The op tokens MUST match oosgql data.rs `allowed_ops` exactly
//! (eq/ne/contains/gt/gte/lt/lte): the chunk teaches the LLM which ops a
//! field accepts and the runtime rejects anything else. GraphQL is
//! retired — a filter is a {field, op, value} entry in a `where` array.

use super::types::{ExampleOp, FieldType};

/// One filter operator available on a given field type.
pub struct FilterOp {
    /// Author-facing op (matches `example` clauses in the DSL).
    pub name: ExampleOp,
    /// Human label used in the chunk ("equals", "contains", …).
    pub label: &'static str,
    /// data.query op token, matching oosgql allowed_ops.
    pub token: &'static str,
    /// Sample value already in JSON form (quoted for string-typed
    /// values, bare for numeric/boolean), used in a typed-default
    /// where-example.
    pub sample_value: &'static str,
}

const fn op(
    name: ExampleOp,
    label: &'static str,
    token: &'static str,
    sample_value: &'static str,
) -> FilterOp {
    FilterOp { name, label, token, sample_value }
}

/// Operators allowed for a field type. Mirrors oosgql data.rs
/// allowed_ops one-to-one so the LLM is only taught ops the runtime
/// accepts.
pub fn operators_for_type(t: FieldType) -> Vec<FilterOp> {
    match t {
        FieldType::String | FieldType::Text => vec![
            op(ExampleOp::Like, "contains", "contains", "\"a\""),
            op(ExampleOp::Eq, "equals", "eq", "\"value\""),
            op(ExampleOp::Ne, "not equals", "ne", "\"value\""),
        ],
        FieldType::Int | FieldType::Float => vec![
            op(ExampleOp::Eq, "equals", "eq", "0"),
            op(ExampleOp::Ne, "not equals", "ne", "0"),
            op(ExampleOp::Gt, "greater than", "gt", "0"),
            op(ExampleOp::Ge, "greater or equal", "gte", "0"),
            op(ExampleOp::Lt, "less than", "lt", "0"),
            op(ExampleOp::Le, "less or equal", "lte", "0"),
        ],
        FieldType::Bool => vec![op(ExampleOp::Eq, "equals", "eq", "true")],
        FieldType::Date | FieldType::Datetime => vec![
            op(ExampleOp::Eq, "equals", "eq", "\"2024-01-01\""),
            op(ExampleOp::Gt, "after", "gt", "\"2024-01-01\""),
            op(ExampleOp::Lt, "before", "lt", "\"2024-01-01\""),
        ],
    }
}

/// The FilterOp matching an author-supplied op name for a field type.
pub fn find_operator(t: FieldType, op_name: ExampleOp) -> Option<FilterOp> {
    operators_for_type(t).into_iter().find(|o| o.name == op_name)
}

/// JSON value form for a where entry: string/text (or any quoted-literal)
/// values are quoted, numeric and boolean values stay bare.
pub fn format_example_value(t: FieldType, value: &str, value_is_string: bool) -> String {
    if value_is_string {
        return format!("\"{value}\"");
    }
    if matches!(t, FieldType::String | FieldType::Text) {
        return format!("\"{value}\"");
    }
    value.to_string()
}

/// Renders one where entry as compact JSON: {"field":..,"op":..,"value":..}.
pub fn render_where_entry(field: &str, token: &str, value: &str) -> String {
    format!("{{ \"field\": \"{field}\", \"op\": \"{token}\", \"value\": {value} }}")
}
