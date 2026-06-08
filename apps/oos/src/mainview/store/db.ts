// store/db.ts \u2014 local persistence via Dexie (IndexedDB).
//
// Why the renderer owns persistence now: in the Tauri/Pfad-C model there is
// no Bun gateway and no bun:sqlite file. Chats, turn telemetry, pipeline
// step outputs and the logs ring-buffer all live here; the seam (rpc.ts)
// reads and writes these tables directly so the store modules (chats.ts,
// turns.ts, pipeline-steps.ts, log-store.ts) keep their exact public API.
// Connection settings move to the Tauri plugin-store instead (see rpc.ts).
//
// Dexie over raw IndexedDB: promise surface, one-line version migrations,
// ~25 KB. Atomic single-row writes, no partial-file risk on crashes.

import Dexie, { type Table } from "dexie";

import type { TurnRow, PipelineStepOutputRow } from "./store-types";
import type { LogRecord } from "./log-types";

/** Stored row in the `settings` table (kept for any local section state). */
export interface SettingsRow {
	/** Section identifier \u2014 e.g. "app". One row per section. */
	key:   string;
	/** Section payload \u2014 shape depends on the key. */
	value: unknown;
}

/** Stored row in the `chats` table. */
export interface ChatRow {
	/** Generated id, e.g. "chat_2026-05-03T12-34-56_abcd". */
	id:        string;
	/** Short title shown in the chat list \u2014 first user line, trimmed. */
	title:     string;
	/** ISO timestamp of when the chat was first created. */
	createdAt: string;
	/** ISO timestamp of the last message added to the chat. */
	updatedAt: string;
	/** Full scrollback, JSON-serialised by the chat store before save. */
	messages:  unknown;
}

/**
 * Stored log row \u2014 a LogRecord plus an autoincrement primary key so the
 * ring-buffer can prune oldest-first without a natural key. The bus *.log
 * subscription in rpc.ts appends here; the Logs tab reads newest-first.
 */
export type LogRow = LogRecord & { id?: number };

/**
 * OosDatabase carries the typed table definitions. Bump version() and add a
 * new stores() call to migrate; Dexie applies the upgrade on next open.
 */
class OosDatabase extends Dexie {
	settings!:      Table<SettingsRow,           string>;
	chats!:         Table<ChatRow,               string>;
	turns!:         Table<TurnRow,               string>;
	logs!:          Table<LogRow,                number>;
	pipelineSteps!: Table<PipelineStepOutputRow, [string, number]>;

	constructor() {
		super("oos");

		// v1 \u2014 settings only.
		this.version(1).stores({ settings: "key" });

		// v2 \u2014 chats added. `&id` = unique primary key; `updatedAt`
		// secondary index so the reverse-chronological list is O(log n).
		this.version(2).stores({
			settings: "key",
			chats:    "&id, updatedAt",
		});

		// v3 \u2014 turns added (one row per finished turn; Activity tab).
		this.version(3).stores({
			settings: "key",
			chats:    "&id, updatedAt",
			turns:    "&id, createdAt, chatId, model, status",
		});

		// v4 \u2014 logs ring-buffer + pipeline step outputs. Logs use an
		// autoincrement key (`++id`) indexed on `ts`; steps use a compound
		// [turnId+stepIndex] primary key with a `turnId` index so the
		// inspector loads one turn's steps without a scan. Both tables move
		// here from the old bun:sqlite store; no data migration needed since
		// the gateway file does not come along to the Tauri build.
		this.version(4).stores({
			settings:      "key",
			chats:         "&id, updatedAt",
			turns:         "&id, createdAt, chatId, model, status",
			logs:          "++id, ts",
			pipelineSteps: "[turnId+stepIndex], turnId",
		});
	}
}

/** Singleton \u2014 one shared connection per renderer. */
export const db = new OosDatabase();
