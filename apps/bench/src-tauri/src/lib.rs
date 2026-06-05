//! bench desktop shell (Tauri 2).
//
// Unlike oosd/oos (Pfad C: a thin shell whose webview talks to the bus over
// websocket), bench's backend IS a NATS request-reply responder. Its tools
// touch the local filesystem, spawn processes and reach Postgres — work that
// must run natively, not in the sandboxed webview. So the dispatcher boots with
// the app and answers bench.* for the app's lifetime; the window is a status
// surface fed over the Tauri event bus, not the data path.

mod ctx;
mod db;
mod dispatcher;
mod error;
mod events_log;
mod observe;
mod roots;
mod settings;
mod telemetry;
mod tools;

use std::sync::Arc;

use tauri::Manager;
use tokio::sync::{mpsc, RwLock};

#[cfg_attr(mobile, tauri::mobile_entry_point)]
pub fn run() {
    tauri::Builder::default()
        .plugin(tauri_plugin_os::init())
        .plugin(tauri_plugin_process::init())
        .plugin(tauri_plugin_dialog::init())
        .plugin(tauri_plugin_store::Builder::new().build())
        .plugin(tauri_plugin_opener::init())
        .plugin(tauri_plugin_fs::init())
        // Window-facing commands (replace the Electrobun RPC of the same names).
        .invoke_handler(tauri::generate_handler![
            observe::ready,
            observe::clear,
            observe::save_settings
        ])
        .setup(|app| {
            let handle = app.handle().clone();

            // Settings are loaded once and shared (RwLock) so the pg.* tools and
            // the reconnect supervisor read the same live snapshot. The OTLP
            // port is captured up front: the receiver binds once at boot, so a
            // port change needs a restart (as in the Bun bench).
            let settings = settings::load(&handle);
            let otlp_port = settings.otlp_port;
            let settings = Arc::new(RwLock::new(settings));

            // Local SQLite audit log. The app data dir may not exist on first
            // boot, so create it before opening events.db.
            let mut db_path = handle
                .path()
                .app_data_dir()
                .expect("resolve app data dir");
            let _ = std::fs::create_dir_all(&db_path);
            db_path.push("events.db");

            // save_settings → supervisor reconnect channel.
            let (reconnect_tx, reconnect_rx) = mpsc::channel::<()>(8);
            let state = Arc::new(observe::AppState::new(settings, reconnect_tx));
            app.manage(state.clone());

            let app_async = handle.clone();
            tauri::async_runtime::spawn(async move {
                let event_log = Arc::new(events_log::EventLog::open(&db_path).await);
                observe::start_otlp(otlp_port, app_async.clone(), state.clone());
                let boot = observe::Boot {
                    app: app_async,
                    state,
                    event_log,
                };
                observe::supervisor(boot, reconnect_rx).await;
            });
            Ok(())
        })
        .run(tauri::generate_context!())
        .expect("error while running tauri application");
}
