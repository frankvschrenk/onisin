// src/mainview/types.ts — shared UI data shapes.
//
// Mirror of the Rust telemetry.rs + settings.rs. The field names match the
// JSON the backend emits over the Tauri event bus and returns from the ready
// command (camelCase), so no remapping is needed on this side.

/** One tool-call event, received on the "tool-event" Tauri event. */
export interface ToolEvent {
  id: string;
  tool: string;
  args: unknown;
  durationMs: number;
  status: "ok" | "error";
  error?: string;
  resultSize: number;
  ts: string;
}

/** One structured log record, received on the "log-record" Tauri event. */
export interface LogRecord {
  ts: string;
  level: "error" | "warn" | "info" | "debug";
  service: string;
  source: string;
  message: string;
  fields?: Record<string, unknown>;
}

/** Named NATS server entry. */
export interface NatsServer {
  name: string;
  url: string;
}

/** Allowed filesystem root entry. */
export interface AllowedRoot {
  path: string;
}

/** Connection status, received on the "server-status" Tauri event. */
export interface ServerStatus {
  name: string;
  url: string;
  connected: boolean;
}

/** Persisted bench configuration (tauri-plugin-store settings.json). */
export interface BenchSettings {
  instanceName: string;
  servers: NatsServer[];
  roots: AllowedRoot[];
  otlpPort: number;
  dsn: string;
  appDatabase: string;
}
