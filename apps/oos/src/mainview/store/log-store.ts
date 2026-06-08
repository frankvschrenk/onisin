// store/log-store.ts \u2014 log record store via Dexie (IndexedDB).
//
// In the Tauri/Pfad-C model there is no Bun process persisting logs. Backend
// services publish LogRecords on the NATS subject "<service>.log"; the seam
// (rpc.ts) subscribes to "*.log" and calls appendLog for each frame. This
// module owns the local ring-buffer: appendLog inserts, getRecentLogs reads
// newest-first, clearLogs empties it. Live updates fan out via in-memory
// pub/sub so the Settings \u2192 Logs tab refreshes without polling.

import { db } from "./db";
import type { LogRecord } from "./log-types";

export type { LogRecord };

/** Upper bound on retained rows; oldest are pruned past this. */
const MAX_LOG_ROWS = 2000;

// \u2500\u2500\u2500 Public API \u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500

/** appendLog persists one record and prunes the ring-buffer if oversized. */
export async function appendLog(record: LogRecord): Promise<void> {
	try {
		await db.logs.add({ ...record });
		const count = await db.logs.count();
		if (count > MAX_LOG_ROWS) {
			// Drop the oldest overflow in one pass (autoincrement id is
			// monotonic, so the first N keys are the oldest).
			const overflow = count - MAX_LOG_ROWS;
			const oldest = await db.logs.orderBy("id").limit(overflow).primaryKeys();
			await db.logs.bulkDelete(oldest);
		}
	} catch {
		/* persistence failure is non-fatal for a log viewer */
	}
	notify();
}

/** getRecentLogs fetches the most recent persisted records, newest first. */
export async function getRecentLogs(limit = 200): Promise<LogRecord[]> {
	try {
		const rows = await db.logs.orderBy("id").reverse().limit(limit).toArray();
		return rows.map(({ id: _id, ...rec }) => rec);
	} catch {
		return [];
	}
}

/** clearLogs empties the ring-buffer. */
export async function clearLogs(): Promise<void> {
	try {
		await db.logs.clear();
	} catch {
		/* non-fatal */
	}
	notify();
}

// \u2500\u2500\u2500 Pub/sub for live updates \u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500

type Listener = () => void;
const listeners = new Set<Listener>();
function notify(): void { for (const fn of listeners) fn(); }

/** subscribe registers a callback invoked when new logs arrive. */
export function subscribe(fn: Listener): () => void {
	listeners.add(fn);
	return () => { listeners.delete(fn); };
}
