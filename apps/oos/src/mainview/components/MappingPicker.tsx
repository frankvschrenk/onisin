// MappingPicker.tsx — Mapping selector for events mode.
//
// Renders a Mantine Select with every enabled event mapping
// (police, support, ...) loaded from oosai's /event/mappings.
// The picked name is persisted in the ui store as `mapping`.
//
// Switching the mapping clears the previously selected streamId
// — a stream from the police mapping is meaningless when the
// active mapping is now support.
//
// The picker is intentionally simple: no auto-pick, no fallback
// to a "first enabled" heuristic. The user is in control. The
// composer stays disabled until a mapping AND a stream are both
// chosen.

import { useCallback, useEffect, useMemo, useState } from "react";
import { Select } from "@mantine/core";

import type { EventMapping } from "../event-types";
import { rpc } from "../rpc";
import { useAppSettings } from "../store/settings";
import { useUiState } from "../store/ui-state";

export function MappingPicker() {
	const { loaded: settingsLoaded } = useAppSettings();
	const { state, save, loaded: uiLoaded } = useUiState();

	const [mappings, setMappings] = useState<EventMapping[]>([]);
	const [loadErr,  setLoadErr]  = useState<string | null>(null);

	// Load mappings from oosai. Extracted so it can be called both on
	// mount and when oosai notifies us of a new mapping via SSE.
	const loadMappings = useCallback(async () => {
		if (!settingsLoaded) return;
		try {
			const res = await rpc.getEventMappings({});
			if (res.error) { setLoadErr(res.error); return; }
			const body = JSON.parse(res.json) as { mappings?: EventMapping[] };
			setMappings(body.mappings ?? []);
			setLoadErr(null);
		} catch (err) {
			setLoadErr(err instanceof Error ? err.message : String(err));
		}
	}, [settingsLoaded]);

	// Initial load.
	useEffect(() => { void loadMappings(); }, [loadMappings]);



	const options = useMemo(() => {
		return mappings
			.filter((m) => m.enabled)
			.map((m) => ({
				value: m.name,
				label: m.listener_active ? m.name : `${m.name} (offline)`,
			}));
	}, [mappings]);

	return (
		<Select
			label="Kontext"
			placeholder={
				loadErr
					? `Fehler: ${loadErr}`
					: options.length === 0
						? "Lade…"
						: "auswählen…"
			}
			data={options}
			value={state.mapping}
			onChange={(v) => {
				if (!uiLoaded) return;
				// Switching the mapping wipes the stream — a stream
				// from the previous mapping does not belong here.
				void save({ ...state, mapping: v, streamId: null });
			}}
			searchable
			clearable
			disabled={options.length === 0}
			comboboxProps={{ withinPortal: true }}
		/>
	);
}
