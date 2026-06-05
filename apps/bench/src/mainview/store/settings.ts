// src/mainview/store/settings.ts — typed settings state with pub/sub.
//
// The backend holds the authoritative copy (tauri-plugin-store settings.json).
// Here we keep a React-friendly copy for the UI, seeded once via the ready
// command (from App.tsx on mount) and written back via save_settings on save.

import { useCallback, useEffect, useState } from "react";
import type { BenchSettings } from "../types";

export type { AllowedRoot, BenchSettings, NatsServer } from "../types";

export const DEFAULT_SETTINGS: BenchSettings = {
  instanceName: "",
  servers: [],
  roots: [],
  otlpPort: 4318,
  dsn: "",
  appDatabase: "",
};

// ─── In-memory pub/sub ─────────────────────────────────────────

type Listener = (next: BenchSettings) => void;
const listeners = new Set<Listener>();

let cachedSettings: BenchSettings = DEFAULT_SETTINGS;
let cacheLoaded = false;

export function notifySettingsChange(value: BenchSettings): void {
  cachedSettings = value;
  cacheLoaded = true;
  for (const fn of listeners) fn(value);
}

// ─── Hook ────────────────────────────────────────────────

export function useSettings(): {
  settings: BenchSettings;
  loaded: boolean;
  save: (next: BenchSettings) => Promise<void>;
} {
  const [settings, setSettings] = useState<BenchSettings>(cachedSettings);
  const [loaded, setLoaded] = useState(cacheLoaded);

  useEffect(() => {
    let cancelled = false;
    const onChange: Listener = (value) => {
      if (!cancelled) {
        setSettings(value);
        setLoaded(true);
      }
    };
    listeners.add(onChange);
    // If the cache is already warm (App seeded it via ready()), adopt it now.
    if (cacheLoaded && !loaded) {
      setSettings(cachedSettings);
      setLoaded(true);
    }
    return () => {
      cancelled = true;
      listeners.delete(onChange);
    };
  }, []);

  const save = useCallback(async (next: BenchSettings) => {
    // Lazy import to avoid a circular dep at module init time.
    const { rpc } = await import("../rpc");
    await rpc.saveSettings({ settings: next });
    notifySettingsChange(next);
  }, []);

  return { settings, loaded, save };
}
