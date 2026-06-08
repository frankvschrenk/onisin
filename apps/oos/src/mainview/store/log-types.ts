// log-types.ts \u2014 local log + error record shapes for oos.
//
// Inlined from the retired oos-logger-ts package: the migrated app is a thin
// Tauri webview with no Bun gateway to pull that package from, and the
// renderer only needs the wire shapes, not the publishing logic.

/** Severity name aligned with OTel SeverityNumber (error highest). */
export type LogLevelName = "error" | "warn" | "info" | "debug";

/**
 * LogRecord is the wire format published by backend services on the NATS
 * subject "<service>.log" and mirrored into the Dexie ring-buffer that backs
 * the Settings \u2192 Logs viewer.
 */
export type LogRecord = {
	/** ISO-8601 timestamp. */
	ts: string;
	/** Severity. */
	level: LogLevelName;
	/** Originating service: "oos" | "oosd" | "oosai" | "oosgql". */
	service: string;
	/** Sub-module within the service, e.g. "pipeline" | "agent". */
	source: string;
	/** Human-readable message. */
	message: string;
	/** Optional structured fields (domain id, SQL, etc.). */
	fields?: Record<string, unknown>;
};

/**
 * OosError is pushed when a backend error belongs to this oos instance. The
 * seam turns it into a LogRecord for the Logs tab and surfaces it in the
 * app-level error drawer.
 */
export interface OosError {
	ts:      string;
	service: string;
	subject: string;
	error:   string;
}
