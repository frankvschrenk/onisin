//! Native Tauri commands the oosd webview invokes for the few operations a
//! browser context cannot do itself. Everything NATS / JetStream-KV / settings
//! is webview-direct (see mainview/rpc.ts); only genuinely native calls land
//! here: server-side LLM HTTP (no CORS, key off the fetch path), the read-only
//! Langium grammar reference, and DB-admin writes against an arbitrary target
//! database.

use tauri_plugin_store::StoreExt;

use crate::error::CmdError;

/// Default admin DB if settings carry none yet (matches the JS default).
const DEFAULT_DB_URL: &str = "postgres://postgres:demo@localhost:5432/onisin";

/// Read settings.dbUrl from the plugin-store (the same settings.json the
/// webview writes). event-context DDL runs against this configured admin DB,
/// exactly as the old gateway used its settings.dbUrl client.
fn settings_db_url(app: &tauri::AppHandle) -> String {
    app.store("settings.json")
        .ok()
        .and_then(|store| store.get("settings"))
        .and_then(|v| v.get("dbUrl").and_then(|d| d.as_str().map(String::from)))
        .filter(|s| !s.is_empty())
        .unwrap_or_else(|| DEFAULT_DB_URL.into())
}

/// Fetch model ids from an OpenAI-compatible /v1/models endpoint.
///
/// Server-side so the key never rides a browser fetch and CORS is moot; this
/// is why the call is native rather than a webview fetch.
#[tauri::command]
pub async fn list_models(base_url: String, api_key: String) -> Result<Vec<String>, String> {
    fetch_models(&base_url, &api_key).await.map_err(|e| e.to_string())
}

async fn fetch_models(base_url: &str, api_key: &str) -> Result<Vec<String>, CmdError> {
    let url = format!("{}/v1/models", base_url.trim_end_matches('/'));
    let key = if api_key.is_empty() { "sk-no-key" } else { api_key };
    let res = reqwest::Client::new().get(&url).bearer_auth(key).send().await?;
    if !res.status().is_success() {
        return Err(CmdError::Msg(format!(
            "models endpoint returned HTTP {}",
            res.status().as_u16()
        )));
    }
    let body: serde_json::Value = res.json().await?;
    // OpenAI shape is { data: [{ id }] }; tolerate a bare array too.
    let arr = body.get("data").and_then(|d| d.as_array());
    let mut ids: Vec<String> = match arr {
        Some(items) => items
            .iter()
            .filter_map(|m| m.get("id").and_then(|i| i.as_str()).map(String::from))
            .collect(),
        None => Vec::new(),
    };
    ids.sort();
    Ok(ids)
}

/// Return the read-only Langium grammar source for the editor reference panel.
///
/// Embedded at compile time from packages/oos-dsls-ts/grammar so the binary
/// carries the canonical source with no runtime path or resource-bundling
/// concern — the grammars are tiny and authored in that package.
#[tauri::command]
pub fn load_grammar_source(kind: String) -> Result<String, String> {
    let src = match kind.as_str() {
        "domain" => include_str!("../../../../packages/oos-dsls-ts/grammar/domain.langium"),
        "view" => include_str!("../../../../packages/oos-dsls-ts/grammar/view.langium"),
        "event-schema" => {
            include_str!("../../../../packages/oos-dsls-ts/grammar/event-schema.langium")
        }
        other => return Err(format!("unknown grammar kind: {other}")),
    };
    Ok(src.to_string())
}

// ── DB-admin commands ────────────────────────────────────────
//
// These write against a target database (the Pipelines panel DSN, the configured
// admin DB for event-context DDL, or the panel DSN for create_table_from_domain)
// via crate::db. The internal/demo/pipeline seeds are real too (crate::seed).
// Only the police/support seeds stay stubbed with an explicit error (NOT_YET):
// they need oosai's event-insert REST API, which the NATS-only oosai-rs does
// not yet serve — a caller fails loudly rather than silently succeeding.

const NOT_YET: &str = "not yet migrated to the Tauri shell";

/// Run an event-context DDL block against the configured admin DB.
/// The post-DDL oos.cmd.event.refresh notify is fired by the seam, not here.
#[tauri::command]
pub async fn exec_event_context(app: tauri::AppHandle, sql: String) -> Result<(), String> {
    let dsn = settings_db_url(&app);
    crate::db::exec_sql(&dsn, &sql).await.map_err(|e| e.to_string())
}

/// Parse a .domain source, generate CREATE TABLE IF NOT EXISTS via oos-dsls,
/// and execute it against the target DB. Returns the executed DDL on success.
/// DDL generation is local (the shell has the source) — no NATS round-trip.
#[tauri::command]
pub async fn create_table_from_domain(source: String, db_url: String) -> Result<String, String> {
    if db_url.is_empty() {
        return Err("No database URL configured in Settings → Database".into());
    }
    let def = oos_dsls::domain::parse_domain(&source).map_err(|e| format!("DSL parse error: {e}"))?;
    let ddl = oos_dsls::domain::domain_to_ddl(&def);
    crate::db::exec_sql(&db_url, &ddl).await.map_err(|e| e.to_string())?;
    Ok(ddl)
}

/// List pipelines from public.pipelines on the panel's target DSN.
#[tauri::command]
pub async fn list_pipelines(dsn: String) -> Result<Vec<crate::db::PipelineRow>, String> {
    crate::db::list_pipelines(&dsn).await.map_err(|e| e.to_string())
}

/// Insert or update a pipeline on the panel's target DSN.
#[tauri::command]
pub async fn save_pipeline(dsn: String, name: String, source: String) -> Result<(), String> {
    crate::db::save_pipeline(&dsn, &name, &source).await.map_err(|e| e.to_string())
}

/// Delete a pipeline by name on the panel's target DSN.
#[tauri::command]
pub async fn delete_pipeline(dsn: String, name: String) -> Result<(), String> {
    crate::db::delete_pipeline(&dsn, &name).await.map_err(|e| e.to_string())
}

/// Seed the internal oos schema.
#[tauri::command]
pub async fn run_internal_seed(dsn: String) -> Result<(), String> {
    crate::seed::run_internal(&dsn).await.map_err(|e| e.to_string())
}

/// Seed the public demo schema, data and the bundled DSL sources.
#[tauri::command]
pub async fn run_demo_seed(dsn: String) -> Result<(), String> {
    crate::seed::run_demo(&dsn).await.map_err(|e| e.to_string())
}

#[tauri::command]
pub fn run_police_seed(_dsn: String) -> Result<(), String> {
    Err(NOT_YET.into())
}

#[tauri::command]
pub fn run_support_seed(_dsn: String) -> Result<(), String> {
    Err(NOT_YET.into())
}

/// Seed the pipeline demo: schema, 110 documents, 3 demo pipelines, S3 upload.
#[tauri::command]
pub async fn run_pipeline_seed(dsn: String) -> Result<(), String> {
    crate::seed::run_pipeline(&dsn).await.map_err(|e| e.to_string())
}
