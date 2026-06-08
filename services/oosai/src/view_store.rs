//! Read/write the oos.oos_view_schema table.
//!
//! One row per parseable `.view` source. Primary key is `id` (the
//! view's name, e.g. "person_list"). The `kind` column distinguishes
//! "element" rows (one per literal view) from future "pattern" rows
//! (extracted snippets); this MVP only ever writes kind='element', and
//! list_keys reads back only element rows so a later pattern path can
//! coexist without being pruned on boot.
//!
//! Symmetric to domain_store; the embedding is serialised via
//! vector::format_vector and cast to ::vector server-side.

use sqlx::{PgPool, Row};

use crate::vector::format_vector;

/// The only view-chunk kind written today. "pattern" (snippet
/// extraction) is a later feature and would use a separate id namespace.
const KIND_ELEMENT: &str = "element";

/// Inserts or replaces one element row. ON CONFLICT (id) covers the
/// rebuild path: a view edited in oosd re-renders its chunk and
/// overwrites the row.
pub async fn upsert(pool: &PgPool, id: &str, chunk: &str, embedding: &[f32]) -> anyhow::Result<()> {
    let vec = format_vector(embedding)?;
    sqlx::query(
        "INSERT INTO oos.oos_view_schema (id, kind, chunk, embedding, updated_at)
         VALUES ($1, $2, $3, $4::vector, now())
         ON CONFLICT (id) DO UPDATE
         SET kind = EXCLUDED.kind, chunk = EXCLUDED.chunk,
             embedding = EXCLUDED.embedding, updated_at = now()",
    )
    .bind(id)
    .bind(KIND_ELEMENT)
    .bind(chunk)
    .bind(vec)
    .execute(pool)
    .await?;
    Ok(())
}

/// Removes a row by primary key. Idempotent — a missing row is fine,
/// matching the delete path after the source view is already gone.
pub async fn delete(pool: &PgPool, id: &str) -> anyhow::Result<()> {
    sqlx::query("DELETE FROM oos.oos_view_schema WHERE id = $1")
        .bind(id)
        .execute(pool)
        .await?;
    Ok(())
}

/// Returns the set of stored element ids, for backfill orphan-pruning.
/// Filters kind='element' so future pattern rows are never pruned by
/// the element backfill.
pub async fn list_keys(pool: &PgPool) -> anyhow::Result<Vec<String>> {
    let rows = sqlx::query("SELECT id FROM oos.oos_view_schema WHERE kind = 'element'")
        .fetch_all(pool)
        .await?;
    Ok(rows.iter().map(|r| r.get::<String, _>("id")).collect())
}
