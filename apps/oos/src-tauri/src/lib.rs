//! oos desktop shell (Tauri 2).
//
// Pfad C: a thin native wrapper around the React webview, which speaks to the
// NATS bus over websocket directly (mainview/nats.ts) and persists locally in
// Dexie. This shell contributes only the OS plugins the panels use and the
// genuinely native commands in commands.rs and dev.rs; all data flow goes
// webview → bus / IndexedDB, not through here.
//
// dev.rs adds the RIG-backed Dev agent as a Tauri command so the DevPanel
// can invoke an agentic loop with access to bench tools (fs, exec, git)
// without running that loop inside the webview's JS context.

mod auth;
mod commands;
mod dev;
mod error;

use commands::*;
use dev::dev_run;
use tauri_plugin_window_state::StateFlags;

#[cfg_attr(mobile, tauri::mobile_entry_point)]
pub fn run() {
    tauri::Builder::default()
        .plugin(tauri_plugin_positioner::init())
        // Exclude DECORATIONS from the persisted window state: the window is
        // frameless via tauri.conf (decorations:false), but the window-state
        // plugin's default flags include DECORATIONS and would otherwise
        // restore a previously-saved decorated=true, re-showing the native
        // title bar over the custom one.
        .plugin(
            tauri_plugin_window_state::Builder::default()
                .with_state_flags(StateFlags::all() & !StateFlags::DECORATIONS)
                .build(),
        )
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
            dev_run,
        ])
        .run(tauri::generate_context!())
        .expect("error while running tauri application");
}
