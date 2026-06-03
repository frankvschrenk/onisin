// rpc.ts — the single seam between the oosd webview and the outside world.
//
// Pfad C transport swap: the Bun/Electrobun gateway is gone. This module
// keeps the EXACT method names and signatures the ~24 panels already call
// (`import { rpc } from "../rpc"`), so the panels compile unchanged, while
// the implementation underneath is rewritten onto three transports:
//
//   • NATS-over-ws  — command/passthrough subjects served by oosai-rs and
//                     oosiam (domain/view/grammar/mappings/iam), plus
//                     JetStream KV (see ./kv).
//   • Tauri invoke  — the handful of genuinely native ops that need OS,
//                     arbitrary-target-DB write, or server-side HTTP
//                     (settings live in plugin-store; clipboard in the
//                     webview; the rest are Rust commands in src-tauri).
//   • local         — calls that used to cross the process boundary but are
//                     now in-webview (open-in-editor, preview toggle, chat
//                     streaming handlers).
//
// The message side of the old RPC shrank to a small local handler registry:
// the detached preview/chat windows are gone, so previewWindowChanged and
// openDslInEditor are emitted locally rather than pushed from another
// process.

import { invoke } from "@tauri-apps/api/core";
import { load as loadStore } from "@tauri-apps/plugin-store";
import { StringCodec } from "nats.ws";

import { getNats, natsRequest, pingBus } from "./nats";
import {
	createKvBucket,
	deleteKvBucket,
	deleteKvEntry,
	listKvBuckets,
	listKvKeys,
	putKvEntry,
	type KvBucketInfo,
	type KvEntry,
} from "./kv";
import { appendLog } from "./store/log-store";
import type { LogRecord } from "./store/log-types";
import { DEFAULT_OOSD_SETTINGS, type OosdSettings } from "./store/settings";

// ─── Shared shapes ───────────────────────────────────────────────
//
// Inlined from the retired gateway modules so the seam is self-contained.

export interface GrammarType {
	id: number;
	name: string;
	source: string;
	tags: string[];
	created_at: string;
}

export interface EventMapping {
	id: number;
	name: string;
	source_schema: string;
	source_table: string;
	source_text_field: string;
	source_id_field: string;
	notify_channel: string;
	target_schema: string;
	target_table: string;
	enabled: boolean;
	created_at: string;
	event_types: string[];
}

export interface IamUser {
	id: number;
	email: string;
	username: string;
	groups: string[];
}

export interface PipelineRow {
	name: string;
	source: string;
	erstellt_am: string;
	geaendert_am: string;
}

export type { KvBucketInfo, KvEntry };

// ─── Common response shapes ──────────────────────────────────────

type Ok = { ok: boolean; error?: string };
type IdsRes = { ids: string[]; error?: string };
type SourceRes = { source: string | null; error?: string };

function errMsg(err: unknown): string {
	return err instanceof Error ? err.message : String(err);
}

// ─── Local message registry ──────────────────────────────────────
//
// The detached windows are gone; these are now in-webview signals.

const handlers = {
	previewWindowChanged: (_msg: { open: boolean }) => {},
	openDslInEditor: (_msg: { kind: "domain" | "view"; id: string; source: string }) => {},
	chatToken: (_msg: { delta: string }) => {},
	chatDone: (_msg: { ok: boolean; error?: string }) => {},
};

/** Most recent source pushed toward a preview surface (read by the in-tab preview). */
let lastPreviewSource: { source: string; viewId: string | null } = { source: "", viewId: null };

// ─── The seam ────────────────────────────────────────────────────

export const rpc = {
	// ── settings (Tauri plugin-store) ──────────────────────────────
	async loadSettings(_?: unknown): Promise<OosdSettings> {
		try {
			const store = await loadStore("settings.json");
			const saved = await store.get<Partial<OosdSettings>>("settings");
			return { ...DEFAULT_OOSD_SETTINGS, ...(saved ?? {}) };
		} catch {
			// No Tauri runtime (plain vite) or first run — fall back to defaults.
			return { ...DEFAULT_OOSD_SETTINGS };
		}
	},
	async saveSettings(next: OosdSettings): Promise<Ok> {
		try {
			const store = await loadStore("settings.json");
			await store.set("settings", next);
			await store.save();
			return { ok: true };
		} catch (err) {
			return { ok: false, error: errMsg(err) };
		}
	},
	async pingNats(_params: { url: string }): Promise<Ok> {
		// In Pfad C the live transport is the websocket bus, not the tcp
		// nats:// URL; reachability of the ws connection is the honest signal.
		try {
			await pingBus();
			return { ok: true };
		} catch (err) {
			return { ok: false, error: errMsg(err) };
		}
	},

	// ── oos.domain (NATS → oosai-rs) ───────────────────────────────
	async listDomain(_?: unknown): Promise<IdsRes> {
		try {
			const res = await natsRequest<{ ids?: string[] }>("oos.cmd.domain.list", {});
			return { ids: res.ids ?? [] };
		} catch (err) {
			return { ids: [], error: errMsg(err) };
		}
	},
	async loadDomain({ id }: { id: string }): Promise<SourceRes> {
		try {
			const res = await natsRequest<{ source: string | null }>("oos.cmd.domain.load", { id });
			return { source: res.source };
		} catch (err) {
			return { source: null, error: errMsg(err) };
		}
	},
	async saveDomain({ id, source }: { id: string; source: string }): Promise<Ok> {
		try {
			return await natsRequest<Ok>("oos.cmd.domain.save", { id, source });
		} catch (err) {
			return { ok: false, error: errMsg(err) };
		}
	},
	// insert is a save by another name — oosai upserts; the panel checks
	// existence before calling, exactly as in the gateway.
	async insertDomain({ id, source }: { id: string; source: string }): Promise<Ok> {
		try {
			return await natsRequest<Ok>("oos.cmd.domain.save", { id, source });
		} catch (err) {
			return { ok: false, error: errMsg(err) };
		}
	},
	async deleteDomain({ id }: { id: string }): Promise<Ok> {
		try {
			return await natsRequest<Ok>("oos.cmd.domain.delete", { id });
		} catch (err) {
			return { ok: false, error: errMsg(err) };
		}
	},

	// ── oos.view (NATS → oosai-rs) ──────────────────────────────────
	async listView(_?: unknown): Promise<IdsRes> {
		try {
			const res = await natsRequest<{ ids?: string[] }>("oos.cmd.view.list", {});
			return { ids: res.ids ?? [] };
		} catch (err) {
			return { ids: [], error: errMsg(err) };
		}
	},
	async loadView({ id }: { id: string }): Promise<SourceRes> {
		try {
			const res = await natsRequest<{ source: string | null }>("oos.cmd.view.load", { id });
			return { source: res.source };
		} catch (err) {
			return { source: null, error: errMsg(err) };
		}
	},
	async saveView({ id, source }: { id: string; source: string }): Promise<Ok> {
		try {
			return await natsRequest<Ok>("oos.cmd.view.save", { id, source });
		} catch (err) {
			return { ok: false, error: errMsg(err) };
		}
	},
	async insertView({ id, source }: { id: string; source: string }): Promise<Ok> {
		try {
			return await natsRequest<Ok>("oos.cmd.view.save", { id, source });
		} catch (err) {
			return { ok: false, error: errMsg(err) };
		}
	},
	async deleteView({ id }: { id: string }): Promise<Ok> {
		try {
			return await natsRequest<Ok>("oos.cmd.view.delete", { id });
		} catch (err) {
			return { ok: false, error: errMsg(err) };
		}
	},

	// ── demo seeds (Tauri — writes arbitrary target DB) ─────────────
	runInternalSeed: ({ dsn }: { dsn: string }) => seed("run_internal_seed", dsn),
	runDemoSeed: ({ dsn }: { dsn: string }) => seed("run_demo_seed", dsn),
	runPoliceSeed: ({ dsn }: { dsn: string }) => seed("run_police_seed", dsn),
	runSupportSeed: ({ dsn }: { dsn: string }) => seed("run_support_seed", dsn),
	runPipelineSeed: ({ dsn }: { dsn: string }) => seed("run_pipeline_seed", dsn),

	// ── JetStream KV (NATS direct) ─────────────────────────────────
	async listKvBuckets(_?: unknown): Promise<{ buckets: KvBucketInfo[]; error?: string }> {
		try {
			return { buckets: await listKvBuckets() };
		} catch (err) {
			return { buckets: [], error: errMsg(err) };
		}
	},
	async createKvBucket({ bucket }: { bucket: string }): Promise<Ok> {
		try {
			await createKvBucket(bucket);
			return { ok: true };
		} catch (err) {
			return { ok: false, error: errMsg(err) };
		}
	},
	async deleteKvBucket({ bucket }: { bucket: string }): Promise<Ok> {
		try {
			await deleteKvBucket(bucket);
			return { ok: true };
		} catch (err) {
			return { ok: false, error: errMsg(err) };
		}
	},
	async listKvKeys({ bucket }: { bucket: string }): Promise<{ entries: KvEntry[]; error?: string }> {
		try {
			return { entries: await listKvKeys(bucket) };
		} catch (err) {
			return { entries: [], error: errMsg(err) };
		}
	},
	async putKvEntry({ bucket, key, value }: { bucket: string; key: string; value: unknown }): Promise<Ok> {
		try {
			await putKvEntry(bucket, key, value);
			return { ok: true };
		} catch (err) {
			return { ok: false, error: errMsg(err) };
		}
	},
	async deleteKvEntry({ bucket, key }: { bucket: string; key: string }): Promise<Ok> {
		try {
			await deleteKvEntry(bucket, key);
			return { ok: true };
		} catch (err) {
			return { ok: false, error: errMsg(err) };
		}
	},

	// ── oosiam (NATS passthrough) ──────────────────────────────────
	async listIamUsers(_?: unknown): Promise<{ users: IamUser[]; error?: string }> {
		try {
			return await natsRequest<{ users: IamUser[] }>("oos.cmd.oosiam.user.list", {});
		} catch (err) {
			return { users: [], error: errMsg(err) };
		}
	},
	async createIamUser(params: { email: string; username: string; password: string; groups: string[] }): Promise<
		{ ok: true; user: IamUser } | { ok: false; error: string }
	> {
		try {
			return await natsRequest("oos.cmd.oosiam.user.create", params);
		} catch (err) {
			return { ok: false, error: errMsg(err) };
		}
	},
	async setIamUserGroups({ id, groups }: { id: number; groups: string[] }): Promise<Ok> {
		try {
			return await natsRequest<Ok>("oos.cmd.oosiam.user.set_groups", { id, groups });
		} catch (err) {
			return { ok: false, error: errMsg(err) };
		}
	},
	async setIamUserPassword({ id, password }: { id: number; password: string }): Promise<Ok> {
		try {
			return await natsRequest<Ok>("oos.cmd.oosiam.user.set_password", { id, password });
		} catch (err) {
			return { ok: false, error: errMsg(err) };
		}
	},
	async deleteIamUser({ id }: { id: number }): Promise<Ok> {
		try {
			return await natsRequest<Ok>("oos.cmd.oosiam.user.delete", { id });
		} catch (err) {
			return { ok: false, error: errMsg(err) };
		}
	},

	// ── pipelines (Tauri — direct write to the target DB) ───────────
	async listPipelines({ dsn }: { dsn: string }): Promise<{ pipelines: PipelineRow[]; error?: string }> {
		try {
			const pipelines = await invoke<PipelineRow[]>("list_pipelines", { dsn });
			return { pipelines };
		} catch (err) {
			return { pipelines: [], error: errMsg(err) };
		}
	},
	async savePipeline({ dsn, name, source }: { dsn: string; name: string; source: string }): Promise<Ok> {
		try {
			await invoke("save_pipeline", { dsn, name, source });
			return { ok: true };
		} catch (err) {
			return { ok: false, error: errMsg(err) };
		}
	},
	async deletePipeline({ dsn, name }: { dsn: string; name: string }): Promise<Ok> {
		try {
			await invoke("delete_pipeline", { dsn, name });
			return { ok: true };
		} catch (err) {
			return { ok: false, error: errMsg(err) };
		}
	},

	// ── events admin ───────────────────────────────────────────────
	async listEventMappings(_?: unknown): Promise<{ mappings: EventMapping[]; error?: string }> {
		try {
			return await natsRequest<{ mappings: EventMapping[] }>("oos.cmd.event_mappings.list", {});
		} catch (err) {
			return { mappings: [], error: errMsg(err) };
		}
	},
	// DDL on an arbitrary target DB is native; the post-DDL refresh is a
	// NATS notify so oosai re-scans event channels (best-effort, as before).
	async execEventContext({ sql }: { sql: string }): Promise<Ok> {
		try {
			await invoke("exec_event_context", { sql });
			try {
				await natsRequest("oos.cmd.event.refresh", {});
			} catch (e) {
				console.warn(`[oosd] event refresh failed (non-fatal): ${errMsg(e)}`);
			}
			return { ok: true };
		} catch (err) {
			return { ok: false, error: errMsg(err) };
		}
	},

	// ── event_type_grammar (NATS → oosai-rs) ───────────────────────
	async listGrammarTypes(_?: unknown): Promise<{ types: GrammarType[]; error?: string }> {
		try {
			const res = await natsRequest<{ types: GrammarType[] }>("oos.cmd.event_type_grammar.list", {});
			return { types: res.types };
		} catch (err) {
			return { types: [], error: errMsg(err) };
		}
	},
	async loadGrammarType({ name }: { name: string }): Promise<SourceRes> {
		try {
			const res = await natsRequest<{ source: string | null }>("oos.cmd.event_type_grammar.load", { name });
			return { source: res.source };
		} catch (err) {
			return { source: null, error: errMsg(err) };
		}
	},
	async saveGrammarType({ name, source }: { name: string; source: string }): Promise<Ok> {
		try {
			return await natsRequest<Ok>("oos.cmd.event_type_grammar.save", { name, source });
		} catch (err) {
			return { ok: false, error: errMsg(err) };
		}
	},
	async saveGrammarTags({ name, tags }: { name: string; tags: string[] }): Promise<Ok> {
		try {
			return await natsRequest<Ok>("oos.cmd.event_type_grammar.save_tags", { name, tags });
		} catch (err) {
			return { ok: false, error: errMsg(err) };
		}
	},
	async insertGrammarType({ name }: { name: string }): Promise<Ok> {
		try {
			return await natsRequest<Ok>("oos.cmd.event_type_grammar.insert", { name });
		} catch (err) {
			return { ok: false, error: errMsg(err) };
		}
	},
	async deleteGrammarType({ name }: { name: string }): Promise<Ok> {
		try {
			return await natsRequest<Ok>("oos.cmd.event_type_grammar.delete", { name });
		} catch (err) {
			return { ok: false, error: errMsg(err) };
		}
	},

	// ── event_mappings.event_types (jsonb on the mapping row) ───────
	async getMappingEventTypes({ mappingId }: { mappingId: number }): Promise<{ eventTypes: string[]; error?: string }> {
		try {
			const res = await natsRequest<{ mappings: Array<{ id: number; event_types: string[] }> }>(
				"oos.cmd.event_mappings.list",
				{},
			);
			const mapping = res.mappings.find((m) => m.id === mappingId);
			return { eventTypes: mapping?.event_types ?? [] };
		} catch (err) {
			return { eventTypes: [], error: errMsg(err) };
		}
	},
	async setMappingEventTypes({ mappingId, eventTypes }: { mappingId: number; eventTypes: string[] }): Promise<Ok> {
		try {
			return await natsRequest<Ok>("oos.cmd.event_mappings.set_types", { mappingId, eventTypes });
		} catch (err) {
			return { ok: false, error: errMsg(err) };
		}
	},

	// ── preview (in-tab; detached window retired) ──────────────────
	async openPreview(_?: unknown): Promise<{ ok: boolean }> {
		handlers.previewWindowChanged({ open: true });
		return { ok: true };
	},
	async closePreview(_?: unknown): Promise<{ ok: boolean }> {
		handlers.previewWindowChanged({ open: false });
		return { ok: true };
	},
	async pushPreviewSource({ source, viewId }: { source: string; viewId: string | null }): Promise<{ ok: boolean }> {
		lastPreviewSource = { source, viewId };
		return { ok: true };
	},

	// ── chat resolver helpers (NATS → oosai-rs) ────────────────────
	async listDomainIds(_?: unknown): Promise<{ ids: string[] }> {
		try {
			const res = await natsRequest<{ ids?: string[] }>("oos.cmd.domain.list", {});
			return { ids: res.ids ?? [] };
		} catch {
			return { ids: [] };
		}
	},
	async listViewIds(_?: unknown): Promise<{ ids: string[] }> {
		try {
			const res = await natsRequest<{ ids?: string[] }>("oos.cmd.view.list", {});
			return { ids: res.ids ?? [] };
		} catch {
			return { ids: [] };
		}
	},
	async loadDomainSource({ id }: { id: string }): Promise<{ source: string | null }> {
		try {
			const res = await natsRequest<{ source?: string | null }>("oos.cmd.domain.load", { id });
			return { source: res.source ?? null };
		} catch {
			return { source: null };
		}
	},
	// The ReAct chat agent has not yet moved into the webview (deferred:
	// panels-backbone first, agent after). Until then chat reports clearly
	// rather than silently doing nothing; the streaming handlers below stay
	// registered so the future webview agent can drive them.
	async chat(_params: unknown): Promise<Ok> {
		return { ok: false, error: "chat agent not yet migrated to the webview" };
	},

	// ── legacy detached chat window (retired — inline panel now) ────
	async openChat(_settings: { llmBaseUrl: string; llmApiKey: string; llmModel: string }): Promise<{ ok: boolean }> {
		return { ok: true };
	},
	async closeChat(_?: unknown): Promise<{ ok: boolean }> {
		return { ok: true };
	},
	async syncLlmSettings(_s: { llmBaseUrl: string; llmApiKey: string; llmModel: string }): Promise<{ ok: boolean }> {
		return { ok: true };
	},

	// ── open DSL in editor (local emit — was cross-process) ─────────
	async openDslInEditor(params: { kind: "domain" | "view"; id: string; source: string }): Promise<{ ok: boolean }> {
		handlers.openDslInEditor(params);
		return { ok: true };
	},

	// ── LLM models (Tauri — server-side HTTP, no CORS) ──────────────
	async listModels({ baseUrl, apiKey }: { baseUrl: string; apiKey: string }): Promise<{ models: string[]; error?: string }> {
		try {
			const models = await invoke<string[]>("list_models", { baseUrl, apiKey });
			return { models };
		} catch (err) {
			return { models: [], error: errMsg(err) };
		}
	},

	// ── Langium grammar source (Tauri — read-only disk reference) ───
	async loadGrammarSource({ kind }: { kind: string }): Promise<SourceRes> {
		try {
			const source = await invoke<string>("load_grammar_source", { kind });
			return { source };
		} catch (err) {
			return { source: null, error: errMsg(err) };
		}
	},

	// ── createTableFromDomain (Tauri — DDL on the target DB) ────────
	async createTableFromDomain({ source, dbUrl }: { source: string; dbUrl: string }): Promise<{ ok: boolean; sql?: string; error?: string }> {
		try {
			const sql = await invoke<string>("create_table_from_domain", { source, dbUrl });
			return { ok: true, sql };
		} catch (err) {
			return { ok: false, error: errMsg(err) };
		}
	},

	// ── clipboard (webview native) ─────────────────────────────────
	async clipboardRead(_?: unknown): Promise<{ text: string | null }> {
		try {
			return { text: await navigator.clipboard.readText() };
		} catch {
			return { text: null };
		}
	},
	async clipboardWrite({ text }: { text: string }): Promise<{ ok: boolean }> {
		try {
			await navigator.clipboard.writeText(text);
			return { ok: true };
		} catch {
			return { ok: false };
		}
	},
};

/** Run a named seed command against the given DSN (Tauri). */
async function seed(command: string, dsn: string): Promise<Ok> {
	if (!dsn) return { ok: false, error: "no DSN provided" };
	try {
		await invoke(command, { dsn });
		return { ok: true };
	} catch (err) {
		return { ok: false, error: errMsg(err) };
	}
}

// ─── Message-side handler setters (unchanged public surface) ─────

export function setPreviewWindowChangedHandler(fn: (msg: { open: boolean }) => void) {
	handlers.previewWindowChanged = fn;
}

export function setOpenDslInEditorHandler(
	fn: (msg: { kind: "domain" | "view"; id: string; source: string }) => void,
) {
	handlers.openDslInEditor = fn;
}

export function setChatTokenHandler(fn: (msg: { delta: string }) => void) {
	handlers.chatToken = fn;
}

export function setChatDoneHandler(fn: (msg: { ok: boolean; error?: string }) => void) {
	handlers.chatDone = fn;
}

// ─── Log mirror ──────────────────────────────────────────────────
//
// Backend services publish LogRecords on "<service>.log". Subscribing to
// the wildcard keeps the Settings → Logs viewer (Dexie ring-buffer) alive
// now that there is no Bun process pushing records to the renderer.
// Best-effort: a missing bus simply yields no log rows.

void (async () => {
	try {
		const nc = await getNats();
		const sc = StringCodec();
		const sub = nc.subscribe("*.log");
		for await (const msg of sub) {
			try {
				const record = JSON.parse(sc.decode(msg.data)) as LogRecord;
				void appendLog(record);
			} catch {
				/* skip malformed log frame */
			}
		}
	} catch {
		/* bus unreachable — logs viewer stays empty, non-fatal */
	}
})();
