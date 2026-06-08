// rpc.ts \u2014 the seam between the webview and the rest of the system.
//
// Replaces the Electrobun defineRPC client. In the Tauri/Pfad-C model there is
// no Bun gateway: the webview reaches the bus directly (nats.ts / kv.ts),
// persists locally in Dexie (store/db.ts), and calls a handful of native host
// commands through Tauri invoke. This module keeps the EXACT method names and
// signatures the old OosRPC.bun.requests exposed, so every panel that calls
// rpc.<method>() compiles and runs unchanged.
//
// Method routing falls into five buckets:
//   (a) NATS passthrough  \u2014 proxies that used to hit oosai/oosgql over HTTP
//                            now Request-Reply straight to the subjects.
//   (b) native (Tauri)    \u2014 list_models, settings, clipboard, node id.
//   (c) local persistence \u2014 chats / turns / pipeline steps / kv in Dexie.
//   (d) not-yet-migrated  \u2014 headless turn engine + pipeline runner (Rust
//                            slice pending) and the event subsystem (same
//                            block as oosd's police/support seeds).
//   (e) push (bus->webview)\u2014 *.log subscription feeds the Logs tab; agent /
//                            pipeline streams arrive once the headless agent
//                            publishes them (subscribe* registries are ready).
//
// Circular-dependency note: the store modules (chats/turns/pipeline-steps/
// ui-state/finetuning) call rpc.* and the seam backs them with Dexie, so the
// dependency points one way (store -> rpc -> db). log-store.ts is the lone
// exception \u2014 the seam calls appendLog from the *.log subscription, so
// log-store owns its Dexie writes directly instead of routing back through here.

import { invoke } from "@tauri-apps/api/core";
import { load as loadTauriStore } from "@tauri-apps/plugin-store";

import { natsRequest, pingBus, getNats, jsonCodec } from "./nats";
import { db, type ChatRow } from "./store/db";
import type { TurnRow, PipelineStepOutputRow } from "./store/store-types";
import type { AgentSettings, AgentTuning, AgentEvent, LlmMessage, TurnTrace } from "./agent-types";
import type { LogRecord, OosError } from "./store/log-types";
import { appendLog } from "./store/log-store";
import { setClaims, clearClaims } from "./store/auth";
import { openAppError } from "./components/AppErrorDrawer";

export type { AgentEvent };

/** Decoded auth status returned by the native get_auth_status / start_login
 *  commands. The raw token rides along so the webview can decode display
 *  claims (store/auth), while the keychain holds it at rest. */
interface AuthStatusNative {
	authenticated: boolean;
	token:    string;
	email:    string;
	username: string;
	role:     string;
	groups:   string[];
}

// \u2500\u2500\u2500 Wire types kept local to the seam \u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500

/** Compact summary used by the chat-list drawer. */
export interface ChatSummary {
	id:        string;
	title:     string;
	updatedAt: string;
}

/** One filter clause for oos.cmd.data.query. */
export interface DataWhere {
	field: string;
	op:    string;
	value: string;
}

/** A single {value,label} option for a meta-backed dropdown. */
export interface DataOption {
	value: string;
	label: string;
}

/** Result of oos.cmd.data.query: the rows plus, when withOptions was
 *  set, every meta dropdown list keyed by its short name. */
export interface DataQueryResult {
	rows:     Record<string, unknown>[];
	options?: Record<string, DataOption[]>;
	error?:   string;
}

/** One event-RAG hit returned by eventTurn. */
export interface EventHit {
	mappingName: string;
	sourceId:    string;
	streamId:    string;
	eventType:   string;
	textContent: string;
	metadata:    Record<string, unknown>;
	score:       number;
}

/** Persisted settings shape. Mirrors the old settings-store AppSettings. */
export interface AppSettings {
	[key: string]: string;
	llmBaseUrl:      string;
	llmApiKey:       string;
	llmModel:        string;
	natsUrl:         string;
	authIssuerUrl:   string;
	authClientId:    string;
	authRedirectUri: string;
	s3AccessKey:     string;
	s3SecretKey:     string;
}

export const DEFAULT_APP_SETTINGS: AppSettings = {
	llmBaseUrl:      "http://localhost:11434",
	llmApiKey:       "",
	llmModel:        "",
	natsUrl:         "nats://localhost:4222",
	authIssuerUrl:   "",
	authClientId:    "oos-desktop",
	authRedirectUri: "http://localhost:5557/callback",
	s3AccessKey:     "",
	s3SecretKey:     "",
};

/**
 * Step trace as published by the pipeline runner. New fields are optional so
 * older messages still type-check; the persister fills sensible defaults.
 */
export interface PipelineRunStepTrace {
	name:        string;
	kind?:       string;
	summary?:    string;
	output?:     string;
	chunks?:     string[];
	durationMs?: number;
	usage?:      { promptTokens: number; completionTokens: number; totalTokens: number };
}

/** Per-step progress event pushed while a pipeline runs. */
export interface PipelineRunProgressEvent {
	runId:      string;
	stepIndex:  number;
	stepName:   string;
	stepKind:   string;
	rowIndex:   number;
	totalRows:  number;
	chunk:      string;
	usage:      { promptTokens: number; completionTokens: number; totalTokens: number };
	elapsedMs:  number;
	totalSteps?: number;
}

/** Final result pushed when a background pipeline run completes. */
export interface PipelineRunResultPayload {
	runId:  string;
	ok:     boolean;
	output: string;
	steps:  PipelineRunStepTrace[];
	error?: string;
	usage?: { promptTokens: number; completionTokens: number; totalTokens: number };
}

// \u2500\u2500\u2500 Helpers \u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500

/** Wrap any NATS result as the opaque { json } envelope the panels parse. */
async function asJson(subject: string, payload: unknown = {}): Promise<{ json: string; error?: string }> {
	try {
		const res = await natsRequest<unknown>(subject, payload);
		return { json: JSON.stringify(res) };
	} catch (err) {
		return { json: "", error: String(err) };
	}
}

/** A minimal finished-trace for the not-yet-migrated turn stubs. */
function emptyTrace(status: TurnTrace["status"] = "error", errorMessage?: string): TurnTrace {
	const now = new Date().toISOString();
	return {
		startedAt:  now,
		finishedAt: now,
		durationMs: 0,
		usage:      { promptTokens: 0, completionTokens: 0, totalTokens: 0 },
		toolCalls:  [],
		status,
		...(errorMessage !== undefined ? { errorMessage } : {}),
	};
}

/** NATS request timeout for a turn: the LLM call's own ceiling plus a
 *  margin for transport and agent overhead. */
function turnTimeout(tuning?: AgentTuning): number {
	return (tuning?.timeoutMs ?? 300_000) + 15_000;
}

const NOT_MIGRATED = "not yet migrated";

/** Lazily-opened Tauri plugin-store backing app settings. */
let settingsStore: Awaited<ReturnType<typeof loadTauriStore>> | null = null;
async function getSettingsStore() {
	if (!settingsStore) settingsStore = await loadTauriStore("settings.json");
	return settingsStore;
}

async function readSettings(): Promise<AppSettings> {
	try {
		const store = await getSettingsStore();
		const saved = (await store.get<Partial<AppSettings>>("settings")) ?? {};
		return { ...DEFAULT_APP_SETTINGS, ...saved } as AppSettings;
	} catch {
		return { ...DEFAULT_APP_SETTINGS };
	}
}

/**
 * Resolves the IdP coordinates for the PKCE flow. Explicit settings win
 * (an operator pointing at an external IdP such as Keycloak); otherwise
 * we fall back to the oos-iam KV bucket the built-in oosiam publishes, so
 * a fresh install needs no auth settings entered by hand. Returns null
 * when neither source has an issuer — the caller treats that as
 * "not configured".
 */
async function resolveIamConfig(): Promise<{ issuer: string; clientId: string; redirectUri: string } | null> {
	const s = await readSettings();
	if (s.authIssuerUrl) {
		return {
			issuer:      s.authIssuerUrl,
			clientId:    s.authClientId || "oos-desktop",
			redirectUri: s.authRedirectUri || "http://localhost:5557/callback",
		};
	}
	try {
		const nc = await getNats();
		const kv = await nc.jetstream().views.kv("oos-iam");
		const e = await kv.get("user");
		if (e && e.operation !== "DEL" && e.operation !== "PURGE") {
			const cfg = jsonCodec().decode(e.value) as { issuerUrl?: string; clientId?: string; redirectUri?: string };
			if (cfg.issuerUrl) {
				return {
					issuer:      cfg.issuerUrl,
					clientId:    cfg.clientId || "oos-desktop",
					redirectUri: cfg.redirectUri || "http://localhost:5557/callback",
				};
			}
		}
	} catch { /* bus offline or no bucket — not configured */ }
	return null;
}

// \u2500\u2500\u2500 The seam object \u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500

export const rpc = {
	// (b) native \u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500
	async listModels(p: { baseUrl: string; apiKey: string }): Promise<{ models: string[]; error?: string }> {
		try { return await invoke<{ models: string[] }>("list_models", { baseUrl: p.baseUrl, apiKey: p.apiKey }); }
		catch (err) { return { models: [], error: String(err) }; }
	},

	async getInitialState(_?: unknown): Promise<{ settings: AppSettings; iamConfigured: boolean; hasToken: boolean; accessToken?: string; nodeId: string }> {
		const settings = await readSettings();
		let nodeId = "";
		try { nodeId = await invoke<string>("node_id"); } catch { /* command lands with the shell */ }
		const cfg = await resolveIamConfig();
		let hasToken = false;
		let accessToken: string | undefined;
		try {
			const st = await invoke<AuthStatusNative>("get_auth_status");
			hasToken = st.authenticated;
			if (st.authenticated) accessToken = st.token;
		} catch { /* command lands with the shell */ }
		return { settings, iamConfigured: cfg !== null, hasToken, accessToken, nodeId };
	},

	async loadSettings(_?: unknown): Promise<AppSettings> {
		return readSettings();
	},

	async saveSettings(next: AppSettings): Promise<{ ok: boolean; error?: string }> {
		try {
			const store = await getSettingsStore();
			await store.set("settings", next);
			await store.save();
			return { ok: true };
		} catch (err) { return { ok: false, error: String(err) }; }
	},

	async pingNats(_p: { url: string }): Promise<{ ok: boolean; error?: string }> {
		try { await pingBus(); return { ok: true }; }
		catch (err) { return { ok: false, error: String(err) }; }
	},

	async openExternalUrl(p: { url: string }): Promise<{ ok: boolean }> {
		try { await invoke("open_external_url", { url: p.url }); return { ok: true }; }
		catch { return { ok: false }; }
	},

	async clipboardRead(_?: unknown): Promise<{ text: string | null }> {
		try { return { text: await navigator.clipboard.readText() }; }
		catch { return { text: null }; }
	},

	async clipboardWrite(p: { text: string }): Promise<{ ok: boolean }> {
		try { await navigator.clipboard.writeText(p.text); return { ok: true }; }
		catch { return { ok: false }; }
	},

	// (b) auth \u2014 native OAuth2 + PKCE. The host exchanges the code and
	// stores the id_token in the OS keychain; the token also rides back so
	// the webview can decode display claims (store/auth), as the Bun model
	// did. IdP coordinates come from settings or the oos-iam KV bucket.
	async getAuthStatus(_?: unknown): Promise<{ iamConfigured: boolean; hasToken: boolean }> {
		const cfg = await resolveIamConfig();
		let hasToken = false;
		try {
			const st = await invoke<AuthStatusNative>("get_auth_status");
			hasToken = st.authenticated;
			if (st.authenticated) setClaims(st.token); else clearClaims();
		} catch { /* command lands with the shell */ }
		return { iamConfigured: cfg !== null, hasToken };
	},
	async startLogin(_?: unknown): Promise<{ ok: boolean; url?: string; error?: string }> {
		const cfg = await resolveIamConfig();
		if (!cfg) return { ok: false, error: "IAM not configured" };
		try {
			// Blocks until the browser login + token exchange complete (or time
			// out): the host opens the system browser and runs the loopback
			// callback. On success the keychain holds the token and we get the
			// decoded claims (+ token) back, so we set claims and fire the
			// completion handler the LoginScreen registered.
			const st = await invoke<AuthStatusNative>("start_login", { issuer: cfg.issuer, clientId: cfg.clientId, redirectUri: cfg.redirectUri });
			if (!st.authenticated) return { ok: false, error: "login did not complete" };
			setClaims(st.token);
			authCompletedHandler?.();
			return { ok: true };
		} catch (err) { return { ok: false, error: String(err) }; }
	},
	async logout(_?: unknown): Promise<{ ok: boolean }> {
		try { await invoke("logout"); } catch { /* best-effort; clear local claims regardless */ }
		clearClaims();
		return { ok: true };
	},
	async getMyPermissions(_?: unknown): Promise<{ role: string; username: string; permissions: Array<{ domain: string; actions: string[] }>; groups: string[]; error?: string }> {
		let role = "";
		let username = "";
		let groups: string[] = [];
		try {
			const st = await invoke<AuthStatusNative>("get_auth_status");
			if (st.authenticated) { role = st.role; username = st.username; groups = st.groups; }
		} catch { /* not signed in */ }
		// No role (signed out, or groups resolve to none) -> empty permissions,
		// no bus call: oosgql requires a role and would answer "role required".
		if (!role) return { role, username, permissions: [], groups };
		try {
			const res = await natsRequest<{ permissions?: Array<{ domain: string; actions: string[] }> }>("oos.cmd.gql.permissions", { role });
			return { role, username, permissions: res.permissions ?? [], groups };
		} catch (err) { return { role, username, permissions: [], groups, error: String(err) }; }
	},

	// (a) NATS passthrough \u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500
	// The resolver wants the rich index (aliases, titles, fields,
	// default), not the lean {ids} of domain.list/view.list — keyword
	// matching and view-hints both need it. oos.cmd.domains/views are
	// the catalogue subjects served by oosai's command_handler.
	getDomains:       (_?: unknown)                          => asJson("oos.cmd.domains", {}),
	getViews:         (_?: unknown)                          => asJson("oos.cmd.views", {}),
	// oosgql-rs gql.view/gql.domain key the source row on `name`, not `id`
	// (the row's primary key column happens to be `id`, but the request
	// field is `name`); sending `id` fails deserialization with 400.
	getView:          (p: { name: string })       => asJson("oos.cmd.gql.view",   { name: p.name }),
	getDomain:        (p: { name: string })       => asJson("oos.cmd.gql.domain", { name: p.name }),
	getEventMappings: (_?: unknown)                          => asJson("oos.cmd.event_mappings.list", {}),

	// data layer: structured query/mutate over oosgql-rs. dataQuery's
	// withOptions folds a record fetch and all its dropdown option lists
	// into one round-trip; dataMutate covers insert/update/delete. These
	// replace the GraphQL-string surface for the detail form. runQuery/
	// runMutation stay stubbed for the agent's GraphQL-emitting tools
	// (ViewRenderer refresh) until that loop is ported.
	async dataQuery(p: { domain: string; fields?: string[]; where?: DataWhere[]; order?: string; limit?: number; withOptions?: boolean }): Promise<DataQueryResult> {
		try {
			const res = await natsRequest<{ rows?: Record<string, unknown>[]; options?: Record<string, DataOption[]>; error?: string }>("oos.cmd.data.query", p);
			if (res.error) return { rows: [], error: res.error };
			return { rows: res.rows ?? [], options: res.options };
		} catch (err) { return { rows: [], error: String(err) }; }
	},
	async dataMutate(p: { domain: string; op: "insert" | "update" | "delete"; role: string; set?: Record<string, string>; id?: string }): Promise<{ record?: Record<string, unknown>; error?: string }> {
		try {
			const res = await natsRequest<{ record?: Record<string, unknown>; error?: string }>("oos.cmd.data.mutate", p);
			if (res.error) return { error: res.error };
			return { record: res.record };
		} catch (err) { return { error: String(err) }; }
	},
	async runQuery(_p: { query: string }): Promise<{ json: string; error?: string }> {
		return { json: "", error: `gql.query superseded by data.*; ${NOT_MIGRATED}` };
	},
	async runMutation(_p: { query: string; role: string }): Promise<{ json: string; error?: string }> {
		return { json: "", error: `gql.mutation superseded by data.*; ${NOT_MIGRATED}` };
	},

	// (a) JetStream KV (read one key directly from the bus).
	async natsKvGet(p: { bucket: string; key: string }): Promise<{ value: unknown | null; error?: string }> {
		try {
			const nc = await getNats();
			const kv = await nc.jetstream().views.kv(p.bucket);
			const e = await kv.get(p.key);
			if (!e || e.operation === "DEL" || e.operation === "PURGE") return { value: null };
			return { value: jsonCodec().decode(e.value) };
		} catch (err) { return { value: null, error: String(err) }; }
	},

	// (d) event subsystem \u2014 blocked on the oosai event endpoints (same block
	// as oosd's police/support seeds). Stubs keep the panels compiling.
	listAllStreams:    (_?: unknown)       => asJsonStub(),
	getEventStreams:   (_p: { mapping: string; limit?: number })                 => asJsonStub(),
	getEventSchemas:   (_p: { mapping: string; stream?: string })                => asJsonStub(),
	getEventTags:      (_p: { mapping: string })                                 => asJsonStub(),
	getStreamTag:      (_p: { stream: string })                                  => Promise.resolve({ tag: null as string | null, error: NOT_MIGRATED }),
	getStreamEvents:   (_p: { mapping: string; streamId: string; limit?: number }) => asJsonStub(),
	getEventSchema:    (_p: { mapping: string; eventType: string })              => asJsonStub(),
	async createEventStream(_p: { stream: string; description: string; eventMappingId: number | null; tag?: string }): Promise<{ ok: boolean; error?: string }> { return { ok: false, error: `events ${NOT_MIGRATED}` }; },
	async deleteEventStream(_p: { stream: string }): Promise<{ ok: boolean; error?: string }> { return { ok: false, error: `events ${NOT_MIGRATED}` }; },
	async insertEvent(_p: { mapping: string; stream: string; eventType: string; text: string; payload: Record<string, unknown> }): Promise<{ ok: boolean; id?: number; closed?: boolean; error?: string }> { return { ok: false, error: `events ${NOT_MIGRATED}` }; },
	async updateEvent(_p: { mapping: string; id: number; text: string; payload: Record<string, unknown> }): Promise<{ ok: boolean; error?: string }> { return { ok: false, error: `events ${NOT_MIGRATED}` }; },
	async deleteEvent(_p: { mapping: string; id: number }): Promise<{ ok: boolean; error?: string }> { return { ok: false, error: `events ${NOT_MIGRATED}` }; },
	async closeEvent(_p: { mapping: string; id: number }): Promise<{ ok: boolean; closed_at?: string; error?: string }> { return { ok: false, error: `events ${NOT_MIGRATED}` }; },

	// (d) headless turn engine + pipeline runner \u2014 Rust slice pending.
	// ask/translate are plain-LLM turns served by the headless oosagent
	// (oos.cmd.turn.ask/translate). The LLM call can run for minutes, so
	// we override natsRequest's short default timeout with the tuning's
	// own ceiling plus a transport margin. The reply already carries the
	// {text, error?, trace} shape the panels expect.
	async askTurn(p: { turnId: string; settings: AgentSettings; tuning?: AgentTuning; history: LlmMessage[]; user: string }): Promise<{ text: string; error?: string; trace: TurnTrace }> {
		try {
			return await natsRequest<{ text: string; error?: string; trace: TurnTrace }>("oos.cmd.turn.ask", p, turnTimeout(p.tuning));
		} catch (err) { return { text: "", error: String(err), trace: emptyTrace("error", String(err)) }; }
	},
	async translateTurn(p: { turnId: string; settings: AgentSettings; tuning?: AgentTuning; source: string; sourceLang: string; targetLang: string }): Promise<{ text: string; error?: string; trace: TurnTrace }> {
		try {
			return await natsRequest<{ text: string; error?: string; trace: TurnTrace }>("oos.cmd.turn.translate", p, turnTimeout(p.tuning));
		} catch (err) { return { text: "", error: String(err), trace: emptyTrace("error", String(err)) }; }
	},
	async chatTurn(p: { turnId: string; settings: AgentSettings; tuning?: AgentTuning; history: LlmMessage[]; user: string; viewHint?: string; userRole?: string; username?: string }): Promise<{ text: string; steps: number; hitLimit: boolean; error?: string; trace: TurnTrace }> {
		// The headless oosagent runs the whole ReAct loop before replying,
		// streaming progress over oos.agent.event.<turnId> meanwhile (wireBusPush
		// fans those into subscribeAgentEvent). The reply carries the final
		// {text, error?, trace}; steps/hitLimit are derived from the trace the
		// loop assembled. Same long timeout as ask/translate — it must cover the
		// whole loop, not a single LLM call.
		try {
			const res = await natsRequest<{ text: string; error?: string; trace: TurnTrace }>("oos.cmd.turn.chat", p, turnTimeout(p.tuning));
			return {
				text:     res.text,
				steps:    res.trace.toolCalls.length,
				hitLimit: res.trace.status === "step_limit",
				error:    res.error,
				trace:    res.trace,
			};
		} catch (err) {
			return { text: "", steps: 0, hitLimit: false, error: String(err), trace: emptyTrace("error", String(err)) };
		}
	},
	async eventTurn(_p: { turnId: string; settings: AgentSettings; tuning?: AgentTuning; mapping: string; streamId: string; question: string }): Promise<{ text: string; hits: EventHit[]; model: string; error?: string }> {
		return { text: "", hits: [], model: "", error: `event RAG ${NOT_MIGRATED}` };
	},
	async cancelTurn(p: { turnId: string }): Promise<{ ok: boolean }> {
		// Trips the cancel flag in the headless agent; the loop checks it
		// between steps, emits `cancelled`, and unwinds. Fast round-trip, so
		// the default request timeout is fine.
		try {
			const res = await natsRequest<{ ok: boolean; cancelled?: boolean }>("oos.cmd.turn.cancel", p);
			return { ok: res.ok };
		} catch { return { ok: false }; }
	},

	// (c) pipelines: CRUD is real (oosgql/oosai data path); RUN is deferred.
	listPipelines: (_?: unknown) => asJsonPipelines(),
	async savePipeline(p: { name: string; source: string }): Promise<{ ok: boolean; error?: string }> {
		try { await natsRequest("oos.cmd.pipeline.save", p); return { ok: true }; }
		catch (err) { return { ok: false, error: String(err) }; }
	},
	async deletePipeline(p: { name: string }): Promise<{ ok: boolean; error?: string }> {
		try { await natsRequest("oos.cmd.pipeline.delete", p); return { ok: true }; }
		catch (err) { return { ok: false, error: String(err) }; }
	},
	async runPipeline(_p: { name: string }): Promise<{ accepted: boolean; runId: string; error?: string }> {
		return { accepted: false, runId: "", error: `pipeline runner ${NOT_MIGRATED}` };
	},

	// (c) local persistence \u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500
	async saveChat(chat: ChatRow): Promise<{ ok: boolean; error?: string }> {
		try { await db.chats.put(chat); return { ok: true }; }
		catch (err) { return { ok: false, error: String(err) }; }
	},
	async loadChat(p: { id: string }): Promise<{ chat: ChatRow | null; error?: string }> {
		try { return { chat: (await db.chats.get(p.id)) ?? null }; }
		catch (err) { return { chat: null, error: String(err) }; }
	},
	async deleteChat(p: { id: string }): Promise<{ ok: boolean; error?: string }> {
		try { await db.chats.delete(p.id); return { ok: true }; }
		catch (err) { return { ok: false, error: String(err) }; }
	},
	async listChats(_?: unknown): Promise<{ chats: ChatSummary[]; error?: string }> {
		try {
			const rows = await db.chats.orderBy("updatedAt").reverse().toArray();
			return { chats: rows.map((r) => ({ id: r.id, title: r.title, updatedAt: r.updatedAt })) };
		} catch (err) { return { chats: [], error: String(err) }; }
	},
	async saveTurn(turn: TurnRow): Promise<{ ok: boolean; error?: string }> {
		try { await db.turns.put(turn); return { ok: true }; }
		catch (err) { return { ok: false, error: String(err) }; }
	},
	async loadTurn(p: { id: string }): Promise<{ turn: TurnRow | null; error?: string }> {
		try { return { turn: (await db.turns.get(p.id)) ?? null }; }
		catch (err) { return { turn: null, error: String(err) }; }
	},
	async listTurns(_?: unknown): Promise<{ turns: TurnRow[]; error?: string }> {
		try { return { turns: await db.turns.orderBy("createdAt").reverse().toArray() }; }
		catch (err) { return { turns: [], error: String(err) }; }
	},
	async loadPipelineSteps(p: { turnId: string }): Promise<{ steps: PipelineStepOutputRow[]; error?: string }> {
		try { return { steps: await db.pipelineSteps.where("turnId").equals(p.turnId).sortBy("stepIndex") }; }
		catch (err) { return { steps: [], error: String(err) }; }
	},
	async kvGet(p: { key: string }): Promise<{ value: unknown; error?: string }> {
		try { const row = await db.settings.get(p.key); return { value: row?.value ?? null }; }
		catch (err) { return { value: null, error: String(err) }; }
	},
	async kvSet(p: { key: string; value: unknown }): Promise<{ ok: boolean; error?: string }> {
		try { await db.settings.put({ key: p.key, value: p.value }); return { ok: true }; }
		catch (err) { return { ok: false, error: String(err) }; }
	},

	// (e) logs: getLogs/clearLogs delegate to the Dexie-backed log-store.
	async getLogs(p: { limit?: number }): Promise<{ logs: LogRecord[]; error?: string }> {
		const { getRecentLogs } = await import("./store/log-store");
		return { logs: await getRecentLogs(p?.limit ?? 200) };
	},
	async clearLogs(_?: unknown): Promise<{ ok: boolean; error?: string }> {
		const { clearLogs } = await import("./store/log-store");
		await clearLogs();
		return { ok: true };
	},
};

// Pipeline CRUD list returns the { json } envelope the panel parses.
async function asJsonPipelines(): Promise<{ pipelines: Array<{ name: string; source: string }>; error?: string }> {
	try {
		const res = await natsRequest<{ pipelines?: Array<{ name: string; source: string }> }>("oos.cmd.pipeline.list", {});
		return { pipelines: res.pipelines ?? [] };
	} catch (err) { return { pipelines: [], error: String(err) }; }
}

/** Shared not-migrated envelope for the event-subsystem stubs. */
function asJsonStub(): Promise<{ json: string; error?: string }> {
	return Promise.resolve({ json: "", error: `events ${NOT_MIGRATED}` });
}

// \u2500\u2500\u2500 Push (bus -> webview) \u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500

type AgentEventListener = (event: AgentEvent) => void;
const agentListeners = new Set<AgentEventListener>();
export function subscribeAgentEvent(fn: AgentEventListener): () => void {
	agentListeners.add(fn);
	return () => { agentListeners.delete(fn); };
}

type PipelineRunResultListener = (result: PipelineRunResultPayload) => void;
const pipelineRunResultListeners = new Set<PipelineRunResultListener>();
export function subscribePipelineRunResult(fn: PipelineRunResultListener): () => void {
	pipelineRunResultListeners.add(fn);
	return () => { pipelineRunResultListeners.delete(fn); };
}

type PipelineRunProgressListener = (event: PipelineRunProgressEvent) => void;
const pipelineRunProgressListeners = new Set<PipelineRunProgressListener>();
export function subscribePipelineRunProgress(fn: PipelineRunProgressListener): () => void {
	pipelineRunProgressListeners.add(fn);
	return () => { pipelineRunProgressListeners.delete(fn); };
}

type AuthCompletedHandler = () => void;
let authCompletedHandler: AuthCompletedHandler | null = null;
export function setAuthCompletedHandler(fn: AuthCompletedHandler): void {
	authCompletedHandler = fn;
}

/** Mark these intentionally-unused-for-now dispatch paths so tsc stays quiet. */
void pipelineRunResultListeners;
void pipelineRunProgressListeners;
void openAppError;

/**
 * wireBusPush subscribes to the fan-in subjects the seam owns. Today that is
 * the "*.log" stream feeding the Logs tab; the agent / pipeline streams light
 * up once the headless agent publishes them (the subscribe* registries above
 * are already wired for that). Best-effort: a missing bus must not break boot.
 */
async function wireBusPush(): Promise<void> {
	try {
		const nc = await getNats();
		const sc = jsonCodec();

		const logs = nc.subscribe("*.log");
		(async () => {
			for await (const m of logs) {
				try { void appendLog(sc.decode(m.data) as LogRecord); } catch { /* skip malformed */ }
			}
		})();

		// Agent turn events: the headless oosagent publishes one subject per
		// turn (oos.agent.event.<turnId>). A single wildcard sub fans every
		// turn's stream into the registered listeners (useChat's onAgentEvent).
		const agent = nc.subscribe("oos.agent.event.>");
		(async () => {
			for await (const m of agent) {
				try {
					const event = sc.decode(m.data) as AgentEvent;
					for (const fn of agentListeners) fn(event);
				} catch { /* skip malformed */ }
			}
		})();
	} catch { /* bus offline at boot; reconnect handles the rest */ }
}
void wireBusPush();

// Keep imported error type referenced for future bus error wiring.
export type { OosError };
