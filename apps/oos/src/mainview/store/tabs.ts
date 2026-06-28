// store/tabs.ts — Tab state for the right-hand workspace.
//
// Tabs are organised into groups. Each group has a kind (welcome,
// docs, results, settings, activity) and tabs that share a
// lifecycle. The displacement rules live on the group:
//
//   - displaceOnNewActivity: a new oos_query result sweeps this
//     group away. Welcome and Docs use this so the workspace shifts
//     to results once the user starts asking questions.
//   - displaceOnDocsOpen: opening the Documentation menu entry
//     sweeps this group away. Used by the Welcome group so the
//     "explain things" menu doesn't stack on the welcome tab.
//
// The Settings group has both flags off, so Settings panels stay
// open in parallel to results and docs — the user can tweak
// finetuning while reading a result.
//
// dsl_detail tabs are placed in the same `results` group as
// graphql_result tabs: a detail edit is a downstream activity of a
// list view, conceptually part of the same "what the user is
// currently working on" space.
//
// The store exposes a small mutator API rather than a generic
// `setState`. Each mutator encodes one user-meaningful action
// (`addResult`, `closeTab`, `openSettings`, ...) so the consuming
// components stay declarative — they say *what* changed, the store
// figures out the displacement implications. Like other stores it
// subscribes to a process-wide bus, so a save by any caller fans
// out to every consumer in the same React tick.

import { useEffect, useState } from "react";

import { docsContent } from "../docs/docs-content.generated";

// ─── Types ───────────────────────────────────────────────────────────

/** Discriminator for the renderer the active tab content uses. */
export type TabKind =
	| "welcome"
	| "doc"                  // a documentation topic; tab.payload.topicId tells which one
	| "graphql_result"       // live result from oos_query — payload carries the data
	| "event_result"         // events-mode RAG answer + source hits
	| "dsl_detail"           // single-record detail/edit form driven by a .view DSL
	| "settings_connection"  // LLM endpoint, key, model, backend URLs
	| "settings_finetuning"  // per-model temperature, timeout, max tokens, top hits
	| "settings_logs"        // local log ring-buffer viewer
	| "settings_permissions" // current user role + domain permission matrix
	| "chat_history"         // list of saved chats; click loads, drag fills composer
	| "activity_list"        // table of every persisted turn for inspection
	| "activity_detail"      // a single turn inspected — tool calls, args, results
	| "person_list"          // mock — kept until every demo path goes through the loop
	| "person_detail"        // mock — same
	| "note_list"            // mock — same
	| "stream_manager"       // admin: create / delete event streams
	| "new_event"            // insert a new event into a source table
	| "translate"            // side-by-side Markdown translator (two MDXEditor panes)
	| "stream_detail"        // event entry form for one stream
	| "pipeline_list"        // browse and launch saved pipelines
	| "pipeline_run"         // running or completed pipeline result
	| "dev";                 // Dev agent tab — agentic loop with bench/oosmem tools

/** Discriminator for the group the tab lives in. */
export type GroupKind = "welcome" | "docs" | "results" | "settings" | "history" | "activity" | "dev";

/** Per-tab payload. Discriminated by `kind` to keep dispatch simple. */
export type TabPayload =
	| { kind: "welcome" }
	| { kind: "doc"; topicId: string }
	| {
			kind:        "graphql_result";
			contextName: string;
			query:       string;
			data:        unknown;
			/**
			 * Optional view name to render the result through. Set
			 * by the agent when the pre-LLM resolver matched a
			 * view; the renderer fetches the .view source from
			 * oosgql and drives an OnisinView with it. Absent
			 * results fall back to the generic ResultTable.
			 */
			viewName?:   string;
	  }
	| {
			/**
			 * Ask-mode answer routed to a tab instead of a chat bubble.
			 * Opened when the user toggles "Editor" in the Footer before
			 * sending. Carries the original question for context and the
			 * Markdown answer, which the panel loads into an editable
			 * MDXEditor so the answer can be further refined, copied or
			 * saved as a document.
			 */
			kind:     "ask_result";
			/** Original question text. */
			question: string;
			/** Markdown answer the LLM produced. */
			answer:   string;
			/** Model that produced the answer. */
			model:    string;
	  }
	| {
			kind:     "event_result";
			/** Mapping name (e.g. "police", "support"). */
			mapping:  string;
			/** Stream filter the question was scoped to. */
			streamId: string;
			/** Original question text. */
			question: string;
			/** Markdown answer the LLM produced. */
			answer:   string;
			/** Raw hits the LLM saw — rendered as a source list. */
			hits:     Array<{
				mappingName: string;
				sourceId:    string;
				streamId:    string;
				eventType:   string;
				textContent: string;
				metadata:    Record<string, unknown>;
				score:       number;
			}>;
			/** Model name that produced the answer. */
			model:    string;
	  }
	| {
			kind:        "dsl_detail";
			/** Domain the detail view edits ("person", "note", ...). */
			contextName: string;
			/** View name in oos.view (e.g. "person_detail"). */
			viewName:    string;
			/**
			 * Seed data for the form. Either a flat row from the
			 * list view (then `id` is read off it) or an empty
			 * object for "new" — in which case `id` stays absent
			 * and Save will render an insert mutation.
			 */
			data:        Record<string, unknown>;
			/**
			 * Cached id, denormalised from `data.id` so tab-key and
			 * activate-existing logic don't have to re-look-up. May
			 * be undefined for "new" tabs.
			 */
			id?:         string | number;
	  }
	| { kind: "settings_connection" }
	| { kind: "settings_finetuning" }
	| { kind: "settings_logs" }
	| { kind: "settings_permissions" }
	| { kind: "chat_history" }
	| { kind: "activity_list" }
	| { kind: "activity_detail"; turnId: string }
	| { kind: "person_list" }
	| { kind: "person_detail" }
	| { kind: "note_list" }
	| { kind: "stream_manager" }
	| { kind: "new_event" }
	| { kind: "translate" }
	| { kind: "pipeline_list" }
	| {
			kind:         "pipeline_run";
			/** Pipeline name as stored in public.pipelines. */
			pipelineName: string;
			/** Current execution status. */
			status:       "idle" | "running" | "done" | "error";
			/** Accumulated output text from the pipeline steps. */
			output:       string;
			/** Error message if status === "error". */
			error?:       string;
			/**
			 * Persisted run id once oosai has accepted the run.
			 * Survives unmount/remount so the panel can resubscribe
			 * to the original run instead of kicking off a second one.
			 */
			runId?:       string;
	  }
	| {
			kind:        "stream_detail";
			/** Mapping name — e.g. "police". */
			mapping:     string;
			/** Source table — e.g. "police_incidents". */
			sourceTable: string;
			/** Stream id — e.g. "fall-2024-0042". */
			streamId:    string;
	  }
	| { kind: "dev" };

/** A single tab in the IDE-style vertical tab rail. */
export interface TabRecord {
	id:        string;
	groupId:   string;
	title:     string;
	subtitle?: string;
	payload:   TabPayload;
}

/** A logical group of related tabs that share a lifecycle. */
export interface TabGroup {
	id:    string;
	kind:  GroupKind;
	title: string;
	tabs:  TabRecord[];
	/** True when a new chat result should sweep this group away. */
	displaceOnNewActivity: boolean;
	/** True when opening the docs group should sweep this one away. */
	displaceOnDocsOpen: boolean;
}

/** Snapshot of everything the TabRail and TabContent need. */
export interface TabsSnapshot {
	groups:   TabGroup[];
	activeId: string | null;
}

// ─── Internal mutable state ──────────────────────────────────────────

let state: TabsSnapshot = computeInitial();

type Listener = (snap: TabsSnapshot) => void;
const listeners = new Set<Listener>();

function notify(): void {
	for (const fn of listeners) fn(state);
}

function setState(next: TabsSnapshot): void {
	state = ensureWelcome(next);
	notify();
}

// ─── Group factories ─────────────────────────────────────────────────

function welcomeGroup(): TabGroup {
	return {
		id:    "welcome",
		kind:  "welcome",
		title: "Welcome",
		tabs:  [
			{
				id:       "welcome",
				groupId:  "welcome",
				title:    "Welcome",
				subtitle: "Start",
				payload:  { kind: "welcome" },
			},
		],
		displaceOnNewActivity: true,
		displaceOnDocsOpen:    true,
	};
}

/**
 * docsGroup builds the documentation group from the bundled topic
 * list. Topic ids that have no Markdown body are dropped so a stale
 * TOPICS array can never spawn an empty tab.
 */
function docsGroup(): TabGroup {
	const topics = DOC_TOPICS.filter((t) => docsContent[t.id] !== undefined);
	return {
		id:    "docs",
		kind:  "docs",
		title: "Documentation",
		tabs: topics.map((t) => ({
			id:       `doc:${t.id}`,
			groupId:  "docs",
			title:    t.title,
			subtitle: "Doc",
			payload:  { kind: "doc", topicId: t.id },
		})),
		displaceOnNewActivity: true,
		displaceOnDocsOpen:    false,
	};
}

/** Static list of documentation topics, in the order they appear. */
const DOC_TOPICS: Array<{ id: string; title: string }> = [
	{ id: "index",        title: "Overview"           },
	{ id: "architecture", title: "Architecture"       },
	{ id: "agent-design", title: "Agent design"       },
	{ id: "settings",     title: "Settings"           },
	{ id: "finetuning",   title: "Finetuning"         },
	{ id: "shortcuts",    title: "Keyboard shortcuts" },
];

/**
 * settingsGroup builds the Settings group. Every settings panel
 * lives as a tab inside this single group so the user can switch
 * between Connection and Finetuning without losing the rail slot.
 *
 * displaceOnNewActivity=false and displaceOnDocsOpen=false are
 * deliberate: settings are a parallel workspace, not transient
 * guidance. They stay open while the user keeps chatting and
 * browsing docs.
 */
function settingsGroup(): TabGroup {
	return {
		id:    "settings",
		kind:  "settings",
		title: "Settings",
		tabs: [
			{
				id:       "settings:connection",
				groupId:  "settings",
				title:    "Connection",
				subtitle: "LLM · Backend",
				payload:  { kind: "settings_connection" },
			},
			{
				id:       "settings:finetuning",
				groupId:  "settings",
				title:    "Finetuning",
				subtitle: "Per model",
				payload:  { kind: "settings_finetuning" },
			},
			{
				id:       "settings:logs",
				groupId:  "settings",
				title:    "Logs",
				subtitle: "Local log viewer",
				payload:  { kind: "settings_logs" },
			},
			{
				id:       "settings:permissions",
				groupId:  "settings",
				title:    "Berechtigungen",
				subtitle: "Rolle & Zugriffsrechte",
				payload:  { kind: "settings_permissions" },
			},
		],
		displaceOnNewActivity: false,
		displaceOnDocsOpen:    false,
	};
}

/**
 * activityGroup builds the Activity group. The list tab is always
 * present; detail tabs are appended as the user clicks rows in the
 * list. Same parallel-workspace flags as Settings and History —
 * an admin inspecting telemetry should not lose the inspector when
 * a new chat result arrives.
 */
function activityGroup(): TabGroup {
	return {
		id:    "activity",
		kind:  "activity",
		title: "Activity",
		tabs: [
			{
				id:       "activity:list",
				groupId:  "activity",
				title:    "Activity",
				subtitle: "Telemetry",
				payload:  { kind: "activity_list" },
			},
		],
		displaceOnNewActivity: false,
		displaceOnDocsOpen:    false,
	};
}

/**
 * historyGroup builds the Chat-History group. A single tab listing
 * every saved conversation, persistent across navigation like
 * Settings. The user closes it explicitly when they no longer want
 * it open; reopening (menu / hotkey) just focuses the existing tab.
 *
 * displaceOnNewActivity / displaceOnDocsOpen are both false — the
 * history is a parallel workspace, not transient guidance.
 */
function historyGroup(): TabGroup {
	return {
		id:    "history",
		kind:  "history",
		title: "Chat-Verlauf",
		tabs: [
			{
				id:       "history:chats",
				groupId:  "history",
				title:    "Chat-Verlauf",
				subtitle: "Gespeicherte Chats",
				payload:  { kind: "chat_history" },
			},
		],
		displaceOnNewActivity: false,
		displaceOnDocsOpen:    false,
	};
}

// ─── Initial state ───────────────────────────────────────────────────

function computeInitial(): TabsSnapshot {
	const w = welcomeGroup();
	return { groups: [w], activeId: w.tabs[0]!.id };
}

/**
 * ensureWelcome guarantees that whenever no tabs are open, the
 * Welcome group materialises again. Centralised here so callers
 * never need to think about the empty case.
 */
function ensureWelcome(snap: TabsSnapshot): TabsSnapshot {
	const nonEmpty = snap.groups.filter((g) => g.tabs.length > 0);
	if (nonEmpty.length === 0) {
		const w = welcomeGroup();
		return { groups: [w], activeId: w.tabs[0]!.id };
	}
	if (nonEmpty.length === snap.groups.length) return snap;
	const stillActive = nonEmpty.some((g) =>
		g.tabs.some((t) => t.id === snap.activeId),
	);
	const fallback = nonEmpty[0]?.tabs[0]?.id ?? null;
	return {
		groups:   nonEmpty,
		activeId: stillActive ? snap.activeId : fallback,
	};
}

// ─── Public mutators ─────────────────────────────────────────────────

/**
 * showWelcome closes every other group and shows just the Welcome
 * tab. Used by the "Welcome" menu entry as an explicit "go home"
 * — this is the one place where settings get displaced too,
 * matching the menu entry's "reset the workspace" intent.
 */
export function showWelcome(): void {
	const w = welcomeGroup();
	setState({ groups: [w], activeId: w.tabs[0]!.id });
}

/**
 * openSettings opens (or focuses) the Settings group. Idempotent:
 * if the group is already up the call only switches the active
 * tab to Connection. Pass a kind to land on a specific panel —
 * the keyboard shortcut and menu entry use that to deep-link.
 */
export function openSettings(landOn?: "connection" | "finetuning" | "logs" | "permissions"): void {
	const targetTabId =
		landOn === "finetuning"   ? "settings:finetuning"   :
		landOn === "logs"         ? "settings:logs"          :
		landOn === "permissions"  ? "settings:permissions"   :
		                            "settings:connection";

	const existing = state.groups.find((g) => g.kind === "settings");
	if (existing) {
		setState({ ...state, activeId: targetTabId });
		return;
	}
	const group  = settingsGroup();
	const groups = [...state.groups.filter((g) => g.kind !== "welcome"), group];
	setState({ groups, activeId: targetTabId });
}

/**
 * openActivityList opens (or focuses) the Activity group's list
 * tab. Triggered from the burger menu. Idempotent — second call
 * activates the existing list tab without spawning duplicates.
 */
export function openActivityList(): void {
	const targetTabId = "activity:list";
	const existing = state.groups.find((g) => g.kind === "activity");
	if (existing) {
		setState({ ...state, activeId: targetTabId });
		return;
	}
	const group  = activityGroup();
	const groups = [...state.groups.filter((g) => g.kind !== "welcome"), group];
	setState({ groups, activeId: targetTabId });
}

/**
 * openActivityDetail opens (or focuses) a detail tab for one
 * persisted turn. Tab id encodes the turnId so a second click on
 * the same row activates the existing inspector instead of
 * stacking duplicates. The Activity group's list tab is implied:
 * if the group is not yet up, it materialises with both list and
 * detail; if it is up, the detail joins the existing tabs.
 */
export function openActivityDetail(turnId: string): void {
	const detailTabId = `activity:detail:${turnId}`;

	// Already open — just activate.
	for (const g of state.groups) {
		for (const t of g.tabs) {
			if (t.id === detailTabId) {
				setState({ ...state, activeId: detailTabId });
				return;
			}
		}
	}

	const detailTab: TabRecord = {
		id:       detailTabId,
		groupId:  "activity",
		title:    "Turn-Inspector",
		subtitle: shortTurnLabel(turnId),
		payload:  { kind: "activity_detail", turnId },
	};

	const existing = state.groups.find((g) => g.kind === "activity");
	if (existing) {
		const groups = state.groups.map((g) =>
			g.kind === "activity"
				? { ...g, tabs: [...g.tabs, detailTab] }
				: g,
		);
		setState({ groups, activeId: detailTabId });
		return;
	}

	const newGroup: TabGroup = {
		...activityGroup(),
		tabs: [...activityGroup().tabs, detailTab],
	};
	const groups = [
		...state.groups.filter((g) => g.kind !== "welcome"),
		newGroup,
	];
	setState({ groups, activeId: detailTabId });
}

/**
 * shortTurnLabel produces a tiny subtitle hint from a turn id of
 * the shape `turn_<ms-timestamp>`. Showing the bare id is too long
 * for the rail; the timestamp's last few digits keep tabs
 * distinguishable when several detail tabs are open at once.
 */
function shortTurnLabel(turnId: string): string {
	const m = turnId.match(/turn_(\d+)/);
	if (!m) return turnId.slice(0, 12);
	const ts = m[1]!;
	return `…${ts.slice(-6)}`;
}

/**
 * openStreamManager opens (or focuses) the Stream Manager tab.
 * Lives in the settings group — parallel workspace, never displaced
 * by new chat results or docs.
 */
export function openStreamManager(): void {
	const targetTabId = "settings:streams";
	// Already open — just activate.
	for (const g of state.groups) {
		for (const t of g.tabs) {
			if (t.id === targetTabId) {
				setState({ ...state, activeId: targetTabId });
				return;
			}
		}
	}
	const tab: TabRecord = {
		id:       targetTabId,
		groupId:  "settings",
		title:    "Streams",
		subtitle: "Event streams",
		payload:  { kind: "stream_manager" },
	};
	const existing = state.groups.find((g) => g.kind === "settings");
	if (existing) {
		const groups = state.groups.map((g) =>
			g.kind === "settings" ? { ...g, tabs: [...g.tabs, tab] } : g,
		);
		setState({ groups, activeId: targetTabId });
	} else {
		const group: TabGroup = {
			...settingsGroup(),
			tabs: [tab],
		};
		const groups = [...state.groups.filter((g) => g.kind !== "welcome"), group];
		setState({ groups, activeId: targetTabId });
	}
}

/**
 * openNewEvent opens (or focuses) the New Event tab.
 * Lives in the settings group — parallel workspace, never displaced
 * by new chat results or docs.
 */
/**
 * openStreamDetail opens (or focuses) a detail tab for one stream.
 * Tab id encodes mapping + streamId so a second right-click on the
 * same stream activates the existing tab instead of stacking duplicates.
 * Lives in the results group — parallel to any active chat results.
 */
export function openStreamDetail(args: {
	mapping:     string;
	sourceTable: string;
	streamId:    string;
}): void {
	const tabId = `stream:${args.mapping}:${args.streamId}`;

	// Already open — just activate.
	for (const g of state.groups) {
		for (const t of g.tabs) {
			if (t.id === tabId) {
				setState({ ...state, activeId: tabId });
				return;
			}
		}
	}

	const tab: TabRecord = {
		id:       tabId,
		groupId:  "results",
		title:    args.streamId,
		subtitle: args.mapping,
		payload:  {
			kind:        "stream_detail",
			mapping:     args.mapping,
			sourceTable: args.sourceTable,
			streamId:    args.streamId,
		},
	};

	const kept = state.groups.filter((g) => !g.displaceOnNewActivity);
	const existing = kept.find((g) => g.kind === "results");
	const groups: TabGroup[] = existing
		? kept.map((g) =>
				g.kind === "results" ? { ...g, tabs: [...g.tabs, tab] } : g,
			)
		: [
				...kept,
				{
					id:    "results",
					kind:  "results" as const,
					title: "Results",
					tabs:  [tab],
					displaceOnNewActivity: false,
					displaceOnDocsOpen:    true,
				},
			];
	setState({ groups, activeId: tabId });
}

export function openNewEvent(): void {
	const targetTabId = "settings:new-event";
	for (const g of state.groups) {
		for (const t of g.tabs) {
			if (t.id === targetTabId) {
				setState({ ...state, activeId: targetTabId });
				return;
			}
		}
	}
	const tab: TabRecord = {
		id:       targetTabId,
		groupId:  "settings",
		title:    "New Event",
		subtitle: "Insert event",
		payload:  { kind: "new_event" },
	};
	const existing = state.groups.find((g) => g.kind === "settings");
	if (existing) {
		const groups = state.groups.map((g) =>
			g.kind === "settings" ? { ...g, tabs: [...g.tabs, tab] } : g,
		);
		setState({ groups, activeId: targetTabId });
	} else {
		const group: TabGroup = {
			...settingsGroup(),
			tabs: [tab],
		};
		const groups = [...state.groups.filter((g) => g.kind !== "welcome"), group];
		setState({ groups, activeId: targetTabId });
	}
}

/**
 * openTranslate opens (or focuses) the Translate tab — a side-by-side
 * Markdown translator. Lives in the settings group, the parallel
 * workspace, so a translation scratchpad is never swept away by a new
 * chat result the way a Welcome or Docs tab would be.
 */
export function openTranslate(): void {
	const targetTabId = "settings:translate";
	for (const g of state.groups) {
		for (const t of g.tabs) {
			if (t.id === targetTabId) {
				setState({ ...state, activeId: targetTabId });
				return;
			}
		}
	}
	const tab: TabRecord = {
		id:       targetTabId,
		groupId:  "settings",
		title:    "Translate",
		subtitle: "Markdown",
		payload:  { kind: "translate" },
	};
	const existing = state.groups.find((g) => g.kind === "settings");
	if (existing) {
		const groups = state.groups.map((g) =>
			g.kind === "settings" ? { ...g, tabs: [...g.tabs, tab] } : g,
		);
		setState({ groups, activeId: targetTabId });
	} else {
		const group: TabGroup = {
			...settingsGroup(),
			tabs: [tab],
		};
		const groups = [...state.groups.filter((g) => g.kind !== "welcome"), group];
		setState({ groups, activeId: targetTabId });
	}
}

/**
 * openChatHistory opens (or focuses) the Chat-History group. Same
 * idempotency contract as openSettings: a second call just
 * activates the existing tab. Triggered from the burger-menu entry
 * and the Ctrl+H shortcut on the chat composer.
 */
export function openChatHistory(): void {
	const targetTabId = "history:chats";
	const existing = state.groups.find((g) => g.kind === "history");
	if (existing) {
		setState({ ...state, activeId: targetTabId });
		return;
	}
	const group  = historyGroup();
	const groups = [...state.groups.filter((g) => g.kind !== "welcome"), group];
	setState({ groups, activeId: targetTabId });
}

/**
 * openDocs opens the documentation group, displacing any existing
 * groups that have asked to be displaced when docs come up. Welcome
 * is one of those, so a fresh app state cleanly transitions from
 * Welcome into Docs.
 *
 * Idempotent: opening docs while docs are already open just
 * activates the first topic — no duplicate group is created.
 */
export function openDocs(): void {
	const docs = docsGroup();
	const kept = state.groups.filter(
		(g) => g.kind !== "welcome" && g.kind !== "docs" && !g.displaceOnDocsOpen,
	);
	const groups = [...kept, docs];
	const firstDocTab = docs.tabs[0]?.id ?? null;
	setState({ groups, activeId: firstDocTab });
}

/**
 * addResult appends a tab to the results group, creating that group
 * if it does not exist yet. Any group with displaceOnNewActivity is
 * removed first — Welcome and Docs are the typical candidates.
 *
 * The new tab is activated. The caller supplies the tab record; the
 * groupId is overwritten so a misconfigured caller cannot place a
 * result in the wrong group.
 */
export function addResult(tab: Omit<TabRecord, "groupId">): void {
	const kept = state.groups.filter((g) => !g.displaceOnNewActivity);
	const existing = kept.find((g) => g.kind === "results");
	const placed: TabRecord = { ...tab, groupId: "results" };

	const groups: TabGroup[] = existing
		? kept.map((g) =>
				g.kind === "results"
					? { ...g, tabs: [...g.tabs, placed] }
					: g,
			)
		: [
				...kept,
				{
					id:    "results",
					kind:  "results",
					title: "Results",
					tabs:  [placed],
					displaceOnNewActivity: false,
					displaceOnDocsOpen:    true,
				},
			];

	setState({ groups, activeId: placed.id });
}

/**
 * addGraphqlResult is a convenience wrapper around addResult for the
 * common case of opening a `graphql_result` tab from a finished
 * oos_query call. Title and subtitle are derived from the domain
 * name and the row count when the data is a single-key array, which
 * covers the vast majority of read queries.
 *
 * The optional `viewName` flows through to the tab payload — when
 * present, the tab's renderer will fetch the corresponding .view
 * source from oosgql and drive an OnisinView; otherwise it falls
 * back to the generic ResultTable.
 */
export function addGraphqlResult(args: {
	contextName: string;
	query:       string;
	data:        unknown;
	viewName?:   string;
}): void {
	const id = `result:${args.contextName}:${Date.now()}`;
	addResult({
		id,
		title:    titleFor(args.contextName),
		subtitle: subtitleFor(args.data),
		payload:  {
			kind:        "graphql_result",
			contextName: args.contextName,
			query:       args.query,
			data:        args.data,
			viewName:    args.viewName,
		},
	});
}

/**
 * addEventResult opens a new tab in the results group for a finished
 * events-mode turn. Title comes from the mapping ("Police"); the
 * subtitle shows the stream id so multiple results from the same
 * mapping but different cases stay distinguishable in the rail.
 *
 * Each call opens a fresh tab — events-mode answers are immutable
 * (the question and sources are baked into the payload), so the
 * "activate the existing one" trick used by addDslDetail does not
 * apply: every question deserves its own tab.
 */
export function addEventResult(args: {
	mapping:  string;
	streamId: string;
	question: string;
	answer:   string;
	hits:     Array<{
		mappingName: string;
		sourceId:    string;
		streamId:    string;
		eventType:   string;
		textContent: string;
		metadata:    Record<string, unknown>;
		score:       number;
	}>;
	model:    string;
}): void {
	const id = `event:${args.mapping}:${args.streamId || "all"}:${Date.now()}`;
	addResult({
		id,
		title:    titleFor(args.mapping),
		subtitle: args.streamId || "alle Streams",
		payload:  {
			kind:     "event_result",
			mapping:  args.mapping,
			streamId: args.streamId,
			question: args.question,
			answer:   args.answer,
			hits:     args.hits,
			model:    args.model,
		},
	});
}

/**
 * addAskResult opens a new tab in the results group for an Ask-mode
 * answer when the user has the Footer's "Editor" switch on. Each
 * call produces a fresh tab — multiple translations or rewrites
 * during a session deserve their own scratchpad. The title uses
 * a short slug of the question so several tabs stay
 * distinguishable in the rail without overflowing.
 */
export function addAskResult(args: {
	question: string;
	answer:   string;
	model:    string;
}): void {
	const id    = `ask:${Date.now()}`;
	const slug  = args.question.trim().split(/\s+/).slice(0, 6).join(" ");
	const title = slug.length > 40 ? slug.slice(0, 40) + "…" : slug || "Ask";
	addResult({
		id,
		title,
		subtitle: args.model,
		payload:  {
			kind:     "ask_result",
			question: args.question,
			answer:   args.answer,
			model:    args.model,
		},
	});
}

/**
 * addDslDetail opens (or focuses) a detail tab driven by a .view
 * DSL. Two flavours:
 *
 *   - With an `id`: this is an existing record. The tab id encodes
 *     view + id so a second click on the same row activates the
 *     existing tab instead of opening a duplicate.
 *   - Without an `id`: this is a "new" record (the toolbar's `new`
 *     button). Each invocation opens a fresh tab — multiple
 *     concurrent drafts are useful when comparing or copying.
 *
 * Title is derived from contextName + id ("Person #5") or
 * contextName + " (neu)" for fresh records. Subtitle stays small
 * — the detail tab is always a focused, single-record edit.
 */
export function addDslDetail(args: {
	contextName: string;
	viewName:    string;
	data:        Record<string, unknown>;
	id?:         string | number;
}): void {
	const idPart = args.id !== undefined && args.id !== "" && args.id !== 0
		? String(args.id)
		: `new-${Date.now()}`;
	const tabId = `detail:${args.viewName}:${idPart}`;

	// Activate the existing tab for the same id, if any.
	for (const g of state.groups) {
		for (const t of g.tabs) {
			if (t.id === tabId) {
				setState({ ...state, activeId: tabId });
				return;
			}
		}
	}

	const isNew = args.id === undefined || args.id === "" || args.id === 0;
	addResult({
		id:       tabId,
		title:    isNew
			? `${titleFor(args.contextName)} (neu)`
			: `${titleFor(args.contextName)} #${args.id}`,
		subtitle: isNew ? "Neu" : "Detail",
		payload:  {
			kind:        "dsl_detail",
			contextName: args.contextName,
			viewName:    args.viewName,
			data:        args.data,
			id:          args.id,
		},
	});
}

/** titleFor capitalises the domain name; "person" → "Person". */
function titleFor(contextName: string): string {
	if (!contextName) return "Result";
	return contextName.charAt(0).toUpperCase() + contextName.slice(1);
}

/** subtitleFor reads the row count off `{ <domain>: [...] }`-shaped data. */
function subtitleFor(data: unknown): string {
	if (!data || typeof data !== "object") return "Ergebnis";
	const entries = Object.entries(data as Record<string, unknown>);
	if (entries.length === 1) {
		const v = entries[0]![1];
		if (Array.isArray(v)) {
			return `${v.length} ${v.length === 1 ? "Eintrag" : "Einträge"}`;
		}
	}
	return "Ergebnis";
}

/**
 * openPipelineList opens (or focuses) the pipeline browser tab.
 * Lives in the results group — the user picks a pipeline to run
 * and each run opens a separate pipeline_run tab.
 */
export function openPipelineList(): void {
	const targetTabId = "pipeline:list";
	for (const g of state.groups) {
		for (const t of g.tabs) {
			if (t.id === targetTabId) {
				setState({ ...state, activeId: targetTabId });
				return;
			}
		}
	}
	const tab: TabRecord = {
		id:       targetTabId,
		groupId:  "results",
		title:    "Pipelines",
		subtitle: "Ausführen",
		payload:  { kind: "pipeline_list" },
	};
	addResult(tab);
}

/**
 * openPipelineRun opens a new tab for a pipeline execution.
 * Each call opens a fresh tab — runs are immutable results.
 */
export function openPipelineRun(pipelineName: string): void {
	const tabId = `pipeline:run:${pipelineName}:${Date.now()}`;
	addResult({
		id:       tabId,
		title:    pipelineName,
		subtitle: "Pipeline",
		payload:  {
			kind:         "pipeline_run",
			pipelineName,
			status:       "idle",
			output:       "",
		},
	});
}

/**
 * updatePipelineRunTab persists the run state back into the tab payload
 * so the content survives unmount/remount cycles. runId is captured
 * the moment oosai accepts the run; that way a remount during a
 * running pipeline can resubscribe to the existing run instead of
 * starting a second one.
 */
export function updatePipelineRunTab(
	tabId:  string,
	update: {
		status: "running" | "done" | "error";
		output?: string;
		error?:  string;
		runId?:  string;
	},
): void {
	const groups = state.groups.map((g) => ({
		...g,
		tabs: g.tabs.map((t) => {
			if (t.id !== tabId) return t;
			if (t.payload.kind !== "pipeline_run") return t;
			const p = t.payload;
			return {
				...t,
				payload: {
					...p,
					status: update.status,
					output: update.output ?? p.output,
					error:  update.error,
					runId:  update.runId ?? p.runId,
				},
			};
		}),
	}));
	setState({ ...state, groups });
}

/**
 * openDev opens (or focuses) the Dev agent tab.
 *
 * Lives in its own group — parallel workspace, never displaced by
 * chat results or docs. A single tab is enough; the agent loop
 * output is displayed inside DevPanel.
 */
export function openDev(): void {
	const targetTabId = "dev:agent";
	for (const g of state.groups) {
		for (const t of g.tabs) {
			if (t.id === targetTabId) {
				setState({ ...state, activeId: targetTabId });
				return;
			}
		}
	}
	const group: TabGroup = {
		id:    "dev",
		kind:  "dev",
		title: "Dev",
		tabs: [
			{
				id:       targetTabId,
				groupId:  "dev",
				title:    "Dev",
				subtitle: "Agent",
				payload:  { kind: "dev" },
			},
		],
		displaceOnNewActivity: false,
		displaceOnDocsOpen:    false,
	};
	const groups = [...state.groups.filter((g) => g.kind !== "welcome"), group];
	setState({ groups, activeId: targetTabId });
}

/**
 * setActive switches the active tab. Silently ignores ids that no
 * longer exist (e.g. a quick double-click during closing).
 */
export function setActive(id: string): void {
	for (const g of state.groups) {
		if (g.tabs.some((t) => t.id === id)) {
			setState({ ...state, activeId: id });
			return;
		}
	}
}

/**
 * closeTab removes a single tab. If that empties its group, the
 * group is removed too. The active tab falls forward to the
 * nearest survivor in tab order; if everything ends up empty,
 * ensureWelcome restores the Welcome group.
 */
export function closeTab(id: string): void {
	const groups = state.groups
		.map((g) => ({ ...g, tabs: g.tabs.filter((t) => t.id !== id) }))
		.filter((g) => g.tabs.length > 0);

	let nextActive: string | null = state.activeId;
	if (state.activeId === id) {
		nextActive =
			nearestSurvivor(state, id) ??
			groups[0]?.tabs[0]?.id ??
			null;
	}
	setState({ groups, activeId: nextActive });
}

function nearestSurvivor(snap: TabsSnapshot, removedId: string): string | null {
	const flat = snap.groups.flatMap((g) => g.tabs.map((t) => t.id));
	const idx = flat.indexOf(removedId);
	if (idx < 0) return null;
	const after  = flat.slice(idx + 1).find((tid) => tid !== removedId);
	if (after) return after;
	const before = flat.slice(0, idx).reverse().find((tid) => tid !== removedId);
	return before ?? null;
}

// ─── Hook ────────────────────────────────────────────────────────────

/**
 * useTabs returns the live snapshot. Re-renders the calling
 * component whenever any tab mutation runs through the bus.
 */
export function useTabs(): TabsSnapshot {
	const [snap, setSnap] = useState<TabsSnapshot>(state);
	useEffect(() => {
		const fn: Listener = (next) => setSnap(next);
		listeners.add(fn);
		setSnap(state); // catch up to anything that happened before mount
		return () => {
			listeners.delete(fn);
		};
	}, []);
	return snap;
}
