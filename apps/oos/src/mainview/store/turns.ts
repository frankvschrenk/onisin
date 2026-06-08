// store/turns.ts — Per-turn telemetry via bun:sqlite (RPC).

import { useCallback, useEffect, useState } from "react";
import { rpc } from "../rpc";
import type { TurnRow } from "./store-types";

// Re-export types the agent loop and activity panels use.
export type { TurnRow };

export type TurnStatus = "success" | "error" | "cancelled" | "step_limit";

export interface TurnToolCall {
	callId:     string;
	name:       string;
	args:       unknown;
	startedAt:  string;
	finishedAt: string;
	durationMs: number;
	ok:         boolean;
	result:     unknown;
}

export interface TurnUsage {
	promptTokens:     number;
	completionTokens: number;
	totalTokens:      number;
}

/**
 * TurnRecord is the runtime shape used by the agent loop and the
 * Activity tab. Stored/transferred as TurnRow (JSON strings for
 * toolCalls and usage).
 */
export interface TurnRecord {
	id:           string;
	chatId:       string | null;
	createdAt:    string;
	finishedAt:   string;
	durationMs:   number;
	model:        string;
	llmBaseUrl:   string;
	userText:     string;
	viewHint:     string | null;
	finalText:    string;
	status:       TurnStatus;
	steps:        number;
	toolCalls:    TurnToolCall[];
	usage:        TurnUsage;
	errorMessage: string | null;
}

// ─── Serialisation helpers ──────────────────────────────────────────────

function recordToRow(t: TurnRecord): TurnRow {
	return {
		id:           t.id,
		chatId:       t.chatId,
		createdAt:    t.createdAt,
		finishedAt:   t.finishedAt,
		durationMs:   t.durationMs,
		model:        t.model,
		llmBaseUrl:   t.llmBaseUrl,
		userText:     t.userText,
		viewHint:     t.viewHint,
		finalText:    t.finalText,
		status:       t.status,
		steps:        t.steps,
		toolCalls:    JSON.stringify(t.toolCalls),
		usage:        JSON.stringify(t.usage),
		errorMessage: t.errorMessage,
	};
}

function rowToRecord(r: TurnRow): TurnRecord {
	let toolCalls: TurnToolCall[] = [];
	let usage: TurnUsage         = { promptTokens: 0, completionTokens: 0, totalTokens: 0 };
	try { toolCalls = JSON.parse(r.toolCalls) as TurnToolCall[]; } catch { /* ok */ }
	try { usage     = JSON.parse(r.usage)     as TurnUsage;      } catch { /* ok */ }
	return {
		id:           r.id,
		chatId:       r.chatId,
		createdAt:    r.createdAt,
		finishedAt:   r.finishedAt,
		durationMs:   r.durationMs,
		model:        r.model,
		llmBaseUrl:   r.llmBaseUrl,
		userText:     r.userText,
		viewHint:     r.viewHint,
		finalText:    r.finalText,
		status:       r.status as TurnStatus,
		steps:        r.steps,
		toolCalls,
		usage,
		errorMessage: r.errorMessage,
	};
}

// ─── Pub/sub ────────────────────────────────────────────────────────────

type Listener = () => void;
const listeners = new Set<Listener>();
function notify(): void { for (const fn of listeners) fn(); }

// ─── Public API ───────────────────────────────────────────────────────

export async function saveTurn(turn: TurnRecord): Promise<void> {
	await rpc.saveTurn(recordToRow(turn));
	notify();
}

export async function loadTurn(id: string): Promise<TurnRecord | undefined> {
	const { turn } = await rpc.loadTurn({ id });
	return turn ? rowToRecord(turn) : undefined;
}

export async function deleteTurn(_id: string): Promise<void> {
	// Not exposed in RPC yet — no UI for it currently.
	notify();
}

export async function loadTurns(): Promise<TurnRecord[]> {
	const { turns } = await rpc.listTurns({});
	return turns.map(rowToRecord);
}

export function useTurns(): {
	turns:   TurnRecord[];
	loaded:  boolean;
	refresh: () => Promise<void>;
} {
	const [turns,  setTurns]  = useState<TurnRecord[]>([]);
	const [loaded, setLoaded] = useState(false);

	const refresh = useCallback(async () => {
		const next = await loadTurns();
		setTurns(next);
		setLoaded(true);
	}, []);

	useEffect(() => {
		void refresh();
		const fn: Listener = () => { void refresh(); };
		listeners.add(fn);
		return () => { listeners.delete(fn); };
	}, [refresh]);

	return { turns, loaded, refresh };
}
