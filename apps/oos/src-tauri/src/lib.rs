//! oos desktop shell (Tauri 2).
//
// Pfad C: a thin native wrapper around the React webview, which speaks to the
// NATS bus over websocket directly (mainview/nats.ts) and persists locally in
// Dexie. This shell contributes only the OS plugins the panels use and the
// three genuinely native commands in commands.rs; all data flow goes
// webview \u2192 bus / IndexedDB, not through here.

mod auth;
mod commands;
mod error;

use commands::*;

#[cfg_attr(mobile, tauri::mobile_entry_point)]
pub fn run() {
    tauri::Builder::default()
        .plugin(tauri_plugin_positioner::init())
        .plugin(tauri_plugin_window_state::Builder::default().build())
        .plugin(tauri_plugin_os::init())
        .plugin(tauri_plugin_process::init())
        .plugin(tauri_plugin_dialog::init())
        .plugin(tauri_plugin_store::Builder::new().build())
        .plugin(tauri_plugin_opener::init())
        .plugin(tauri_plugin_fs::init())
        .invoke_handler(tauri::generate_handler![
            list_models,
            node_id,
            open_external_url,
            start_login,
            get_auth_status,
            logout,
        ])
        .run(tauri::generate_context!())
        .expect("error while running tauri application");
}
