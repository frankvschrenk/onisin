//! Read/write the oos.oos_domain_schema table.
//!
//! One row per parsed `.domain` source. Primary key is `context_name`
//! (the domain's `name`) — the legacy column name from the original Go
//! pipeline, kept so search clients and the LLM tool layer stay on the
//! same wire shape. Layout: context_name varchar PK, chunk text,
//! embedding vector, updated_at.
//!
//! Symmetric to global_store; the two tables never overlap (standing
//! instructions live in oos_global_schema). The embedding is serialised
//! via vector::format_vector and cast to ::vector server-side.

use sqlx::{PgPool, Row};

use crate::vector::format_vector;

/// Inserts or replaces one row. ON CONFLICT covers the rebuild path: a
/// domain edited in oosd re-renders its chunk and overwrites the row.
pub async fn upsert(
    pool: &PgPool,
    context_name: &str,
    chunk: &str,
    embedding: &[f32],
) -> anyhow::Result<()> {
    let vec = format_vector(embedding)?;
    sqlx::query(
        "INSERT INTO oos.oos_domain_schema (context_name, chunk, embedding, updated_at)
         VALUES ($1, $2, $3::vector, now())
         ON CONFLICT (context_name) DO UPDATE
         SET chunk = EXCLUDED.chunk, embedding = EXCLUDED.embedding, updated_at = now()",
    )
    .bind(context_name)
    .bind(chunk)
    .bind(vec)
    .execute(pool)
    .await?;
    Ok(())
}

/// Removes a row by primary key. Idempotent — a missing row is fine,
/// which is what the notify handler wants when the source was deleted
/// between the NOTIFY and our follow-up read.
pub async fn delete(pool: &PgPool, context_name: &str) -> anyhow::Result<()> {
    sqlx::query("DELETE FROM oos.oos_domain_schema WHERE context_name = $1")
        .bind(context_name)
        .execute(pool)
        .await?;
    Ok(())
}

/// Returns the set of stored context_names, for backfill orphan-pruning.
pub async fn list_keys(pool: &PgPool) -> anyhow::Result<Vec<String>> {
    let rows = sqlx::query("SELECT context_name FROM oos.oos_domain_schema")
        .fetch_all(pool)
        .await?;
    Ok(rows
        .iter()
        .map(|r| r.get::<String, _>("context_name"))
        .collect())
}
