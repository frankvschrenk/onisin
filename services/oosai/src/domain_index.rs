//! Compact one-entry-per-domain catalogue for the agent's system prompt.
//!
//! Rust port of the Bun `domain-index.ts`. The oos agent always carries
//! an index of every known domain in its prompt so the LLM can map a
//! phrase like "Mitarbeiter" onto the `person` domain before fetching
//! the full chunk via oos.cmd.search; the webview resolver matches the
//! same aliases without an LLM round-trip.
//!
//! The entry is deliberately lean. Anything richer (relations, filter
//! examples) lives in oos_domain_schema and reaches the model on demand,
//! so the index stays cheap no matter how many domains a deployment has.

use serde::Serialize;
use sqlx::{PgPool, Row};

use oos_dsls::domain::{domain_aliases, parse_domain};

/// One row of the domain catalogue. Serialized camelCase to stay
/// byte-compatible with the DomainIndexEntry the oos resolver and prompt
/// builder already expect over NATS.
#[derive(Debug, Clone, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct DomainIndexEntry {
    /// Domain name, e.g. "person" — identical to context_name in
    /// oos_domain_schema.
    pub name: String,
    /// Underlying source, e.g. "person@demo".
    pub source: String,
    /// The author-written `scope` AI hint, when one is present.
    #[serde(skip_serializing_if = "Option::is_none")]
    pub scope: Option<String>,
    /// Canonical aliases derived from the domain name, so the resolver
    /// can recognise a domain mention without going through the LLM.
    pub aliases: Vec<String>,
}

/// Returns one entry per parseable row of oos.domain, ordered by id so
/// the rendered prompt is stable across reloads (matters for caching and
/// human inspection). An unparseable row is logged and skipped: a broken
/// domain in the editor must not blank out the rest of the index.
pub async fn load_domain_index(pool: &PgPool) -> anyhow::Result<Vec<DomainIndexEntry>> {
    let rows = sqlx::query("SELECT id, source FROM oos.domain ORDER BY id")
        .fetch_all(pool)
        .await?;

    let mut out = Vec::with_capacity(rows.len());
    for row in &rows {
        let id: String = row.try_get("id")?;
        let source: String = row.try_get("source")?;
        match parse_domain(&source) {
            Ok(def) => {
                let scope = def
                    .ai_hints
                    .iter()
                    .find(|h| h.name == "scope")
                    .map(|h| h.body.clone());
                out.push(DomainIndexEntry {
                    name: def.name.clone(),
                    source: format!("{}@{}", def.source, def.dsn),
                    scope,
                    aliases: domain_aliases(&def),
                });
            }
            Err(e) => eprintln!("[oosai] domain-index parse failed {id}: {e}"),
        }
    }
    Ok(out)
}
