// OosSpotlight.tsx — Global command palette for the OOS desktop app.
//
// Uses the Mantine Spotlight v9 low-level composition API
// (SpotlightRoot + SpotlightSearch + SpotlightActionsList) so we
// can render grouped actions with per-group icons and two-line
// (label + description) action rows — the high-level <Spotlight>
// convenience wrapper collapses groups into a flat list.
//
// Action groups rendered:
//   Navigation  — static menu shortcuts
//   Settings    — deep-links into settings panels
//   Streams     — one entry per live stream (lazy, from NATS)
//   Mappings    — one entry per event mapping (lazy, from NATS)
//   Debug       — developer utilities
//
// Shortcut: mod+K (⌘K on macOS, Ctrl+K elsewhere). Spotlight's
// own shortcut registration handles the global intercept — this
// is why the old onKeyDown workaround in Chat.tsx was removed.
//
// CSS: @mantine/spotlight/styles.css is imported in index.css.

import { useEffect, useRef, useState } from "react";
import {
	SpotlightRoot,
	SpotlightSearch,
	SpotlightActionsList,
	SpotlightActionsGroup,
	SpotlightAction,
	SpotlightEmpty,
	type SpotlightActionData,
	type SpotlightActionGroupData,
} from "@mantine/spotlight";
import { IconSearch } from "@tabler/icons-react";

import { rpc } from "../rpc";
import {
	STATIC_ACTIONS,
	buildStreamActions,
	buildMappingActions,
} from "./spotlight-actions";

// ─── Types ────────────────────────────────────────────────────────────────

type ActionGroup = SpotlightActionGroupData;

// ─── Helpers ──────────────────────────────────────────────────────────────

/**
 * groupBy partitions a flat SpotlightActionData array into
 * SpotlightActionGroupData objects preserving insertion order of
 * the first occurrence of each group label.
 */
function groupBy(actions: SpotlightActionData[]): ActionGroup[] {
	const map = new Map<string, SpotlightActionData[]>();
	for (const a of actions) {
		const key = a.group ?? "Other";
		if (!map.has(key)) map.set(key, []);
		map.get(key)!.push(a);
	}
	return Array.from(map.entries()).map(([group, actions]) => ({ group, actions }));
}

/**
 * matchActions does case-insensitive substring matching of the query
 * against each action's label, description, keywords and group.
 *
 * Empty query passes everything through. Non-empty query keeps an
 * action when ANY of its searchable fields contains the query as a
 * substring — same forgiving semantic Mantine's high-level
 * <Spotlight> uses internally.
 */
function matchActions(
	actions: SpotlightActionData[],
	query:   string,
): SpotlightActionData[] {
	const q = query.trim().toLowerCase();
	if (!q) return actions;
	return actions.filter((a) => {
		const hay = [
			a.label,
			a.description,
			a.group,
			...(Array.isArray(a.keywords) ? a.keywords : a.keywords ? [a.keywords] : []),
		].filter((s): s is string => typeof s === "string")
		 .join(" ")
		 .toLowerCase();
		return hay.includes(q);
	});
}

// ─── Component ────────────────────────────────────────────────────────────

export function OosSpotlight() {
	const loadedRef = useRef(false);
	const [actions, setActions] = useState<SpotlightActionData[]>(STATIC_ACTIONS);
	const [query,   setQuery]   = useState("");

	useEffect(() => {
		void load();
	}, []);

	async function load(): Promise<void> {
		if (loadedRef.current) return;
		loadedRef.current = true;
		try {
			const [streamActions, mappingActions] = await Promise.all([
				buildStreamActions({ listAllStreams: () => rpc.listAllStreams({}) }),
				buildMappingActions({ getEventMappings: () => rpc.getEventMappings({}) }),
			]);
			setActions([...STATIC_ACTIONS, ...streamActions, ...mappingActions]);
		} catch {
			// NATS not ready — statics still work.
		}
	}

	// Two-stage filter:
	//
	//   1. Streams and Mappings stay hidden until the user has typed
	//      at least 2 characters — with 100+ stream entries the
	//      palette would otherwise drown the navigation hits.
	//   2. Once there is a query, every action is matched against it
	//      by label/description/keywords. The composition API does
	//      not filter on its own (unlike the high-level <Spotlight>
	//      wrapper), so without this the keyboard selection lands on
	//      the first rendered row regardless of what the user typed.
	const baseActions = query.length >= 2
		? actions
		: actions.filter((a) => a.group !== "Streams" && a.group !== "Mappings");

	const visibleActions = matchActions(baseActions, query);

	const groups = groupBy(visibleActions);

	return (
		<SpotlightRoot
			shortcut={["mod+k"]}
			tagsToIgnore={[]}
			query={query}
			onQueryChange={setQuery}
			onSpotlightClose={() => setQuery("")}
		>
			<SpotlightSearch
				placeholder="Search actions, streams, settings…"
				leftSection={<IconSearch size={16} />}
			/>

			<SpotlightActionsList>
				{groups.length === 0 && <SpotlightEmpty>No actions found</SpotlightEmpty>}
				{groups.map((g) => (
					<SpotlightActionsGroup key={g.group} label={g.group}>
						{g.actions.map((action) => (
							<SpotlightAction
								key={action.id}
								label={action.label}
								description={action.description}
								leftSection={action.leftSection}
								rightSection={action.rightSection}
								onClick={action.onClick as React.MouseEventHandler<HTMLButtonElement>}
							/>
						))}
					</SpotlightActionsGroup>
				))}
			</SpotlightActionsList>
		</SpotlightRoot>
	);
}
