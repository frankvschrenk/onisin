// store/ui-state.ts — UI preferences via bun:sqlite (RPC).

import { useCallback, useEffect, useState } from "react";
import { rpc } from "../rpc";

export type ChatMode = "ask" | "forms" | "events" | "documents" | "dev";

export interface UiState {
	mode:     ChatMode;
	mapping:  string | null;
	streamId: string | null;
	/**
	 * When true, an Ask-mode answer opens in a new editable Markdown
	 * tab in the results group instead of replying as a chat bubble.
	 * Toggled from the Footer; persists across launches.
	 */
	askToTab: boolean;
}

export const DEFAULT_UI_STATE: UiState = {
	mode:     "forms",
	mapping:  null,
	streamId: null,
	askToTab: false,
};

const UI_KEY = "ui";

type Listener = (next: UiState) => void;
const listeners = new Set<Listener>();
function notifyListeners(value: UiState): void {
	for (const fn of listeners) fn(value);
}

export async function loadUiState(): Promise<UiState> {
	const { value } = await rpc.kvGet({ key: UI_KEY });
	return parseUiState(value);
}

export async function saveUiState(value: UiState): Promise<void> {
	await rpc.kvSet({ key: UI_KEY, value });
	notifyListeners(value);
}

export function useUiState(): {
	state:  UiState;
	loaded: boolean;
	save:   (next: UiState) => Promise<void>;
} {
	const [state,  setState]  = useState<UiState>(DEFAULT_UI_STATE);
	const [loaded, setLoaded] = useState(false);

	useEffect(() => {
		let cancelled = false;
		void loadUiState().then((value) => {
			if (cancelled) return;
			setState(value);
			setLoaded(true);
		});
		const onChange: Listener = (value) => {
			if (!cancelled) setState(value);
		};
		listeners.add(onChange);
		return () => { cancelled = true; listeners.delete(onChange); };
	}, []);

	const save = useCallback(async (next: UiState) => {
		await saveUiState(next);
	}, []);

	return { state, loaded, save };
}

function parseUiState(raw: unknown): UiState {
	if (!raw || typeof raw !== "object") return { ...DEFAULT_UI_STATE };
	const r = raw as Record<string, unknown>;
	return {
		mode:     r.mode === "ask" ? "ask" : r.mode === "events" ? "events" : r.mode === "documents" ? "documents" : r.mode === "dev" ? "dev" : "forms",
		mapping:  typeof r.mapping  === "string" && r.mapping  ? r.mapping  : null,
		streamId: typeof r.streamId === "string" && r.streamId ? r.streamId : null,
		askToTab: r.askToTab === true,
	};
}
