// store/finetuning.ts — Per-model fine-tuning settings via bun:sqlite (RPC).

import { useCallback, useEffect, useState } from "react";
import { rpc } from "../rpc";

export interface FinetuningSettings {
	temperature: number;
	timeoutMs:   number;
	maxTokens:   number;
	topHits:     number;
}

export type FinetuningPreset = "cloud" | "local";

export const FINETUNING_PRESETS: Record<FinetuningPreset, FinetuningSettings> = {
	cloud: { temperature: 0.2, timeoutMs: 5 * 60 * 1000, maxTokens: 4096, topHits: 10 },
	local: { temperature: 0.2, timeoutMs: 90 * 1000,     maxTokens: 1024, topHits: 10 },
};

export const DEFAULT_FINETUNING: FinetuningSettings = FINETUNING_PRESETS.cloud;

const KEY_PREFIX = "finetuning:";

type Listener = (modelId: string, next: FinetuningSettings) => void;
const listeners = new Set<Listener>();
function notifyListeners(modelId: string, value: FinetuningSettings): void {
	for (const fn of listeners) fn(modelId, value);
}

export async function loadFinetuning(modelId: string): Promise<FinetuningSettings> {
	if (!modelId) return { ...DEFAULT_FINETUNING };
	const { value } = await rpc.kvGet({ key: `${KEY_PREFIX}${modelId}` });
	return parseFinetuning(value);
}

export async function saveFinetuning(modelId: string, value: FinetuningSettings): Promise<void> {
	if (!modelId) return;
	await rpc.kvSet({ key: `${KEY_PREFIX}${modelId}`, value });
	notifyListeners(modelId, value);
}

export function useFinetuning(modelId: string): {
	settings: FinetuningSettings;
	loaded:   boolean;
	save:     (next: FinetuningSettings) => Promise<void>;
} {
	const [settings, setSettings] = useState<FinetuningSettings>(DEFAULT_FINETUNING);
	const [loaded,   setLoaded]   = useState(false);

	useEffect(() => {
		let cancelled = false;
		setLoaded(false);
		void loadFinetuning(modelId).then((value) => {
			if (cancelled) return;
			setSettings(value);
			setLoaded(true);
		});
		const onChange: Listener = (changedId, value) => {
			if (!cancelled && changedId === modelId) setSettings(value);
		};
		listeners.add(onChange);
		return () => { cancelled = true; listeners.delete(onChange); };
	}, [modelId]);

	const save = useCallback(async (next: FinetuningSettings) => {
		await saveFinetuning(modelId, next);
	}, [modelId]);

	return { settings, loaded, save };
}

function parseFinetuning(raw: unknown): FinetuningSettings {
	if (!raw || typeof raw !== "object") return { ...DEFAULT_FINETUNING };
	const r = raw as Record<string, unknown>;
	return {
		temperature: numberOr(r.temperature, DEFAULT_FINETUNING.temperature),
		timeoutMs:   numberOr(r.timeoutMs,   DEFAULT_FINETUNING.timeoutMs),
		maxTokens:   numberOr(r.maxTokens,   DEFAULT_FINETUNING.maxTokens),
		topHits:     numberOr(r.topHits,     DEFAULT_FINETUNING.topHits),
	};
}

function numberOr(v: unknown, fallback: number): number {
	return typeof v === "number" && Number.isFinite(v) ? v : fallback;
}
