//! PostgreSQL connection pool.
//!
//! Several services need pgvector or plain relational access. The pool
//! is opened once at startup and shared across handlers.
//!
//! Postgres-only by design for now: the Bun services routed through Knex
//! and nominally supported mysql/mssql/oracle, but every deployment runs
//! postgres and pgvector is postgres-specific. Multi-dialect, if it ever
//! returns, is a separate layer — not a hidden default here.

use sqlx::postgres::PgPoolOptions;
use sqlx::PgPool;

/// Opens the pool and pings it once so a misconfigured pgUrl fails
/// loudly at startup rather than on the first query.
pub async fn connect(url: &str) -> anyhow::Result<PgPool> {
    let pool = PgPoolOptions::new()
        .max_connections(5)
        .connect(url)
        .await?;
    sqlx::query("SELECT 1").execute(&pool).await?;
    Ok(pool)
}
