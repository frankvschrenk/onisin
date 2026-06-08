// DetailRenderer.tsx — single-record edit tab driven by a .view DSL.
//
// Pipeline:
//
//   1. Fetch the .view source from oosgql via rpc.getView.
//   2. Fetch the .domain source from oosgql via rpc.getDomain.
//      Done in parallel — the detail tab is the slowest cold-path
//      render in the app, so we overlap the second round-trip with
//      the first.
//   3. Parse both in the shared Langium worker (parse-client.ts).
//      Same worker singleton apps/oos already uses for ViewRenderer.
//   4. Load the record and its dropdown option lists in ONE
//      structured round-trip via rpc.dataQuery({ withOptions: true }).
//      This folds the old detail-query + meta-query GraphQL pair into
//      a single oos.cmd.data.query call — the read path is ungated, so
//      the form populates even before auth lands. Wrap the row +
//      options into a oos-ui-ts envelope and load a fresh ViewState.
//   5. Render <OnisinView> with onToolbar wired:
//
//        save   → buildEvent(state) → rpc.dataMutate(insert|update)
//                 → on success refresh the state from the record the
//                 server returned and stamp "Gespeichert um HH:MM".
//        delete → Mantine confirm modal (using the toolbar item's
//                 confirm message) → rpc.dataMutate(delete) → on
//                 success closeTab.
//        exit   → closeTab.
//
// Writes carry an empty role for now; oosgql-rs gates mutate on the
// role (read stays open), so save/delete report "role required" until
// the native auth slice supplies one. The form still loads and edits
// — only persistence waits on auth.
//
// The state lives across renders via a useRef so toggling between
// tabs doesn't drop in-flight edits, and a stamp ref guards against
// the stale-async-resolve race that bit ViewRenderer earlier.

import { useEffect, useRef, useState } from "react";
import {
	Alert,
	Box,
	Button,
	Center,
	Group,
	Loader,
	ScrollArea,
	Stack,
	Text,
} from "@mantine/core";
import { modals }        from "@mantine/modals";
import { notifications } from "@mantine/notifications";
import { IconLock, IconAlertCircle as IconAlert } from "@tabler/icons-react";
import { IconAlertCircle } from "@tabler/icons-react";
import {
	OnisinView,
	ViewState,
	buildEvent,
	loadEnvelope,
} from "oos-ui-ts";
import type { DomainDef, DomainFieldDef } from "oos-dsls-ts";
import type {
	ToolbarItemDef,
	ViewDef,
} from "oos-dsls-ts/types";

import {
	parseDomainInWorker,
	parseViewInWorker,
} from "../../lang/worker/parse-client";
import { publish as publishDomainEvent } from "../domain-events";
import { rpc, type DataWhere } from "../rpc";

import { closeTab } from "../store/tabs";
import { QueryInspector } from "./QueryInspector";

interface DetailRendererProps {
	tabId:       string;
	contextName: string;
	viewName:    string;
	data:        Record<string, unknown>;
	id?:         string | number;
}

/** Stages the renderer walks through. */
type Stage =
	| { kind: "loading" }
	| { kind: "error"; message: string }
	| {
		kind:    "ready";
		def:     ViewDef;
		domain:  DomainDef;
		state:   ViewState;
		/**
		 * Diagnostic strings surfaced via the QueryInspector panel.
		 * Currently the data.query request feeding the form; later save
		 * and delete mutations append to this list as they fire.
		 */
		inspect: ReadonlyArray<{ label: string; value: string }>;
	};

export function DetailRenderer(props: DetailRendererProps) {
	const { tabId, contextName, viewName, data, id } = props;
	const [stage, setStage] = useState<Stage>({ kind: "loading" });

	// Visible "saved at HH:MM" indicator after a successful save.
	// Clears when the user edits again — but for the first cut we
	// just stamp it and leave the user to read the change.
	const [savedAt, setSavedAt] = useState<string | null>(null);

	// Stale-resolve guard for the load effect.
	const stampRef = useRef(0);

	useEffect(() => {
		setStage({ kind: "loading" });
		setSavedAt(null);
		const stamp = ++stampRef.current;

		void (async () => {
			try {
				// Fetch view + domain in parallel.
				const [viewResp, domainResp] = await Promise.all([
					rpc.getView({
						name:      viewName,
					}),
					rpc.getDomain({
						name:      contextName,
					}),
				]);
				if (stamp !== stampRef.current) return;

				if (viewResp.error || !viewResp.json) {
					setStage({
						kind:    "error",
						message: viewResp.error ?? `view "${viewName}" leer`,
					});
					return;
				}
				if (domainResp.error || !domainResp.json) {
					setStage({
						kind:    "error",
						message: domainResp.error ?? `domain "${contextName}" leer`,
					});
					return;
				}

				const viewSource   = pickSource(viewResp.json);
				const domainSource = pickSource(domainResp.json);
				if (!viewSource || !domainSource) {
					setStage({
						kind:    "error",
						message: "view oder domain ohne Quelle in der DB",
					});
					return;
				}

				const [viewParsed, domainParsed] = await Promise.all([
					parseViewInWorker(viewSource, `db://oos.view/${viewName}`),
					parseDomainInWorker(domainSource, `db://oos.domain/${contextName}`),
				]);
				if (stamp !== stampRef.current) return;

				if (!viewParsed.def) {
					setStage({
						kind:    "error",
						message: viewParsed.diagnostics[0]?.message ?? "view-parse fehlgeschlagen",
					});
					return;
				}
				if (!domainParsed.def) {
					setStage({
						kind:    "error",
						message: domainParsed.diagnostics[0]?.message ?? "domain-parse fehlgeschlagen",
					});
					return;
				}

				// Fetch the full record and the dropdown options in ONE
				// structured round-trip. The list view that opened this tab
				// projected only its visible columns, so its row stub is
				// missing most fields the detail form binds to. data.query
				// with `where id` returns the complete row; withOptions
				// attaches every meta dropdown list (keyed by short name)
				// in the same reply, folding the old detail+meta query pair
				// into a single call. Both halves soft-fail without breaking
				// the form:
				//   - row miss  → fall back to the list-view stub
				//   - opts miss → dropdowns render value-only (raw text)
				const hasId = id !== undefined && id !== "" && id !== 0 && id !== "0";
				const where: DataWhere[] | undefined = hasId
					? [{ field: "id", op: "eq", value: String(id) }]
					: undefined;
				const queryRequest = { domain: contextName, where, limit: 1, withOptions: true };

				const queryResp = await rpc.dataQuery(queryRequest);
				if (stamp !== stampRef.current) return;

				let metaPayload: Record<string, unknown> = {};
				let detailRow:   Record<string, unknown> | undefined;

				if (queryResp.error) {
					console.warn(
						`[detail-renderer] data.query failed: ${queryResp.error}`,
					);
				} else {
					// Only adopt the fetched row when we asked for a specific
					// id; the capped row of a "new" record query is noise.
					if (hasId && queryResp.rows.length > 0) {
						detailRow = queryResp.rows[0];
					}
					if (queryResp.options) metaPayload = queryResp.options;
				}

				// Prefer the freshly fetched row when we have one;
				// fall back to whatever the list view passed in so a
				// brand-new record (no id, no detail-fetch) still
				// renders with whatever stub the caller seeded.
				const rowForState = detailRow ?? data;

				const state = new ViewState();
				const envelope = wrapDataAsEnvelope(viewParsed.def, rowForState, metaPayload);
				loadEnvelope(state, envelope);

				const inspect: { label: string; value: string }[] = [
					{ label: "Load (data.query)", value: JSON.stringify(queryRequest) },
				];

				setStage({
					kind:   "ready",
					def:    viewParsed.def,
					domain: domainParsed.def,
					state,
					inspect,
				});
			} catch (err) {
				if (stamp !== stampRef.current) return;
				const msg = err instanceof Error ? err.message : String(err);
				setStage({ kind: "error", message: msg });
			}
		})();
	}, [viewName, contextName, id, data]);

	if (stage.kind === "loading") {
		return (
			<Center style={{ height: "100%" }}>
				<Stack align="center" gap="xs">
					<Loader type="dots" size="sm" />
					<Text size="xs" c="dimmed">Detail wird geladen…</Text>
				</Stack>
			</Center>
		);
	}

	if (stage.kind === "error") {
		return (
			<Box p="lg">
				<Alert
					color="red"
					variant="light"
					icon={<IconAlertCircle size={16} />}
					title="Detail konnte nicht geladen werden"
				>
					<Text size="sm">{stage.message}</Text>
				</Alert>
			</Box>
		);
	}

	return (
		<DetailReady
			tabId={tabId}
			contextName={contextName}
			def={stage.def}
			domain={stage.domain}
			state={stage.state}
			inspect={stage.inspect}
			savedAt={savedAt}
			onSavedAt={setSavedAt}
		/>
	);
}

// ─── Ready stage — the actual editing surface ────────────────────

interface DetailReadyProps {
	tabId:       string;
	contextName: string;
	def:         ViewDef;
	domain:      DomainDef;
	state:       ViewState;
	inspect:     ReadonlyArray<{ label: string; value: string }>;
	savedAt:     string | null;
	onSavedAt:   (stamp: string | null) => void;
}

/**
 * DetailReady is the success-path subtree. Pulled out so the busy /
 * error states stay tiny and the toolbar handlers don't have to
 * narrow stage every time they fire.
 */
function DetailReady(props: DetailReadyProps) {
	const { tabId, contextName, def, domain, state, savedAt, onSavedAt } = props;
	// Live diagnostic strings shown in the QueryInspector. Seeded
	// with the data.query that ran during load; appended to as the
	// user fires save and delete mutations so the actual outbound
	// requests are visible right alongside the form.
	const [extraInspect, setExtraInspect] = useState<
		Array<{ label: string; value: string }>
	>([]);
	const inspect = [...props.inspect, ...extraInspect];
	const pushInspect = (label: string, value: string): void => {
		setExtraInspect((prev) => [...prev, { label, value }]);
	};

	// oosgql-rs gates mutate on the role. Auth is a later slice, so the
	// renderer sends an empty role today — the server answers "role
	// required" and the save/delete handlers surface that as a
	// permission notice. Once native PKCE lands, the resolved role
	// flows in here.
	const role = "";

	// name → field lookup, used to drop readonly fields from the mutate
	// `set` (the server strips them too, but a tight set is clearer).
	const fieldsByName = indexDomainFields(domain);

	// Local alias of the primary domain — the prefix every state key
	// touched by save/delete carries. Falls back to the domain name
	// when the view declared no explicit alias, which keeps single-
	// domain views working unchanged.
	const primaryAlias = def.domains[0]?.alias ?? contextName;
	// Domain *name* (not alias) for cross-tab pub/sub. List views
	// declaring `auto_refresh on <name>.changed` subscribe under
	// the domain name, not its in-view alias.
	const primaryDomainName = def.domains[0]?.name ?? contextName;

	const handleToolbar = (item: ToolbarItemDef): void => {
		switch (item.kind) {
			case "save":
				void doSave();
				return;
			case "delete":
				doConfirmDelete(item.confirm);
				return;
			case "exit":
				closeTab(tabId);
				return;
			case "new":
				// `new` on a detail toolbar is rare (usually only
				// list views have it). For symmetry we swallow it
				// today rather than treating it as another save —
				// a follow-up could route to addDslDetail.
				return;
		}
	};

	const doSave = async (): Promise<void> => {
		try {
			// buildEvent returns a flat, bare-keyed, string-valued map
			// (the view's `person.title` becomes `title`), which is
			// exactly the shape data.mutate's text-bound `set` wants.
			const flat = buildEvent(def.name, "save", state);
			const idValue = flat.id;
			const isUpdate = !isEmptyId(idValue);

			// Strip id (sent separately) and readonly fields from the set.
			const set: Record<string, string> = {};
			for (const [key, value] of Object.entries(flat)) {
				if (key === "id") continue;
				if (fieldsByName.get(key)?.readOnly) continue;
				set[key] = value;
			}

			const request = {
				domain: contextName,
				op:     (isUpdate ? "update" : "insert") as "update" | "insert",
				role,
				set,
				...(isUpdate ? { id: idValue } : {}),
			};
			pushInspect("Save (data.mutate)", JSON.stringify(request));

			const resp = await rpc.dataMutate(request);
			if (resp.error) {
				const denied = isPermissionError(resp.error);
				notifications.show({
					title:     denied ? "Keine Berechtigung" : "Speichern fehlgeschlagen",
					message:   denied
						? "Deine Rolle hat keinen Schreibzugriff auf diesen Datensatz."
						: resp.error,
					color:     denied ? "orange" : "red",
					icon:      denied ? <IconLock size={16} /> : <IconAlert size={16} />,
					autoClose: 5000,
				});
				return;
			}

			// Refresh from the record the server returned — the caller's
			// local copy may be one round of server-side transforms
			// behind (timestamps, generated ids on insert).
			refreshFromRecord(state, primaryAlias, resp.record);
			onSavedAt(formatNow());
			// Notify any sibling list-view tab to refresh.
			publishDomainEvent(primaryDomainName, "changed");
		} catch (err) {
			const msg = err instanceof Error ? err.message : String(err);
			modals.open({
				title:    "Speichern fehlgeschlagen",
				children: <Text size="sm">{msg}</Text>,
			});
		}
	};

	const doConfirmDelete = (message: string | undefined): void => {
		const idValue = state.get(`${primaryAlias}.id`);
		if (!idValue || idValue === "0") {
			modals.open({
				title:    "Löschen nicht möglich",
				children: <Text size="sm">Dieser Datensatz hat noch keine ID.</Text>,
			});
			return;
		}
		modals.openConfirmModal({
			title:    "Datensatz löschen",
			children: <Text size="sm">{message ?? "Wirklich löschen?"}</Text>,
			labels:   { confirm: "Löschen", cancel: "Abbrechen" },
			confirmProps: { color: "red" },
			onConfirm: () => void doDelete(idValue),
		});
	};

	const doDelete = async (idValue: string): Promise<void> => {
		try {
			const request = {
				domain: contextName,
				op:     "delete" as const,
				role,
				id:     idValue,
			};
			pushInspect("Delete (data.mutate)", JSON.stringify(request));
			const resp = await rpc.dataMutate(request);
			if (resp.error) {
				const denied = isPermissionError(resp.error);
				notifications.show({
					title:     denied ? "Keine Berechtigung" : "Löschen fehlgeschlagen",
					message:   denied
						? "Deine Rolle hat keinen Löschzugriff auf diesen Datensatz."
						: resp.error,
					color:     denied ? "orange" : "red",
					icon:      denied ? <IconLock size={16} /> : <IconAlert size={16} />,
					autoClose: 5000,
				});
				return;
			}
			publishDomainEvent(primaryDomainName, "changed");
			closeTab(tabId);
		} catch (err) {
			const msg = err instanceof Error ? err.message : String(err);
			modals.open({
				title:    "Löschen fehlgeschlagen",
				children: <Text size="sm">{msg}</Text>,
			});
		}
	};

	return (
		<ScrollArea style={{ height: "100%" }}>
			<Box p="lg">
				<QueryInspector
					heading="Detail-Queries"
					entries={inspect}
				/>
				<OnisinView
					def={def}
					state={state}
					domain={domain}
					onToolbar={handleToolbar}
				/>
				{savedAt && (
					<Group justify="flex-end" mt="md">
						<Text size="xs" c="dimmed">
							Gespeichert um {savedAt}
						</Text>
					</Group>
				)}
			</Box>
		</ScrollArea>
	);
}

// ─── Helpers ────────────────────────────────────────────

/**
 * pickSource extracts a `.source` string from an oosgql gql.view or
 * gql.domain response body. Returns undefined on any shape mismatch
 * — the caller surfaces a friendly error message.
 */
function pickSource(json: string): string | undefined {
	try {
		const parsed = JSON.parse(json) as Record<string, unknown>;
		const src = parsed.source;
		return typeof src === "string" && src.length > 0 ? src : undefined;
	} catch {
		return undefined;
	}
}

/** indexDomainFields builds a name → field lookup once per ready render. */
function indexDomainFields(domain: DomainDef): Map<string, DomainFieldDef> {
	const out = new Map<string, DomainFieldDef>();
	for (const f of domain.fields) out.set(f.name, f);
	return out;
}

/**
 * isEmptyId decides INSERT vs UPDATE: a missing/blank/zero id means
 * the row does not exist yet. Mirrors the legacy convention so the
 * verb choice matches what the rest of the stack expects.
 */
function isEmptyId(v: string | undefined): boolean {
	if (v === undefined) return true;
	const t = v.trim();
	return t === "" || t === "0";
}

/**
 * isPermissionError recognises the role/permission denials oosgql-rs
 * returns from data.mutate ("role required" before auth, "not allowed
 * to write" when the role lacks the action) so the UI can show a
 * permission notice rather than a generic failure.
 */
function isPermissionError(error: string): boolean {
	const e = error.toLowerCase();
	return e.includes("role required") ||
		e.includes("not allowed") ||
		e.includes("permission") ||
		e.includes("not authenticated");
}

/**
 * refreshFromRecord pushes the server's authoritative record back
 * into the state so a generated id (insert) or server-side
 * updated_at (update) lands in the form. data.mutate returns the
 * full row under `record`; we re-key each field under the view's
 * primary alias to match the bind paths the widgets read.
 */
function refreshFromRecord(
	state:        ViewState,
	primaryAlias: string,
	record:       Record<string, unknown> | undefined,
): void {
	if (!record) return;
	for (const [field, value] of Object.entries(record)) {
		if (value === null || value === undefined) continue;
		state.set(`${primaryAlias}.${field}`, String(value));
	}
}

/**
 * wrapDataAsEnvelope shapes the seed data into the envelope the
 * oos-ui-ts ViewState expects. Detail tabs deal with a single
 * record so the content slot is always
 * `{ <domain>: { …row… } }`.
 *
 * An empty seed (= "new" record) still calls loadEnvelope with an
 * empty domain object so the state is populated with the right
 * keys at the right paths and downstream widgets render with empty
 * inputs rather than "undefined".
 *
 * The optional `meta` map is dropped into the envelope verbatim;
 * loadEnvelope keys options by their short name ("roles",
 * "cities", …) — the same keys data.query's withOptions returns.
 */
function wrapDataAsEnvelope(
	def:  ViewDef,
	data: Record<string, unknown>,
	meta: Record<string, unknown> = {},
): { content: Record<string, unknown>; meta: Record<string, unknown> } {
	// Detail-tabs are always primary-domain centric: the row delivered
	// by the list view belongs to the view's first (primary) domain.
	// The envelope is keyed by that domain's local alias so widget
	// bind paths like `p.firstname` resolve directly.
	const primary = def.domains[0];
	const alias   = primary?.alias ?? primary?.name ?? "data";
	return {
		content: { [alias]: data },
		meta,
	};
}

/** formatNow returns the current local time as "HH:MM". */
function formatNow(): string {
	const d  = new Date();
	const hh = String(d.getHours()).padStart(2, "0");
	const mm = String(d.getMinutes()).padStart(2, "0");
	return `${hh}:${mm}`;
}
