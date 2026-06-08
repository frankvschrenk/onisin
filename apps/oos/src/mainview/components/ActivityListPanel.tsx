// ActivityListPanel.tsx — Tab body listing every persisted turn.
//
// Admin-facing telemetry view. Every chat turn writes one row to
// the `turns` Dexie table; this panel renders them in reverse
// chronological order so the most recent run is at the top. Click
// a row to open a detail tab for full inspection (tool calls,
// arguments, results, JSON export).
//
// The columns are chosen for triage at a glance:
//
//   - Status      — coloured badge so the eye finds errors and
//                   cancelled turns instantly.
//   - Time        — relative for the recent past, absolute when
//                   older than a week. Same formatter as the
//                   chat-history panel for consistency.
//   - Model       — which LLM ran. Useful when comparing models.
//   - Frage       — the user's question, truncated. The full text
//                   is always one click away in the detail tab.
//   - Tokens      — total tokens of the turn. Sum across all
//                   LLM calls of the loop. 0 means the local
//                   server did not return usage info.
//   - Dauer       — wall-clock from send to final answer.
//   - Tools       — count of tool calls in the turn.
//
// Search is a substring match against the user text; the dataset
// is small enough that a simple lower-case includes does the job.

import { useMemo, useState } from "react";

import {
	Badge,
	Box,
	Center,
	Group,
	ScrollArea,
	Stack,
	Table,
	Text,
	TextInput,
	UnstyledButton,
} from "@mantine/core";
import { IconSearch } from "@tabler/icons-react";

import { openActivityDetail } from "../store/tabs";
import { useTurns, type TurnRecord, type TurnStatus } from "../store/turns";

// ─── Public component ────────────────────────────────────────────────

export function ActivityListPanel() {
	const { turns, loaded } = useTurns();
	const [query, setQuery] = useState("");

	const filtered = useMemo(() => {
		const needle = query.trim().toLowerCase();
		if (!needle) return turns;
		return turns.filter((t) => t.userText.toLowerCase().includes(needle));
	}, [turns, query]);

	if (loaded && turns.length === 0) {
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
					placeholder="Aktivität durchsuchen…"
					leftSection={<IconSearch size={14} />}
					size="sm"
					aria-label="Activity search"
				/>
			</Box>

			{filtered.length === 0 ? (
				<Center style={{ flex: 1 }}>
					<Text size="sm" c="dimmed">
						Kein Eintrag gefunden.
					</Text>
				</Center>
			) : (
				<ScrollArea
					style={{ flex: 1, minHeight: 0 }}
					offsetScrollbars
					scrollbarSize={6}
				>
					<Box px="md" pb="md">
						<Table
							verticalSpacing="xs"
							horizontalSpacing="xs"
							highlightOnHover
							withRowBorders
						>
							<Table.Thead>
								<Table.Tr>
									<Table.Th style={{ width: 90 }}>Status</Table.Th>
									<Table.Th style={{ width: 120 }}>Zeit</Table.Th>
									<Table.Th style={{ width: 140 }}>Modell</Table.Th>
									<Table.Th>Frage</Table.Th>
									<Table.Th style={{ width: 90, textAlign: "right" }}>Tokens</Table.Th>
									<Table.Th style={{ width: 70, textAlign: "right" }}>Dauer</Table.Th>
									<Table.Th style={{ width: 60, textAlign: "right" }}>Tools</Table.Th>
								</Table.Tr>
							</Table.Thead>
							<Table.Tbody>
								{filtered.map((turn) => (
									<TurnRow key={turn.id} turn={turn} />
								))}
							</Table.Tbody>
						</Table>
					</Box>
				</ScrollArea>
			)}
		</Stack>
	);
}

// ─── One row ─────────────────────────────────────────────────────────

interface TurnRowProps {
	turn: TurnRecord;
}

/**
 * TurnRow renders one persisted turn. The whole row is clickable —
 * an UnstyledButton wraps each cell so a click anywhere in the row
 * dispatches openActivityDetail. We use Box-with-onClick instead of
 * one big UnstyledButton because the cells must stay table cells
 * for the column alignment to work.
 */
function TurnRow({ turn }: TurnRowProps) {
	const handleClick = () => openActivityDetail(turn.id);

	return (
		<Table.Tr
			style={{ cursor: "pointer" }}
			onClick={handleClick}
		>
			<Table.Td>
				<StatusBadge status={turn.status} />
			</Table.Td>
			<Table.Td>
				<Text size="xs" c="dimmed" title={turn.createdAt}>
					{formatRelative(turn.createdAt)}
				</Text>
			</Table.Td>
			<Table.Td>
				<Text size="xs" lineClamp={1} title={turn.model}>
					{turn.model}
				</Text>
			</Table.Td>
			<Table.Td>
				<Text size="sm" lineClamp={1} title={turn.userText}>
					{turn.userText}
				</Text>
			</Table.Td>
			<Table.Td style={{ textAlign: "right" }}>
				<Text
					size="xs"
					c={turn.usage.totalTokens === 0 ? "dimmed" : undefined}
					ff="monospace"
				>
					{turn.usage.totalTokens || "—"}
				</Text>
			</Table.Td>
			<Table.Td style={{ textAlign: "right" }}>
				<Text size="xs" ff="monospace">
					{formatDuration(turn.durationMs)}
				</Text>
			</Table.Td>
			<Table.Td style={{ textAlign: "right" }}>
				<Text size="xs" ff="monospace">
					{turn.toolCalls.length || "—"}
				</Text>
			</Table.Td>
		</Table.Tr>
	);
}

// ─── Status badge ────────────────────────────────────────────────────

interface StatusBadgeProps {
	status: TurnStatus;
}

/**
 * StatusBadge maps the turn status onto a Mantine colour. Errors
 * and cancellations are loud (red, gray); success is quiet (green
 * is reserved for explicit positive outcomes elsewhere — for an
 * append-only log "success" is the boring default, so a teal
 * outline is enough). step_limit is yellow because it's a soft
 * failure: the model gave up but the user can retry with a
 * sharper question.
 */
function StatusBadge({ status }: StatusBadgeProps) {
	const config = STATUS_CONFIG[status];
	return (
		<Badge size="sm" variant={config.variant} color={config.color}>
			{config.label}
		</Badge>
	);
}

const STATUS_CONFIG: Record<TurnStatus, {
	label:   string;
	color:   string;
	variant: "light" | "filled" | "outline";
}> = {
	success:    { label: "OK",         color: "teal",   variant: "light" },
	error:      { label: "Fehler",     color: "red",    variant: "filled" },
	cancelled:  { label: "Abbruch",    color: "gray",   variant: "light" },
	step_limit: { label: "Step-Limit", color: "yellow", variant: "filled" },
};

// ─── Empty state ─────────────────────────────────────────────────────

function EmptyState() {
	return (
		<Stack align="center" justify="center" gap="xs" py="xl">
			<Text size="sm" c="dimmed" ta="center">
				Noch keine Aktivität.
			</Text>
			<Text size="xs" c="dimmed" ta="center">
				Sobald du eine Frage stellst, taucht der Turn hier auf.
			</Text>
		</Stack>
	);
}

// ─── Helpers ─────────────────────────────────────────────────────────

/**
 * formatRelative — same shape as the chat-history panel uses, but
 * a touch more terse here because the activity table is meant to
 * be scanned, not read.
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
		return `vor ${hours}h`;
	}
	if (diffSec < 60 * 60 * 24 * 2) return "gestern";
	if (diffSec < 60 * 60 * 24 * 7) {
		const days = Math.round(diffSec / (60 * 60 * 24));
		return `vor ${days}d`;
	}
	return d.toLocaleDateString("de-DE", {
		day:   "2-digit",
		month: "2-digit",
		year:  d.getFullYear() === new Date().getFullYear() ? undefined : "numeric",
	});
}

/**
 * formatDuration converts milliseconds into a compact human
 * representation. Sub-second durations get "ms" precision; longer
 * durations switch to seconds, then minutes. Goal is fixed-width
 * so the column scans cleanly.
 */
function formatDuration(ms: number): string {
	if (!Number.isFinite(ms) || ms < 0) return "—";
	if (ms < 1000)  return `${ms}ms`;
	if (ms < 10_000) return `${(ms / 1000).toFixed(1)}s`;
	if (ms < 60_000) return `${Math.round(ms / 1000)}s`;
	const min = Math.floor(ms / 60_000);
	const sec = Math.round((ms % 60_000) / 1000);
	return `${min}m${sec.toString().padStart(2, "0")}s`;
}
