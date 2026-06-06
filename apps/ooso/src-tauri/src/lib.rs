//! ooso desktop shell (Tauri 2) — the Onisin operator console.
//
// Pfad C: a thin native wrapper around the React webview, which speaks to the
// NATS bus over websocket directly (mainview/nats.ts). It subscribes to
// status.*, renders the live/stale/gone process table, and publishes ooso's
// own heartbeat on status.ooso. The only thing the webview cannot do itself is
// learn this machine's stable node id / host / pid, so the single native
// command get_identity provides them.

mod commands;

use commands::get_identity;

#[cfg_attr(mobile, tauri::mobile_entry_point)]
pub fn run() {
    tauri::Builder::default()
        .plugin(tauri_plugin_positioner::init())
        .invoke_handler(tauri::generate_handler![get_identity])
        .run(tauri::generate_context!())
        .expect("error while running tauri application");
}
