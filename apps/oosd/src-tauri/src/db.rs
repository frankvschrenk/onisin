//! DB-admin helpers for the native commands. Each connects ad-hoc to a target
//! database the user points at — the Pipelines panel DSN, or the configured
//! admin DB (settings.dbUrl) for event-context DDL — with a short-lived
//! single-connection pool and runtime queries. Postgres only. Mirrors the old
//! Bun gateway's pipeline-rpc.ts / events-rpc.ts.

use serde::Serialize;
use sqlx::postgres::PgPoolOptions;
use sqlx::{PgPool, Row};

use crate::error::CmdError;

/// One row of public.pipelines, wire-shaped for the Pipelines panel
/// (field names match the PipelineRow interface in mainview/rpc.ts).
#[derive(Serialize)]
pub struct PipelineRow {
    pub name: String,
    pub source: String,
    pub erstellt_am: String,
    pub geaendert_am: String,
}

/// Open a short-lived single-connection pool to the given DSN.
async fn connect(dsn: &str) -> Result<PgPool, CmdError> {
    if dsn.is_empty() {
        return Err(CmdError::Msg("no DSN".into()));
    }
    let pool = PgPoolOptions::new().max_connections(1).connect(dsn).await?;
    Ok(pool)
}

/// List all pipelines ordered by name.
pub async fn list_pipelines(dsn: &str) -> Result<Vec<PipelineRow>, CmdError> {
    let pool = connect(dsn).await?;
    // Timestamps read as ::text so there is no chrono mapping to depend on.
    let rows = sqlx::query(
        "SELECT name, source, erstellt_am::text AS erstellt_am, \
         geaendert_am::text AS geaendert_am FROM public.pipelines ORDER BY name",
    )
    .fetch_all(&pool)
    .await?;
    let out = rows
        .into_iter()
        .map(|r| PipelineRow {
            name: r.get::<String, _>("name"),
            source: r.get::<String, _>("source"),
            erstellt_am: r.get::<Option<String>, _>("erstellt_am").unwrap_or_default(),
            geaendert_am: r.get::<Option<String>, _>("geaendert_am").unwrap_or_default(),
        })
        .collect();
    pool.close().await;
    Ok(out)
}

/// Insert or update a pipeline by name.
pub async fn save_pipeline(dsn: &str, name: &str, source: &str) -> Result<(), CmdError> {
    if name.is_empty() {
        return Err(CmdError::Msg("name is required".into()));
    }
    let pool = connect(dsn).await?;
    sqlx::query(
        "INSERT INTO public.pipelines (name, source) VALUES ($1, $2) \
         ON CONFLICT (name) DO UPDATE SET source = EXCLUDED.source, geaendert_am = now()",
    )
    .bind(name)
    .bind(source)
    .execute(&pool)
    .await?;
    pool.close().await;
    Ok(())
}

/// Delete a pipeline by name.
pub async fn delete_pipeline(dsn: &str, name: &str) -> Result<(), CmdError> {
    if name.is_empty() {
        return Err(CmdError::Msg("name is required".into()));
    }
    let pool = connect(dsn).await?;
    sqlx::query("DELETE FROM public.pipelines WHERE name = $1")
        .bind(name)
        .execute(&pool)
        .await?;
    pool.close().await;
    Ok(())
}

/// Execute a raw DDL block (event-context creation) against the admin DB.
///
/// Postgres' simple-query protocol parses the whole multi-statement string
/// itself — including $$-quoted PL/pgSQL function bodies — so no client-side
/// statement splitting is needed (that is exactly what the old dollar-quote
/// aware splitSql worked around; raw_sql sidesteps it).
pub async fn exec_sql(dsn: &str, sql: &str) -> Result<(), CmdError> {
    let pool = connect(dsn).await?;
    sqlx::raw_sql(sql).execute(&pool).await?;
    pool.close().await;
    Ok(())
}
