// ViewRenderer.tsx — DSL-driven renderer for a graphql_result tab.
//
// When the agent's tab_open event carries a `viewName`, the tab is
// supposed to render through the .view DSL instead of the generic
// ResultTable. This component owns the full pipeline:
//
//   1. Pull the .view source from oosgql via the bun RPC proxy
//      (the renderer can't fetch http://localhost:4000 directly —
//      same CORS rule as for /domains and /views; see Memory 82).
//   2. Parse the source in the shared diagnostics worker — Langium
//      pulls in a vscode-jsonrpc cancellation namespace that
//      Electrobun's main-thread bundler mishandles, so all parsing
//      stays inside the worker (Memory built around oosd's path).
//   3. Wrap the GraphQL payload into an envelope shaped the way
//      oos-ui-ts/loadEnvelope expects — list payloads become
//      `{ content: { <domain>: { rows: [...] } } }` to match the
//      `<domain>.rows` bind path the table widget reads from.
//   4. Hand the resulting ViewState + ViewDef to OnisinView, with
//      `onAction` and `onToolbar` wired to open detail tabs.
//
// Detail-tab navigation:
//
//   - on_select on a table row opens an existing-record detail tab
//     via `addDslDetail`. The action carries `target` (the detail
//     view name) and `navMods` — the `bind` modifier tells which
//     local field carries the id (usually `<domain>.id` for the
//     simple case).
//   - the toolbar's `new` item opens a "new record" detail tab
//     with empty data; the same `target` plus `navMods` shape is
//     parsed.
//
// On any failure (no view name, fetch error, parse error, payload
// shape unfit for the view) we silently fall back to ResultTable so
// the user always sees something useful. The fetch error gets
// logged for diagnosis but not surfaced — the fallback already
// conveys "we got data, just can't render it through the DSL".
//
// Above either rendering, a QueryInspector exposes the actual
// GraphQL string the LLM emitted. Collapsed by default, one click
// to read. Diagnosing wrong / surprising results without it is
// pure guesswork.
//
// Only one effect chain runs per (viewName, contextName) pair; if
// the user closes and re-opens the same tab, the source comes from
// a per-render fetch but the worker is shared with the rest of the
// app so the parser warms up exactly once.

import { useEffect, useRef, useState } from "react";
import { Box, Center, Loader, ScrollArea, Stack, Text } from "@mantine/core";
import type {
	NavModifierDef,
	ToolbarItemDef,
	ViewActionDef,
	ViewDef,
} from "oos-dsls-ts/types";
import { OnisinView, ViewState, loadEnvelope } from "oos-ui-ts";

import { parseViewInWorker } from "../../lang/worker/parse-client";
import { rpc } from "../rpc";
import { subscribe as subscribeDomainEvent } from "../domain-events";
import { addDslDetail } from "../store/tabs";
import { QueryInspector } from "./QueryInspector";
import { ResultTable } from "./ResultTable";

interface ViewRendererProps {
	contextName: string;
	query:       string;
	data:        unknown;
	viewName:    string | null | undefined;
}

/** Internal stage the renderer walks through. */
type Stage =
	| { kind: "fallback" }                            // no viewName, or definitive failure
	| { kind: "loading" }                             // fetching or parsing
	| { kind: "ready"; def: ViewDef; state: ViewState };

export function ViewRenderer(props: ViewRendererProps) {
	const { contextName, query, viewName } = props;

	const [stage, setStage] = useState<Stage>(() =>
		viewName ? { kind: "loading" } : { kind: "fallback" },
	);

	// Local copy of the row payload, seeded from the prop and then
	// overwritten when the user clicks Refresh in the view's toolbar.
	// Re-running the original GraphQL query is cheap and gives the
	// table a way to pick up changes that landed in the database via
	// detail-tab Save/Delete or anything else upstream of this tab.
	const [liveData, setLiveData] = useState<unknown>(props.data);
	useEffect(() => {
		setLiveData(props.data);
	}, [props.data]);

	// Refresh-flag bumped to trigger the load effect on demand. The
	// alternative — calling rpc inside an event handler and patching
	// state by hand — would duplicate the parse-and-load pipeline.
	// Bumping a counter and letting the effect re-run keeps the load
	// path single-source.
	const [refreshTick, setRefreshTick] = useState(0);

	// Stamp every effect run so a stale resolve cannot clobber the
	// state when props change mid-flight (rare but cheap to cover).
	const stampRef = useRef(0);

	useEffect(() => {
		if (!viewName) {
			setStage({ kind: "fallback" });
			return;
		}
		setStage({ kind: "loading" });
		const stamp = ++stampRef.current;

		void (async () => {
			try {
				const fetched = await rpc.getView({
					name:      viewName,
				});
				if (stamp !== stampRef.current) return;
				if (fetched.error || !fetched.json) {
					console.warn(
						`[view-renderer] getView(${viewName}) failed:`,
						fetched.error,
					);
					setStage({ kind: "fallback" });
					return;
				}

				const parsedJson = safeParseJson(fetched.json);
				const source = pickSource(parsedJson);
				if (!source) {
					console.warn(
						`[view-renderer] view "${viewName}" has no source`,
					);
					setStage({ kind: "fallback" });
					return;
				}

				const result = await parseViewInWorker(
					source,
					`db://oos.view/${viewName}`,
				);
				if (stamp !== stampRef.current) return;
				if (!result.def) {
					const firstErr = result.diagnostics[0]?.message ?? "no AST";
					console.warn(
						`[view-renderer] parse "${viewName}" failed: ${firstErr}`,
					);
					setStage({ kind: "fallback" });
					return;
				}

				// Domain-mismatch guard. The query opened this tab
				// against `contextName` (e.g. "person"), but the
				// pinned view may be defined over a different
				// domain (e.g. a stale "note_list" hint that
				// survived the resolver). Rendering through that
				// view would label the table with the wrong
				// header and feed the GraphQL rows into widgets
				// that bind on a different alias — confusing and
				// hard to debug. We bail out to the generic
				// ResultTable instead, which honours the actual
				// query's contextName.
				const viewDomains = result.def.domains.map((d) => d.name);
				if (
					contextName &&
					viewDomains.length > 0 &&
					!viewDomains.includes(contextName)
				) {
					console.warn(
						`[view-renderer] view "${viewName}" is defined over ` +
							`[${viewDomains.join(", ")}] but the query ran ` +
							`on "${contextName}" — falling back to the ` +
							`generic table to avoid a domain mismatch.`,
					);
					setStage({ kind: "fallback" });
					return;
				}

				const rows = await fetchRows(contextName);
				if (stamp !== stampRef.current) return;
				const state = new ViewState();
				const envelope = buildRowsEnvelope(result.def, rows);
				loadEnvelope(state, envelope);
				setStage({ kind: "ready", def: result.def, state });
			} catch (err) {
				if (stamp !== stampRef.current) return;
				const msg = err instanceof Error ? err.message : String(err);
				console.warn(
					`[view-renderer] unexpected error for "${viewName}": ${msg}`,
				);
				setStage({ kind: "fallback" });
			}
		})();
	}, [viewName, contextName, refreshTick]);

	// Auto-refresh subscription: when the view declares
	// `auto_refresh on <domain>.<event>`, subscribe to that channel
	// and re-fire the original GraphQL query whenever the domain
	// reports a change. The result replaces `liveData`, which causes
	// the parse effect above to re-run and rebuild the state.
	//
	// `query` may be empty (older fallback paths) — we skip the
	// subscription in that case since there's nothing to fetch.
	const autoRefresh = stage.kind === "ready" ? stage.def.autoRefresh : undefined;
	useEffect(() => {
		if (!autoRefresh) return;
		// On the declared domain event, bump the refresh tick. The load
		// effect re-runs and re-fetches rows via rpc.dataQuery — one
		// row-source path for first load, manual refresh, and auto-refresh.
		const off = subscribeDomainEvent(
			autoRefresh.domain,
			autoRefresh.event,
			() => setRefreshTick((t) => t + 1),
		);
		return off;
	}, [autoRefresh]);

	if (stage.kind === "loading") {
		return (
			<Center style={{ height: "100%" }}>
				<Stack align="center" gap="xs">
					<Loader type="dots" size="sm" />
					<Text size="xs" c="dimmed">View wird geladen…</Text>
				</Stack>
			</Center>
		);
	}

	if (stage.kind === "ready") {
		const handleAction = (
			action: ViewActionDef,
			row:    Record<string, unknown>,
		): void => {
			if (action.event !== "on_select") return;
			const fieldName = pickBindLocalField(action.navMods, contextName);
			const id = fieldName ? row[fieldName] : undefined;
			addDslDetail({
				contextName,
				viewName: action.target,
				data:     row,
				id:       coerceId(id),
			});
		};

		const handleToolbar = (item: ToolbarItemDef): void => {
			// On a list view, the toolbar items that make sense at
			// this layer are `new` (open a fresh detail tab) and
			// `refresh` (re-run the original query against oosgql).
			// Save / delete / exit live on the detail view's toolbar
			// and are silently swallowed here as a guard against a
			// view DSL with a misplaced item.
			if (item.kind === "new") {
				addDslDetail({
					contextName,
					viewName: item.target,
					data:     {},
				});
				return;
			}
			if (item.kind === "refresh") {
				void doRefresh();
				return;
			}
		};

		const doRefresh = (): void => {
			// Re-run the load effect, which re-fetches rows via
			// rpc.dataQuery({ domain: contextName }).
			setRefreshTick((t) => t + 1);
		};

		return (
			<ScrollArea style={{ height: "100%" }}>
				<Box p="lg">
					<QueryInspector entries={[{ label: "GraphQL", value: query, language: "graphql" }]} />
					<OnisinView
						def={stage.def}
						state={stage.state}
						onAction={handleAction}
						onToolbar={handleToolbar}
					/>
				</Box>
			</ScrollArea>
		);
	}

	// Fallback path — no viewName, or any failure that left us
	// without a parsed def. The user still sees the data, and the
	// QueryInspector still helps diagnose surprising results.
	return (
		<ScrollArea style={{ height: "100%" }}>
			<Box p="lg">
				<QueryInspector entries={[{ label: "GraphQL", value: query, language: "graphql" }]} />
				<ResultTable
					contextName={contextName}
					query={query}
					data={liveData}
				/>
			</Box>
		</ScrollArea>
	);
}

// ─── Helpers ─────────────────────────────────────────────────────────

interface ViewSourceResponse {
	name?:   unknown;
	source?: unknown;
	error?:  unknown;
}

/** safeParseJson returns the parsed body or undefined on bad JSON. */
function safeParseJson(text: string): ViewSourceResponse | undefined {
	try {
		return JSON.parse(text) as ViewSourceResponse;
	} catch {
		return undefined;
	}
}

/** pickSource pulls the .view source string out of /view/:name. */
function pickSource(body: ViewSourceResponse | undefined): string | undefined {
	if (!body) return undefined;
	return typeof body.source === "string" && body.source.length > 0
		? body.source
		: undefined;
}

/**
 * pickBindLocalField walks the NavModifierDef array looking for a
 * `bind` modifier and returns the local field name to read off the
 * row. The DSL syntax `bind=person.id` produces `{kind:"bind",
 * source:{domain:"person", field:"id"}}` — we read `source.field`.
 *
 * The `contextName` parameter is the domain the row belongs to. The
 * DSL allows binding from a foreign domain (e.g. for join scenarios)
 * but the simple case is always the same domain — we don't validate
 * domain match here, the workspace validator does.
 *
 * Returns undefined when the action has no bind modifier — the
 * caller treats that as "open without an id" (a fresh record from
 * a click, which is unusual but DSL-legal).
 */
function pickBindLocalField(
	navMods:     NavModifierDef[],
	contextName: string,
): string | undefined {
	void contextName;
	for (const m of navMods) {
		if (m.kind === "bind") return m.source.field;
	}
	return undefined;
}

/**
 * coerceId narrows the typing of an id pulled off a row. Numbers
 * and non-empty strings pass through; anything else returns
 * undefined so the detail tab opens as a new record.
 */
function coerceId(v: unknown): string | number | undefined {
	if (typeof v === "number" && Number.isFinite(v)) return v;
	if (typeof v === "string" && v.trim() !== "") return v;
	return undefined;
}

/**
 * fetchRows pulls the view's rows straight from the structured data
 * path (oos.cmd.data.query) keyed on the tab's contextName domain.
 *
 * Why fetch here instead of reading a payload handed down from the
 * agent: the headless oosagent's oos_query tool returns only a summary
 * ("10 rows") to keep the LLM context small, not the rows themselves.
 * Materialising the rows in the renderer over data.query also drops the
 * old GraphQL round-trip (rpc.runQuery) entirely.
 */
async function fetchRows(domain: string): Promise<Record<string, unknown>[]> {
	try {
		const resp = await rpc.dataQuery({ domain });
		if (resp.error) {
			console.warn(`[view-renderer] dataQuery(${domain}) failed:`, resp.error);
			return [];
		}
		return resp.rows;
	} catch (err) {
		console.warn(`[view-renderer] dataQuery(${domain}) threw:`, err);
		return [];
	}
}

/**
 * buildRowsEnvelope wraps the fetched rows in the shape
 * oos-ui-ts/loadEnvelope expects: rows sit under the primary domain's
 * alias as `<alias>.rows`, matching the `table -> <alias>.rows` bind
 * path the seeded view DSL uses. Falls back to the domain name when the
 * view declared no alias.
 */
function buildRowsEnvelope(
	def:  ViewDef,
	rows: Record<string, unknown>[],
): { content: Record<string, unknown> } {
	const primary = def.domains[0];
	const alias   = primary?.alias ?? primary?.name ?? "data";
	return { content: { [alias]: { rows } } };
}
