//! oosd desktop shell (Tauri 2).
//
// Pfad C: a thin native wrapper around the React webview, which speaks to the
// NATS bus over websocket directly (mainview/nats.ts). This shell contributes
// only the OS plugins the panels use and the handful of genuinely native
// commands in commands.rs; all data flow goes webview → bus, not through here.

mod commands;
mod db;
mod error;
mod seed;

use commands::*;

#[cfg_attr(mobile, tauri::mobile_entry_point)]
pub fn run() {
    tauri::Builder::default()
        .plugin(tauri_plugin_os::init())
        .plugin(tauri_plugin_process::init())
        .plugin(tauri_plugin_dialog::init())
        .plugin(tauri_plugin_store::Builder::new().build())
        .plugin(tauri_plugin_opener::init())
        .plugin(tauri_plugin_fs::init())
        .invoke_handler(tauri::generate_handler![
            list_models,
            load_grammar_source,
            exec_event_context,
            create_table_from_domain,
            list_pipelines,
            save_pipeline,
            delete_pipeline,
            run_internal_seed,
            run_demo_seed,
            run_police_seed,
            run_support_seed,
            run_pipeline_seed,
        ])
        .run(tauri::generate_context!())
        .expect("error while running tauri application");
}
