//! The one native command the operator console needs. Everything else
//! (status.* subscription, env.show requests, the heartbeat) is webview-direct
//! over nats.ws. A browser context cannot learn its own process identity, so
//! this hands the webview the fields its status.ooso heartbeat must carry.

use serde::Serialize;
use tauri::Manager;

/// Process identity for ooso's own heartbeat. camelCase on the wire so the
/// webview can spread it straight into the HeartbeatPayload it publishes.
#[derive(Serialize)]
#[serde(rename_all = "camelCase")]
pub struct Identity {
    node_id: String,
    host: String,
    pid: u32,
    version: String,
}

/// Return this install's stable node id, host name, pid and version.
///
/// The node id is generated once and persisted under the app data dir, so the
/// operator table keeps the same row for ooso across restarts (the dashboard
/// keys rows by node id).
#[tauri::command]
pub fn get_identity(app: tauri::AppHandle) -> Result<Identity, String> {
    let node_id = load_or_create_node_id(&app).map_err(|e| e.to_string())?;
    Ok(Identity {
        node_id,
        host: gethostname::gethostname().to_string_lossy().into_owned(),
        pid: std::process::id(),
        version: env!("CARGO_PKG_VERSION").to_string(),
    })
}

// Read the persisted node id, or mint one from random uuid bytes encoded as
// uppercase Base32 (the same look the old Electrobun ooso showed) and persist
// it. Errors bubble up as a string the command turns into a JS rejection.
fn load_or_create_node_id(app: &tauri::AppHandle) -> Result<String, Box<dyn std::error::Error>> {
    let dir = app.path().app_data_dir()?;
    std::fs::create_dir_all(&dir)?;
    let path = dir.join("node-id");
    if let Ok(existing) = std::fs::read_to_string(&path) {
        let trimmed = existing.trim();
        if !trimmed.is_empty() {
            return Ok(trimmed.to_string());
        }
    }
    let bytes = uuid::Uuid::new_v4().into_bytes();
    let id = data_encoding::BASE32_NOPAD.encode(&bytes);
    std::fs::write(&path, &id)?;
    Ok(id)
}
