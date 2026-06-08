// store/pipeline-steps.ts — Per-turn pipeline step outputs via bun:sqlite (RPC).
//
// Companion to store/turns.ts. Turns hold the run-level summary,
// these rows hold the per-step detail the inspector unfolds.

import { useEffect, useState } from "react";
import { rpc } from "../rpc";
import type { PipelineStepOutputRow } from "./store-types";

export type { PipelineStepOutputRow };

export interface StepUsage {
	promptTokens:     number;
	completionTokens: number;
	totalTokens:      number;
}

/**
 * Runtime shape used by the inspector. Mirrors PipelineStepOutputRow but
 * with chunks and usage already JSON-parsed.
 */
export interface PipelineStepRecord {
	turnId:     string;
	stepIndex:  number;
	stepName:   string;
	stepKind:   string;
	summary:    string;
	chunks:     string[];
	durationMs: number;
	usage:      StepUsage;
	createdAt:  string;
}

function rowToRecord(r: PipelineStepOutputRow): PipelineStepRecord {
	let chunks: string[] = [];
	let usage:  StepUsage = { promptTokens: 0, completionTokens: 0, totalTokens: 0 };
	try { chunks = JSON.parse(r.chunks) as string[]; } catch { /* ok */ }
	try { usage  = JSON.parse(r.usage)  as StepUsage; } catch { /* ok */ }
	return {
		turnId:     r.turnId,
		stepIndex:  r.stepIndex,
		stepName:   r.stepName,
		stepKind:   r.stepKind,
		summary:    r.summary,
		chunks,
		durationMs: r.durationMs,
		usage,
		createdAt:  r.createdAt,
	};
}

export async function loadPipelineSteps(turnId: string): Promise<PipelineStepRecord[]> {
	const { steps } = await rpc.loadPipelineSteps({ turnId });
	return steps.map(rowToRecord);
}

/**
 * useTurnPipelineSteps reactively loads the step outputs for one turn.
 * Returns an empty array until the load completes or when the turn has
 * no associated pipeline rows.
 */
export function useTurnPipelineSteps(turnId: string | undefined): {
	steps:  PipelineStepRecord[];
	loaded: boolean;
} {
	const [steps,  setSteps]  = useState<PipelineStepRecord[]>([]);
	const [loaded, setLoaded] = useState(false);

	useEffect(() => {
		if (!turnId) { setSteps([]); setLoaded(true); return; }
		let cancelled = false;
		setLoaded(false);
		void (async () => {
			const rows = await loadPipelineSteps(turnId);
			if (cancelled) return;
			setSteps(rows);
			setLoaded(true);
		})();
		return () => { cancelled = true; };
	}, [turnId]);

	return { steps, loaded };
}
