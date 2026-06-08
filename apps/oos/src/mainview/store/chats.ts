// store/chats.ts — Persistent chat history via bun:sqlite (RPC).
//
// Bun owns the SQLite file; the renderer calls rpc.saveChat /
// loadChat / deleteChat / listChats. Pub/sub is in-memory as before.

import { useCallback, useEffect, useState } from "react";

import type { ChatMessage } from "../types";
import { rpc } from "../rpc";

/** A persisted chat — primary key plus its scrollback. */
export interface PersistedChat {
	id:        string;
	title:     string;
	createdAt: string;
	updatedAt: string;
	messages:  ChatMessage[];
}

/** Compact summary used by the chat-list drawer. */
export interface ChatSummary {
	id:        string;
	title:     string;
	updatedAt: string;
}

// ─── Pub/sub ────────────────────────────────────────────────────────────

type Listener = () => void;
const listeners = new Set<Listener>();

function notify(): void {
	for (const fn of listeners) fn();
}

// ─── Public API ───────────────────────────────────────────────────────

export function createChatId(): string {
	const ts   = new Date().toISOString().replace(/[:.]/g, "-").replace(/Z$/, "");
	const rand = Math.random().toString(36).slice(2, 6);
	return `chat_${ts}_${rand}`;
}

export function deriveTitle(messages: ChatMessage[]): string {
	for (const m of messages) {
		if (m.role !== "user") continue;
		const trimmed = m.text.trim();
		if (!trimmed) continue;
		return trimmed.length > 60 ? `${trimmed.slice(0, 60)}…` : trimmed;
	}
	return "Neue Unterhaltung";
}

export async function saveChat(chat: PersistedChat): Promise<void> {
	await rpc.saveChat({
		id:        chat.id,
		title:     chat.title,
		createdAt: chat.createdAt,
		updatedAt: chat.updatedAt,
		messages:  JSON.stringify(chat.messages),
	});
	notify();
}

export async function loadChat(id: string): Promise<PersistedChat | undefined> {
	const { chat } = await rpc.loadChat({ id });
	if (!chat) return undefined;
	return {
		id:        chat.id,
		title:     chat.title,
		createdAt: chat.createdAt,
		updatedAt: chat.updatedAt,
		messages:  parseMessages(chat.messages),
	};
}

export async function deleteChat(id: string): Promise<void> {
	await rpc.deleteChat({ id });
	notify();
}

export async function loadSummaries(): Promise<ChatSummary[]> {
	const { chats } = await rpc.listChats({});
	return chats;
}

export function useChats(): {
	chats:   ChatSummary[];
	loaded:  boolean;
	refresh: () => Promise<void>;
} {
	const [chats,  setChats]  = useState<ChatSummary[]>([]);
	const [loaded, setLoaded] = useState(false);

	const refresh = useCallback(async () => {
		const next = await loadSummaries();
		setChats(next);
		setLoaded(true);
	}, []);

	useEffect(() => {
		void refresh();
		const fn: Listener = () => { void refresh(); };
		listeners.add(fn);
		return () => { listeners.delete(fn); };
	}, [refresh]);

	return { chats, loaded, refresh };
}

// ─── Internals ───────────────────────────────────────────────────────────

function parseMessages(raw: unknown): ChatMessage[] {
	const arr = typeof raw === "string" ? (() => { try { return JSON.parse(raw); } catch { return []; } })() : raw;
	if (!Array.isArray(arr)) return [];
	const out: ChatMessage[] = [];
	for (const item of arr) {
		if (!item || typeof item !== "object") continue;
		const r    = item as Record<string, unknown>;
		const role = r.role;
		if (role !== "user" && role !== "assistant" && role !== "tool") continue;
		const text = typeof r.text === "string" ? r.text : "";
		const id   = typeof r.id   === "string" ? r.id   : `restored_${out.length}`;
		const ts   = typeof r.ts   === "string" ? r.ts   : "";
		const msg: ChatMessage = { id, role, text, ts };
		if (typeof r.turnId     === "string")  msg.turnId     = r.turnId;
		if (typeof r.model      === "string")  msg.model      = r.model;
		if (typeof r.durationMs === "number") msg.durationMs = r.durationMs;
		if (r.usage && typeof r.usage === "object") {
			const u = r.usage as Record<string, unknown>;
			if (typeof u.promptTokens === "number" && typeof u.completionTokens === "number" && typeof u.totalTokens === "number") {
				msg.usage = { promptTokens: u.promptTokens, completionTokens: u.completionTokens, totalTokens: u.totalTokens };
			}
		}
		out.push(msg);
	}
	return out;
}
