// store/settings.ts — Settings bridge between renderer and Bun process.
//
// Settings are stored by the Bun process (settings-store.ts) as a JSON
// file on disk. The renderer communicates via RPC — NOT IndexedDB/Dexie.
// Since CEF sandboxes the renderer, Dexie cannot be relied upon for
// data that the Bun process also needs (NATS URL, LLM endpoint etc.).
//
// Multi-consumer reactivity is kept via the same in-memory pub/sub bus
// as before so components don't need to be aware of the RPC layer.

import { useCallback, useEffect, useState } from "react";
import { rpc } from "../rpc";

/** Connection settings persisted by the Bun process. */
export interface AppSettings {
	[key: string]: string;  // index signature required by the Bun-side SettingsRecord shape
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

/** Defaults used when no settings exist yet. */
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

// ── In-memory pub/sub ───────────────────────────────────────────

type Listener = (next: AppSettings) => void;
const listeners = new Set<Listener>();

function notifyListeners(value: AppSettings): void {
	for (const fn of listeners) fn(value);
}

// ── Public API ───────────────────────────────────────────────

/**
 * loadAppSettings fetches current settings from the Bun process via RPC.
 */
export async function loadAppSettings(): Promise<AppSettings> {
	try {
		return await rpc.loadSettings({});
	} catch {
		return { ...DEFAULT_APP_SETTINGS };
	}
}

/**
 * saveAppSettings sends settings to the Bun process via RPC.
 * Bun writes them to disk and applies side-effects (e.g. NATS reconnect).
 */
export async function saveAppSettings(value: AppSettings): Promise<void> {
	const result = await rpc.saveSettings(value);
	if (!result.ok) {
		throw new Error(result.error ?? "saveSettings failed");
	}
	notifyListeners(value);
}

/**
 * useAppSettings is the React hook for reading and writing settings.
 * Loads from Bun on mount, re-renders on any save by any component.
 */
export function useAppSettings(): {
	settings: AppSettings;
	loaded:   boolean;
	save:     (next: AppSettings) => Promise<void>;
} {
	const [settings, setSettings] = useState<AppSettings>(DEFAULT_APP_SETTINGS);
	const [loaded,   setLoaded]   = useState(false);

	useEffect(() => {
		let cancelled = false;

		void loadAppSettings().then((value) => {
			if (cancelled) return;
			setSettings(value);
			setLoaded(true);
		});

		const onChange: Listener = (value) => {
			if (!cancelled) setSettings(value);
		};
		listeners.add(onChange);

		return () => {
			cancelled = true;
			listeners.delete(onChange);
		};
	}, []);

	const save = useCallback(async (next: AppSettings) => {
		await saveAppSettings(next);
	}, []);

	return { settings, loaded, save };
}
