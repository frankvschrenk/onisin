//! Read/write the oos.oos_global_schema table.
//!
//! One row per standing-instruction prompt from oos.global_prompt.
//! Primary key is plain `name` (the table defines the namespace).
//! Layout: name varchar PK, chunk text, embedding vector, updated_at.
//!
//! Symmetric to the (not-yet-ported) domain and view stores. The
//! embedding is serialised via vector::format_vector and cast to
//! ::vector server-side.

use sqlx::{PgPool, Row};

use crate::vector::format_vector;

/// Inserts or replaces one row. ON CONFLICT covers the rebuild path:
/// a prompt edited in oosd fires pg_notify, the pipeline re-embeds,
/// and this overwrites the previous row.
pub async fn upsert(pool: &PgPool, name: &str, chunk: &str, embedding: &[f32]) -> anyhow::Result<()> {
    let vec = format_vector(embedding)?;
    sqlx::query(
        "INSERT INTO oos.oos_global_schema (name, chunk, embedding, updated_at)
         VALUES ($1, $2, $3::vector, now())
         ON CONFLICT (name) DO UPDATE
         SET chunk = EXCLUDED.chunk, embedding = EXCLUDED.embedding, updated_at = now()",
    )
    .bind(name)
    .bind(chunk)
    .bind(vec)
    .execute(pool)
    .await?;
    Ok(())
}

/// Removes a row by primary key. Idempotent — a missing row is fine,
/// which is what the notify handler wants when the source was deleted
/// between the NOTIFY and our follow-up read.
pub async fn delete(pool: &PgPool, name: &str) -> anyhow::Result<()> {
    sqlx::query("DELETE FROM oos.oos_global_schema WHERE name = $1")
        .bind(name)
        .execute(pool)
        .await?;
    Ok(())
}

/// Returns the set of stored names, for backfill orphan-pruning.
pub async fn list_keys(pool: &PgPool) -> anyhow::Result<Vec<String>> {
    let rows = sqlx::query("SELECT name FROM oos.oos_global_schema")
        .fetch_all(pool)
        .await?;
    Ok(rows.iter().map(|r| r.get::<String, _>("name")).collect())
}
