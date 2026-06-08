//! Compact one-entry-per-view catalogue for the agent's pre-LLM resolver.
//!
//! Rust port of the Bun `views-index.ts`. The resolver reads this once
//! to learn which views exist for each domain, which one is the natural
//! default, and — most importantly — which fields each view displays, so
//! the agent fetches only the columns the UI will render instead of the
//! full domain payload.
//!
//! Field extraction reuses the parser's table-column collection; the
//! `id` hoist policy (always present, always first) lives here because
//! it is an index concern, not a parse concern.

use serde::Serialize;
use sqlx::{PgPool, Row};

use oos_dsls::view::parse_view;

/// One row of the view catalogue. Serialized camelCase to stay
/// byte-compatible with the ViewIndexEntry the oos resolver expects.
#[derive(Debug, Clone, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct ViewIndexEntry {
    /// View identifier, e.g. "person_list".
    pub name: String,
    /// Human title, e.g. "Personen".
    pub title: String,
    /// The primary domain the view is bound to (the first `over` entry).
    /// The resolver matches user text against this domain's aliases.
    pub domain: String,
    /// True when this view is the natural starting point for the domain.
    pub default: bool,
    /// Field names this view displays — the union of every table
    /// column's field, with `id` hoisted to the front (always present
    /// when the view shows any columns, because every read needs the row
    /// identifier downstream). Detail-style views with no table get an
    /// empty list and the resolver falls back to the full domain.
    pub fields: Vec<String>,
}

/// Returns one entry per parseable row of oos.view, ordered by id for
/// stable output. An unparseable row is logged and skipped so a broken
/// view in the editor cannot blank out the rest of the catalogue.
pub async fn load_view_index(pool: &PgPool) -> anyhow::Result<Vec<ViewIndexEntry>> {
    let rows = sqlx::query("SELECT id, source FROM oos.view ORDER BY id")
        .fetch_all(pool)
        .await?;

    let mut out = Vec::with_capacity(rows.len());
    for row in &rows {
        let id: String = row.try_get("id")?;
        let source: String = row.try_get("source")?;
        match parse_view(&source) {
            Ok(def) => {
                let primary_domain = def.domains.first().map(|d| d.name.clone()).unwrap_or_default();
                out.push(ViewIndexEntry {
                    name: def.name,
                    title: def.title,
                    domain: primary_domain,
                    default: def.default,
                    fields: hoist_id(def.table_fields),
                });
            }
            Err(e) => eprintln!("[oosai] view-index parse failed {id}: {e}"),
        }
    }
    Ok(out)
}

/// Ensures `id` leads the field list: moved to the front when present,
/// prepended when absent but the view shows other columns, and left
/// empty when the view has no columns at all. Mirrors the Bun
/// collectFields hoist so the agent always has a row identifier.
fn hoist_id(mut fields: Vec<String>) -> Vec<String> {
    match fields.iter().position(|f| f == "id") {
        Some(0) => fields,
        Some(idx) => {
            fields.remove(idx);
            fields.insert(0, "id".to_string());
            fields
        }
        None if !fields.is_empty() => {
            fields.insert(0, "id".to_string());
            fields
        }
        None => fields,
    }
}
