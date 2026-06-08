// StreamPicker.tsx — Stream selector for events mode.
//
// Reads the active mapping from the ui store (set by the
// MappingPicker) and lists every stream visible to that mapping.
// The user picks one before they can send a question.
//
// Why Autocomplete and not Select:
//   With dozens or hundreds of cases under one mapping a Select
//   combobox lists everything by default and forces visual
//   scanning. Autocomplete narrows the dropdown as the user
//   types, so the picker scales to large case files.
//
// Persistence model:
//   * The Autocomplete value is the full display string
//     ("fall-2024-0080 — Papageien-Ausbruch …"), held in
//     local state.
//   * The local draft starts empty even when the ui store
//     already carries a streamId from a previous session — a
//     pre-filled input is more obstructive than helpful when
//     the user wants to type a different case. The persisted
//     id is shown in the placeholder ("aktuell: …") so the
//     user knows the previous selection is still in effect.
//   * On commit (option submit / blur) the visible text is
//     mapped back to a real stream id via resolveStream and
//     written to the ui store.
//   * `clearable` enables Mantine's built-in "X" rightSection,
//     which fires onClear so we can wipe both the draft and
//     the persisted streamId in one click.

import { useEffect, useMemo, useState } from "react";
import { Autocomplete } from "@mantine/core";

import type { StreamSummary } from "../event-types";
import { rpc } from "../rpc";
import { useAppSettings } from "../store/settings";
import { useUiState } from "../store/ui-state";

export function StreamPicker() {
	const { loaded: settingsLoaded } = useAppSettings();
	const { state, save, loaded: uiLoaded } = useUiState();

	const [streams, setStreams] = useState<StreamSummary[]>([]);
	const [loadErr, setLoadErr] = useState<string | null>(null);
	const [draft,   setDraft]   = useState("");

	// Load streams whenever the active mapping changes.
	useEffect(() => {
		if (!settingsLoaded) return;
		if (!state.mapping) {
			setStreams([]);
			setLoadErr(null);
			return;
		}
		let cancelled = false;
		void (async () => {
			try {
				const res = await rpc.getEventStreams({
					mapping:  state.mapping!,
					limit:    100,
				});
				if (cancelled) return;
				if (res.error) {
					setLoadErr(res.error);
					return;
				}
				const body = JSON.parse(res.json) as { streams?: StreamSummary[] };
				setStreams(body.streams ?? []);
				setLoadErr(null);
			} catch (err) {
				if (cancelled) return;
				setLoadErr(err instanceof Error ? err.message : String(err));
			}
		})();
		return () => {
			cancelled = true;
		};
	}, [settingsLoaded, state.mapping]);

	// labels = the strings shown in the dropdown.
	// Each label starts with the stream id followed by an em-dash
	// and the description, so the user can either type the id or
	// search by free text.
	const labels = useMemo(() => {
		return streams.map((s) =>
			s.description ? `${s.stream} — ${s.description}` : s.stream,
		);
	}, [streams]);

	// resolveStream maps a free-form input back to a known stream
	// id. Accepts either the bare id or the full
	// "<stream> — <description>" label. Returns null when nothing
	// matches — the caller treats null as "clear the selection".
	const resolveStream = (text: string): string | null => {
		const trimmed = text.trim();
		if (!trimmed) return null;
		const direct = streams.find((s) => s.stream === trimmed);
		if (direct) return direct.stream;
		const labelled = streams.find((s) =>
			(s.description ? `${s.stream} — ${s.description}` : s.stream) === trimmed,
		);
		return labelled ? labelled.stream : null;
	};

	const commit = (text: string) => {
		if (!uiLoaded) return;
		const resolved = resolveStream(text);
		if (resolved !== state.streamId) {
			void save({ ...state, streamId: resolved });
		}
	};

	// onClear is what Mantine's `clearable` X-button fires.
	// Both the local draft and the persisted streamId need to go
	// — leaving the persisted id in place would trick the
	// composer into thinking a stream is still selected.
	const handleClear = () => {
		setDraft("");
		if (uiLoaded && state.streamId) {
			void save({ ...state, streamId: null });
		}
	};

	// Placeholder reflects the persisted state when the field is
	// empty: if the user already picked a stream earlier the hint
	// "aktuell: <stream>" reminds them which one is in effect even
	// though the input itself is blank.
	const placeholder = useMemo(() => {
		if (loadErr)              return `Fehler: ${loadErr}`;
		if (!state.mapping)       return "Erst Kontext wählen…";
		if (labels.length === 0)  return "Keine Streams";
		if (state.streamId)       return `aktuell: ${state.streamId}`;
		return "tippen oder auswählen…";
	}, [loadErr, state.mapping, state.streamId, labels.length]);

	return (
		<Autocomplete
			label="Stream"
			placeholder={placeholder}
			data={labels}
			value={draft}
			onChange={(v) => {
				setDraft(v);
			}}
			onOptionSubmit={(v) => commit(v)}
			onBlur={() => commit(draft)}
			disabled={!state.mapping || labels.length === 0}
			limit={20}
			clearable
			onClear={handleClear}
			comboboxProps={{ withinPortal: true }}
		/>
	);
}
