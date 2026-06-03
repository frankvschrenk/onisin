//! Schema backfill: reconcile source tables with their embedding
//! (schema) tables on boot.
//!
//! Currently only the DSL-free global-prompt side. domain and view
//! need parseDomain/parseView + renderers from oos-dsls-ts (Langium),
//! which is a later Rust-parser slice; their stores and the orphan
//! pruning are mechanically identical and slot in then.
//!
//! Re-embedding is unconditional (no updated_at short-circuit): a model
//! upgrade would otherwise silently leave stale vectors, and the demo
//! dataset is cheap to re-embed on each boot.

use std::collections::HashSet;
use std::sync::Arc;

use sqlx::{PgPool, Row};

use crate::embed::EmbedClient;
use crate::global_store;

/// Per-kind result counts.
pub struct BackfillCount {
    pub embedded: usize,
    pub pruned: usize,
}

/// Reconciles oos.global_prompt -> oos.oos_global_schema: re-embeds
/// every non-locale prompt and prunes schema rows whose source row is
/// gone. A single bad row is logged and skipped so it can't block the
/// rest of the backfill.
pub async fn run_global(pool: &PgPool, embed: &Arc<EmbedClient>) -> anyhow::Result<BackfillCount> {
    let rows = sqlx::query(
        "SELECT name, text FROM oos.global_prompt WHERE name NOT LIKE 'locale.%' ORDER BY name",
    )
    .fetch_all(pool)
    .await?;

    let mut source_keys: HashSet<String> = HashSet::new();
    let mut embedded = 0;
    for row in &rows {
        let name: String = row.get("name");
        let text: String = row.get("text");
        source_keys.insert(name.clone());
        match embed.embed(&text).await {
            Ok(vec) => match global_store::upsert(pool, &name, &text, &vec).await {
                Ok(()) => embedded += 1,
                Err(e) => eprintln!("[oosai] global upsert failed {name}: {e}"),
            },
            Err(e) => eprintln!("[oosai] global embed failed {name}: {e}"),
        }
    }

    let mut pruned = 0;
    for key in global_store::list_keys(pool).await? {
        if !source_keys.contains(&key) {
            global_store::delete(pool, &key).await?;
            println!("[oosai] pruned global orphan {key}");
            pruned += 1;
        }
    }

    Ok(BackfillCount { embedded, pruned })
}
