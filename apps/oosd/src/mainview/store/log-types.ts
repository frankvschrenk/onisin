// log-types.ts — local log record shape for oosd.
//
// Inlined from the retired oos-logger-ts package: the migrated app is a
// thin Tauri webview with no Bun gateway to pull that package from, and
// the renderer only needs the wire shape, not the publishing logic.

/** Severity name aligned with OTel SeverityNumber (error highest). */
export type LogLevelName = "error" | "warn" | "info" | "debug";

/**
 * LogRecord is the wire format published by backend services on the
 * NATS subject "<service>.log" and mirrored into the Dexie ring-buffer
 * that backs the Settings → Logs viewer.
 */
export type LogRecord = {
	/** ISO-8601 timestamp. */
	ts: string;
	/** Severity. */
	level: LogLevelName;
	/** Originating service: "oos" | "oosd" | "oosai" | "oosgql". */
	service: string;
	/** Sub-module within the service, e.g. "pipeline" | "schema-host". */
	source: string;
	/** Human-readable message. */
	message: string;
	/** Optional structured fields (domain id, SQL, etc.). */
	fields?: Record<string, unknown>;
};
