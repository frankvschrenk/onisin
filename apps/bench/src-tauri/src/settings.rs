//! bench settings — persisted by the webview via tauri-plugin-store
//! (settings.json, key "settings") and read here at boot to configure the
//! dispatcher.
//
// The Bun bench kept these in oos-store-ts SQLite; the Tauri model uses the
// store plugin like oosd/oos. The camelCase serde names keep the JSON
// byte-compatible with what the settings UI writes.

use serde::{Deserialize, Serialize};
use tauri::AppHandle;
use tauri_plugin_store::StoreExt;

#[derive(Clone, Debug, Serialize, Deserialize)]
pub struct NatsServer {
    pub name: String,
    pub url: String,
}

#[derive(Clone, Debug, Serialize, Deserialize)]
pub struct AllowedRoot {
    pub path: String,
}

#[derive(Clone, Debug, Serialize, Deserialize)]
#[serde(rename_all = "camelCase", default)]
pub struct BenchSettings {
    /// Human-readable name for this instance, e.g. "macos". Empty = no
    /// instance-targeted subscription, only the shared bench.> queue.
    pub instance_name: String,
    pub servers: Vec<NatsServer>,
    pub roots: Vec<AllowedRoot>,
    pub otlp_port: u16,
    /// Host-only PostgreSQL DSN, no dbname segment. bench appends "/<appDatabase>"
    /// for pg.* tools. Empty disables the pg.* tools.
    pub dsn: String,
    /// Application database name the pg.* tools operate on (typically "onisin").
    pub app_database: String,
}

impl Default for BenchSettings {
    // otlpPort defaults to the OTLP/HTTP convention 4318; everything else is
    // empty until the user fills Settings. #[serde(default)] above pulls each
    // missing field from here, so rows written by older versions never crash.
    fn default() -> Self {
        Self {
            instance_name: String::new(),
            servers: Vec::new(),
            roots: Vec::new(),
            otlp_port: 4318,
            dsn: String::new(),
            app_database: String::new(),
        }
    }
}

/// Load settings from the store, falling back to defaults when the file or the
/// "settings" key is absent (first boot) or malformed.
pub fn load(app: &AppHandle) -> BenchSettings {
    let Ok(store) = app.store("settings.json") else {
        return BenchSettings::default();
    };
    match store.get("settings") {
        Some(value) => serde_json::from_value(value).unwrap_or_default(),
        None => BenchSettings::default(),
    }
}

/// Persist settings to the same store the boot path reads (settings.json, key
/// "settings"). The save_settings command calls this before triggering a
/// reconnect.
pub fn save(app: &AppHandle, settings: &BenchSettings) -> Result<(), String> {
    let store = app.store("settings.json").map_err(|e| e.to_string())?;
    let value = serde_json::to_value(settings).map_err(|e| e.to_string())?;
    store.set("settings", value);
    store.save().map_err(|e| e.to_string())
}
