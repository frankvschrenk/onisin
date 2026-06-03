// log-store.ts — Dexie ring-buffer for log records in the desktop apps.
//
// Stores up to MAX_ENTRIES records. When the limit is exceeded the
// oldest records are pruned so the table never grows unbounded.
//
// The store is write-only from the bun side (via the logRecord push
// event) and read-only from the Settings → Logs viewer.

import Dexie, { type Table } from "dexie";
import type { LogRecord }     from "./log-types";

// ─── Schema ───────────────────────────────────────────────────────────

export interface LogEntry extends LogRecord {
	/** Auto-incremented primary key — used for ordering and pruning. */
	id?: number;
}

class LogDb extends Dexie {
	logs!: Table<LogEntry, number>;

	constructor() {
		super("oosd-logs");
		this.version(1).stores({
			logs: "++id, ts, level, service, source",
		});
	}
}

const db = new LogDb();

const MAX_ENTRIES = 500;

// ─── Public API ───────────────────────────────────────────────────────

/** Append a LogRecord. Prunes oldest entries when MAX_ENTRIES is exceeded. */
export async function appendLog(record: LogRecord): Promise<void> {
	await db.logs.add(record as LogEntry);
	const count = await db.logs.count();
	if (count > MAX_ENTRIES) {
		const excess = count - MAX_ENTRIES;
		const oldest = await db.logs.orderBy("id").limit(excess).primaryKeys();
		await db.logs.bulkDelete(oldest as number[]);
	}
}

/** Return the most recent `limit` entries, newest first. */
export async function getRecentLogs(limit = 200): Promise<LogEntry[]> {
	return db.logs.orderBy("id").reverse().limit(limit).toArray();
}

/** Delete all log entries. */
export async function clearLogs(): Promise<void> {
	await db.logs.clear();
}
