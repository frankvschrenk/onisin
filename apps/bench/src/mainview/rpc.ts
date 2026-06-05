// src/mainview/rpc.ts — Tauri bridge for the bench mainview.
//
// Replaces the Electrobun RPC client. Two directions:
//   - commands (invoke): ready / clear / save_settings, the backend
//     #[tauri::command]s in observe.rs.
//   - events (listen): the backend emits "tool-event" / "log-record" /
//     "server-status" on the Tauri event bus; we fan them out to component
//     subscribers through the same pub/sub sets the Electrobun version used,
//     so onToolEvent/onLogRecord/onServerStatus stay synchronous and any number
//     of components can subscribe without re-wiring the transport.

import { invoke } from "@tauri-apps/api/core";
import { listen } from "@tauri-apps/api/event";
import type { BenchSettings, LogRecord, ServerStatus, ToolEvent } from "./types";

export interface ReadyResponse {
  settings: BenchSettings;
  serverStatuses: ServerStatus[];
}

// ─── Commands ────────────────────────────────────────────────

export const rpc = {
  /** Mark the window ready and pull the initial settings + server statuses. */
  ready: () => invoke<ReadyResponse>("ready"),
  /** Drop the backend's buffered (not-yet-flushed) events. */
  clear: () => invoke<{ ok: boolean }>("clear"),
  /** Persist settings; the backend reconnects NATS (otlpPort needs a restart). */
  saveSettings: (args: { settings: BenchSettings }) =>
    invoke<{ ok: boolean }>("save_settings", args),
};

// ─── Events (pub/sub) ────────────────────────────────────────

type ToolEventListener = (e: ToolEvent) => void;
type LogRecordListener = (r: LogRecord) => void;
type ServerStatusListener = (s: ServerStatus) => void;

const toolListeners = new Set<ToolEventListener>();
const logListeners = new Set<LogRecordListener>();
const statusListeners = new Set<ServerStatusListener>();

// One Tauri listener per subject, wired once at module load. The window's
// events outlive any single component, so these are never torn down.
void listen<ToolEvent>("tool-event", (e) => {
  for (const fn of toolListeners) fn(e.payload);
});
void listen<LogRecord>("log-record", (e) => {
  for (const fn of logListeners) fn(e.payload);
});
void listen<ServerStatus>("server-status", (e) => {
  for (const fn of statusListeners) fn(e.payload);
});

export function onToolEvent(fn: ToolEventListener): () => void {
  toolListeners.add(fn);
  return () => { toolListeners.delete(fn); };
}
export function onLogRecord(fn: LogRecordListener): () => void {
  logListeners.add(fn);
  return () => { logListeners.delete(fn); };
}
export function onServerStatus(fn: ServerStatusListener): () => void {
  statusListeners.add(fn);
  return () => { statusListeners.delete(fn); };
}
