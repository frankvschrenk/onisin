// ModeSwitch.tsx — Four-way switch between ask, forms, events and documents mode.
//
// Lives in the Header right of the burger menu. The user toggles
// between a plain tool-less LLM question ("Ask"), the GraphQL ReAct
// loop ("Forms"), the deterministic vector-RAG pipeline ("Events"),
// and the Pipeline document runner ("Documents").
//
// Ask is the simplest path: the question goes straight to the model
// with no tools, no domain system prompt, no RAG — translation,
// arithmetic, text fixes. It deliberately cannot reach the database
// or the internet; that stays the job of the other modes (and, for
// the web, a future admin-gated capability).
//
// Mode is persisted via the ui store, so a fresh app launch lands
// the user back on the mode they last used. The tabs read from
// useUiState directly — no props — so dropping the component
// anywhere in the tree just works.
//
// Underline Tabs rather than Radio/SegmentedControl: the active mode
// has to stay obvious at a glance, and the mode list is expected to
// grow. A tab row absorbs new modes by staying one uniform strip
// (scrolling if it ever overflows) instead of widening every item or
// hiding choices behind a stepper.

import { useEffect, useRef } from "react";
import { Tabs, Group } from "@mantine/core";
import {
	IconMessageCircle,
	IconForms,
	IconCalendarEvent,
	IconFileText,
} from "@tabler/icons-react";

import { useUiState, type ChatMode } from "../store/ui-state";
import { openPipelineList }           from "../store/tabs";

export function ModeSwitch() {
	const { state, save, loaded } = useUiState();
	const documentsBootstrapped = useRef(false);

	// On first load after a restart the persisted mode may be
	// "documents" but the pipeline tab is not yet open (tabs aren't
	// persisted). Trigger openPipelineList once so the panel matches
	// the tab state. Guarded by a ref so it never repeats.
	useEffect(() => {
		if (!loaded) return;
		if (documentsBootstrapped.current) return;
		documentsBootstrapped.current = true;
		if (state.mode === "documents") openPipelineList();
	}, [loaded, state.mode]);

	if (!loaded) {
		// Render a minimal placeholder so the header height does
		// not jump while the IndexedDB read settles.
		return <Group h={32} />;
	}

	const onChange = (next: ChatMode) => {
		// Switching away from events mode does NOT clear streamId
		// — the user often flips back and forth and expects the
		// last picked stream to still be there.
		// Switching to documents opens the pipeline browser automatically.
		void save({ ...state, mode: next });
		if (next === "documents") openPipelineList();
	};

	return (
		<Tabs
			value={state.mode}
			// Tabs.onChange can emit null when an active tab is cleared;
			// that never happens here (no closable tabs), but guard so a
			// stray null can never be persisted as the mode.
			onChange={(v) => { if (v) onChange(v as ChatMode); }}
			variant="default"
		>
			<Tabs.List>
				<Tabs.Tab value="ask"       leftSection={<IconMessageCircle size={16} />}>Ask</Tabs.Tab>
				<Tabs.Tab value="forms"     leftSection={<IconForms size={16} />}>Forms</Tabs.Tab>
				<Tabs.Tab value="events"    leftSection={<IconCalendarEvent size={16} />}>Events</Tabs.Tab>
				<Tabs.Tab value="documents" leftSection={<IconFileText size={16} />}>Documents</Tabs.Tab>
			</Tabs.List>
		</Tabs>
	);
}
