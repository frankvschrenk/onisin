// store/settings.ts — Settings bridge for oosd renderer.
//
// Mirrors the pattern from oos: settings live in Bun (settings-store.ts)
// and are read/written via RPC. CEF sandboxes the renderer so IndexedDB
// is not accessible from Bun.

import { useCallback, useEffect, useState } from "react";
import { rpc } from "../rpc";

export interface OosdSettings {
	natsUrl:    string;
	dbUrl:      string;
	llmBaseUrl: string;
	llmApiKey:  string;
	llmModel:   string;
}

export const DEFAULT_OOSD_SETTINGS: OosdSettings = {
	natsUrl:    "nats://localhost:4222",
	dbUrl:      "postgres://postgres:demo@localhost:5432/onisin",
	llmBaseUrl: "http://localhost:11434",
	llmApiKey:  "",
	llmModel:   "",
};

type Listener = (next: OosdSettings) => void;
const listeners = new Set<Listener>();

function notifyListeners(value: OosdSettings): void {
	for (const fn of listeners) fn(value);
}

export async function loadOosdSettings(): Promise<OosdSettings> {
	try {
		return await rpc.loadSettings({});
	} catch {
		return { ...DEFAULT_OOSD_SETTINGS };
	}
}

export async function saveOosdSettings(value: OosdSettings): Promise<void> {
	const result = await rpc.saveSettings(value);
	if (!result.ok) throw new Error(result.error ?? "saveSettings failed");
	notifyListeners(value);
}

export function useOosdSettings(): {
	settings: OosdSettings;
	loaded:   boolean;
	save:     (next: OosdSettings) => Promise<void>;
} {
	const [settings, setSettings] = useState<OosdSettings>(DEFAULT_OOSD_SETTINGS);
	const [loaded,   setLoaded]   = useState(false);

	useEffect(() => {
		let cancelled = false;
		void loadOosdSettings().then((value) => {
			if (cancelled) return;
			setSettings(value);
			setLoaded(true);
		});
		const onChange: Listener = (value) => { if (!cancelled) setSettings(value); };
		listeners.add(onChange);
		return () => { cancelled = true; listeners.delete(onChange); };
	}, []);

	const save = useCallback(async (next: OosdSettings) => {
		await saveOosdSettings(next);
	}, []);

	return { settings, loaded, save };
}
