// ChatHistoryPanel.tsx — Tab body listing every saved chat.
//
// Replaces the previous left-edge drawer. As a regular tab the
// history is a parallel workspace: the user can keep it open while
// browsing results, drag entries into the composer, and dismiss
// the tab when no longer needed. Reopened any time via the burger
// menu or Cmd+K on the chat input.
//
// Three actions per row:
//
//   - Click anywhere on the row body → load the chat. Publishes
//     `{ kind: "load", chatId }` on chat-events; App.tsx swaps it
//     into the live conversation.
//   - Drag the row → starts a drag carrying the chat's first user
//     message as plain text. The chat composer's Textarea is the
//     drop target; dropping replaces the draft.
//   - Click the trash icon → publishes `{ kind: "delete", chatId }`.
//     The icon stops propagation so deleting does not also fire
//     the row's load handler.
//
// On top of the row list there is now:
//
//   - A search field. Case-insensitive substring match against the
//     chat title. Empty input shows everything; the search itself
//     is purely a client-side filter, so typing is instant.
//   - Title-based deduplication. The chat history easily fills
//     with near-identical titles ("zeige mir alle personen") when
//     the user iterates on a prompt; rendering each one as its
//     own row drowns the genuinely different chats. We collapse
//     rows whose normalised title (lowercase, collapsed
//     whitespace) is identical and show a small counter on the
//     group. Click and drag operate on the most recent member.
//     Trash deletes every member of the group, with a confirm
//     dialog when the group has more than one entry.
//
// Empty state mirrors the old drawer so first-time users see the
// hint about chats appearing here once they ask something.
//
// "Neuer Chat" button at the top is gone: the equivalent action
// lives in the burger menu (or the user just sends a message in
// an empty composer). The history tab is purely about reading and
// reusing existing conversations.

import { useMemo, useState } from "react";

import {
	ActionIcon,
	Badge,
	Box,
	Center,
	Group,
	ScrollArea,
	Stack,
	Text,
	TextInput,
	UnstyledButton,
} from "@mantine/core";
import { modals } from "@mantine/modals";
import { IconSearch, IconTrash } from "@tabler/icons-react";

import { publish as publishChatEvent } from "../chat-events";
import { useChats, type ChatSummary, loadChat } from "../store/chats";

// ─── Public component ────────────────────────────────────────────────

export function ChatHistoryPanel() {
	const { chats, loaded } = useChats();
	const [query, setQuery] = useState("");

	// Group rows by normalised title. The grouping is recomputed
	// on every render but the list is short (chat history rarely
	// reaches the thousands) and the work is linear, so doing it
	// inside useMemo against `chats` is more than fast enough.
	const groups = useMemo(() => groupByTitle(chats), [chats]);

	// Filter on the trimmed lowercase query against each group's
	// display title. Substring match; no fancy ranking. If the
	// user wants ranking later we can swap in a tiny scorer.
	const filtered = useMemo(() => {
		const needle = query.trim().toLowerCase();
		if (!needle) return groups;
		return groups.filter((g) => g.title.toLowerCase().includes(needle));
	}, [groups, query]);

	if (loaded && chats.length === 0) {
		return (
			<Box p="lg" style={{ height: "100%" }}>
				<EmptyState />
			</Box>
		);
	}

	return (
		<Stack
			gap={0}
			style={{ height: "100%", display: "flex", flexDirection: "column" }}
		>
			<Box px="md" pt="md" pb="xs">
				<TextInput
					value={query}
					onChange={(e) => setQuery(e.currentTarget.value)}
					placeholder="Chats durchsuchen…"
					leftSection={<IconSearch size={14} />}
					size="sm"
					aria-label="Chat history search"
				/>
			</Box>

			{filtered.length === 0 ? (
				<Center style={{ flex: 1 }}>
					<Text size="sm" c="dimmed">
						Keine Chats gefunden.
					</Text>
				</Center>
			) : (
				<ScrollArea
					style={{ flex: 1, minHeight: 0 }}
					offsetScrollbars
					scrollbarSize={6}
				>
					<Stack gap={4} px="md" pb="md">
						{filtered.map((group) => (
							<ChatRow key={group.key} group={group} />
						))}
					</Stack>
				</ScrollArea>
			)}
		</Stack>
	);
}

// ─── Grouping ────────────────────────────────────────────────────────

/**
 * ChatGroup is one entry in the rendered list. When the chat
 * history contains several chats with the same normalised title,
 * they collapse into a single group; `members` carries every chat
 * in the group, newest first. `title` and `updatedAt` reflect the
 * newest member so the row shows the most recent variant.
 */
interface ChatGroup {
	key:       string;          // normalised title — stable across renders
	title:     string;          // display title (newest member's exact title)
	updatedAt: string;          // newest member's updatedAt, for the timestamp
	members:   ChatSummary[];   // newest first
}

/**
 * groupByTitle collapses chats with the same normalised title into
 * one group. The input is already in reverse-chronological order
 * (loadSummaries sorts by updatedAt desc), so the first chat we
 * see for a key is the newest — we just push subsequent ones onto
 * the same group.
 *
 * Normalisation: lowercase, trim, collapse internal whitespace.
 * That catches "zeige mir alle personen" vs "Zeige mir alle
 * Personen" vs "Zeige  mir  alle Personen" — all the same chat
 * to a human, all distinct rows in the database.
 */
function groupByTitle(chats: readonly ChatSummary[]): ChatGroup[] {
	const out: ChatGroup[] = [];
	const byKey = new Map<string, ChatGroup>();
	for (const chat of chats) {
		const key = normaliseTitle(chat.title);
		const existing = byKey.get(key);
		if (existing) {
			existing.members.push(chat);
			continue;
		}
		const group: ChatGroup = {
			key,
			title:     chat.title,
			updatedAt: chat.updatedAt,
			members:   [chat],
		};
		byKey.set(key, group);
		out.push(group);
	}
	return out;
}

/**
 * normaliseTitle prepares the dedup key. Cheap, deterministic, and
 * reversible enough for debugging — paste the key into a column
 * search and you'll find the original. We deliberately keep
 * punctuation: "Personen aus Berlin?" and "Personen aus Berlin"
 * are different intents and should stay separate.
 */
function normaliseTitle(title: string): string {
	return title.trim().toLowerCase().replace(/\s+/g, " ");
}

// ─── One row in the list ─────────────────────────────────────────────

interface ChatRowProps {
	group: ChatGroup;
}

/**
 * ChatRow renders one row, possibly representing several chats
 * with the same title. The body is an UnstyledButton so the whole
 * row is clickable; a smaller trash ActionIcon sits to the right
 * and stops propagation so deleting does not also load the chat
 * we're trying to remove.
 *
 * The row is `draggable`. On dragstart it asynchronously fetches
 * the newest chat in the group, picks the first user message, and
 * sets it as `text/plain` on the DataTransfer. Asynchronous because
 * the summaries we render here intentionally skip the messages
 * field — we only pay for the full payload when the user actually
 * drags one row.
 *
 * Click and drag operate on the newest member of the group; trash
 * deletes every member, with a confirm dialog when the group has
 * more than one entry so a stray click can't wipe out a bunch of
 * variants at once.
 */
function ChatRow({ group }: ChatRowProps) {
	const newest = group.members[0]!;
	const count  = group.members.length;

	const handleClick = () => {
		publishChatEvent({ kind: "load", chatId: newest.id });
	};

	const handleDelete = (e: React.MouseEvent) => {
		e.stopPropagation();
		if (count === 1) {
			publishChatEvent({ kind: "delete", chatId: newest.id });
			return;
		}
		modals.openConfirmModal({
			title:    "Mehrere Chats löschen?",
			children: (
				<Text size="sm">
					Diese Gruppe enthält {count} Chats mit demselben Titel.
					Sollen alle gelöscht werden?
				</Text>
			),
			labels:   { confirm: "Alle löschen", cancel: "Abbrechen" },
			confirmProps: { color: "red" },
			onConfirm: () => {
				for (const m of group.members) {
					publishChatEvent({ kind: "delete", chatId: m.id });
				}
			},
		});
	};

	const handleDragStart = (e: React.DragEvent<HTMLDivElement>) => {
		// We can only set DataTransfer synchronously inside the
		// dragstart event. Pre-seed with the title so a drop
		// always carries *something* meaningful, then kick off
		// the full-chat fetch and overwrite if it lands before
		// the drop.
		e.dataTransfer.setData("text/plain", newest.title);
		e.dataTransfer.effectAllowed = "copy";

		// Best-effort upgrade: pull the full chat and replace the
		// payload with the first user message. If the user drops
		// before this resolves, the title is what they get —
		// still better than an empty payload.
		void (async () => {
			const full = await loadChat(newest.id);
			if (!full) return;
			const firstUser = full.messages.find((m) => m.role === "user");
			if (!firstUser || !firstUser.text.trim()) return;
			// `dataTransfer` is read-only after dragstart; we use a
			// small global instead so the drop handler can pick it
			// up. Cleared on drop to avoid leaking across drags.
			pendingDragText = firstUser.text;
		})();
	};

	return (
		<Box
			draggable
			onDragStart={handleDragStart}
			style={{
				borderRadius: 6,
				cursor: "grab",
				transition: "background 80ms ease",
			}}
			onMouseEnter={(e) => {
				e.currentTarget.style.background =
					"light-dark(var(--mantine-color-gray-1), var(--mantine-color-dark-6))";
			}}
			onMouseLeave={(e) => {
				e.currentTarget.style.background = "";
			}}
		>
			<Group gap={4} wrap="nowrap" px="xs" py={6}>
				<UnstyledButton
					onClick={handleClick}
					style={{ flex: 1, minWidth: 0 }}
				>
					<Stack gap={2} style={{ minWidth: 0 }}>
						<Group gap={6} wrap="nowrap">
							<Text
								size="sm"
								fw={500}
								lineClamp={1}
								style={{ wordBreak: "break-word", flex: 1, minWidth: 0 }}
							>
								{group.title}
							</Text>
							{count > 1 && (
								<Badge
									size="xs"
									variant="light"
									color="gray"
									title={`${count} Chats mit gleichem Titel`}
								>
									{count}
								</Badge>
							)}
						</Group>
						<Text size="xs" c="dimmed">
							{formatRelative(group.updatedAt)}
						</Text>
					</Stack>
				</UnstyledButton>
				<ActionIcon
					variant="subtle"
					color="gray"
					size="sm"
					aria-label={count > 1 ? "Chats löschen" : "Chat löschen"}
					onClick={handleDelete}
				>
					<IconTrash size={14} />
				</ActionIcon>
			</Group>
		</Box>
	);
}

// ─── Drag payload pickup ─────────────────────────────────────────────
//
// The row's dragstart handler stashes the chat's first user message
// here when the asynchronous loadChat resolves. The composer's drop
// handler reads it and clears the slot. A simple module-level
// variable is enough — only one drag can be in flight at a time.

let pendingDragText: string | null = null;

/**
 * consumePendingDragText returns the upgraded drag payload (if any)
 * and clears the slot. Called by the chat composer's drop handler
 * — when present, it overrides the plain-text DataTransfer payload
 * (which was set synchronously to the chat title as a fallback).
 */
export function consumePendingDragText(): string | null {
	const v = pendingDragText;
	pendingDragText = null;
	return v;
}

// ─── Empty state ─────────────────────────────────────────────────────

function EmptyState() {
	return (
		<Stack align="center" justify="center" gap="xs" py="xl">
			<Text size="sm" c="dimmed" ta="center">
				Noch keine Unterhaltungen gespeichert.
			</Text>
			<Text size="xs" c="dimmed" ta="center">
				Sobald du eine Frage stellst, taucht der Chat hier auf.
			</Text>
		</Stack>
	);
}

// ─── Helpers ─────────────────────────────────────────────────────────

/**
 * formatRelative renders the timestamp as "vor 5 min" / "gestern" /
 * "12.04." for a chat-list-friendly density. Falls back to the raw
 * ISO date if parsing fails. Lifted from the old drawer; kept here
 * locally so the panel has no leftover dependency on a deleted file.
 */
function formatRelative(iso: string): string {
	const d = new Date(iso);
	if (Number.isNaN(d.getTime())) return iso;

	const now = Date.now();
	const diffSec = Math.round((now - d.getTime()) / 1000);
	if (diffSec < 60)         return "gerade eben";
	if (diffSec < 60 * 60)    return `vor ${Math.round(diffSec / 60)} min`;
	if (diffSec < 60 * 60 * 24) {
		const hours = Math.round(diffSec / 3600);
		return `vor ${hours} ${hours === 1 ? "Stunde" : "Stunden"}`;
	}
	if (diffSec < 60 * 60 * 24 * 2) return "gestern";
	if (diffSec < 60 * 60 * 24 * 7) {
		const days = Math.round(diffSec / (60 * 60 * 24));
		return `vor ${days} Tagen`;
	}
	// Older — drop to a calendar date in DE locale.
	return d.toLocaleDateString("de-DE", {
		day:   "2-digit",
		month: "2-digit",
		year:  d.getFullYear() === new Date().getFullYear() ? undefined : "numeric",
	});
}
