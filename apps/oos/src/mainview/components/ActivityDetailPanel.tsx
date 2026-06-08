// ActivityDetailPanel.tsx — Tab body inspecting one persisted turn.
//
// The complement to ActivityListPanel. Where the list is the
// triage view, this is the deep-dive: every tool call expanded,
// every argument and result rendered as a JSON code block, plus
// a one-click "copy as JSON" so an admin can paste the entire
// turn record into a support ticket.
//
// Layout, top to bottom:
//
//   - Header with status badge, model, timestamp, copy button
//   - Vital stats: duration, tokens, steps, tool count
//   - User question (verbatim) and final assistant text
//   - Optional error message when status === "error"
//   - Tool-call timeline: one card per tool invocation, in
//     execution order, with args/result expandable
//
// We deliberately render arguments and results unfiltered. The
// admin needs to see exactly what went over the wire — that is
// the whole reason this view exists. If a future privacy mode
// requires redaction, gate it behind an explicit toggle rather
// than scrubbing here.

import { useEffect, useState } from "react";

import {
	Accordion,
	ActionIcon,
	Badge,
	Box,
	Button,
	Center,
	Code,
	CopyButton,
	Group,
	Loader,
	Paper,
	ScrollArea,
	Stack,
	Text,
	Tooltip,
} from "@mantine/core";
import {
	IconAlertCircle,
	IconCheck,
	IconCopy,
	IconX,
} from "@tabler/icons-react";

import {
	loadTurn,
	type TurnRecord,
	type TurnStatus,
	type TurnToolCall,
} from "../store/turns";
import { MarkdownView } from "./MarkdownView";
import { PipelineStepTimeline } from "./PipelineStepTimeline";

interface ActivityDetailPanelProps {
	turnId: string;
}

// ─── Public component ────────────────────────────────────────────────

export function ActivityDetailPanel({ turnId }: ActivityDetailPanelProps) {
	const [turn,   setTurn]   = useState<TurnRecord | undefined>(undefined);
	const [loaded, setLoaded] = useState(false);

	useEffect(() => {
		let cancelled = false;
		void (async () => {
			const row = await loadTurn(turnId);
			if (cancelled) return;
			setTurn(row);
			setLoaded(true);
		})();
		return () => {
			cancelled = true;
		};
	}, [turnId]);

	if (!loaded) {
		return (
			<Center style={{ height: "100%" }}>
				<Loader type="dots" size="sm" />
			</Center>
		);
	}

	if (!turn) {
		return (
			<Center style={{ height: "100%" }} p="lg">
				<Stack align="center" gap="xs">
					<IconAlertCircle size={28} color="var(--mantine-color-gray-5)" />
					<Text size="sm" c="dimmed">
						Turn nicht gefunden.
					</Text>
					<Text size="xs" c="dimmed" ta="center">
						Möglicherweise wurde er gelöscht oder die Datenbank ist neu.
					</Text>
				</Stack>
			</Center>
		);
	}

	return (
		<ScrollArea style={{ height: "100%" }} offsetScrollbars scrollbarSize={6}>
			<Stack p="lg" gap="md">
				<HeaderRow turn={turn} />
				<VitalsRow turn={turn} />
				<MessageBlock title="Frage des Nutzers" body={turn.userText} />
				<MessageBlock
					title="Antwort des Assistenten"
					body={turn.finalText || "(leer)"}
					markdown
				/>
				{turn.errorMessage && (
					<ErrorBlock message={turn.errorMessage} />
				)}
				{turn.viewHint === "pipeline_run" ? (
					<PipelineStepTimeline turnId={turn.id} />
				) : (
					<ToolCallTimeline calls={turn.toolCalls} />
				)}
			</Stack>
		</ScrollArea>
	);
}

// ─── Header ──────────────────────────────────────────────────────────

function HeaderRow({ turn }: { turn: TurnRecord }) {
	const json = JSON.stringify(turn, null, 2);
	return (
		<Group justify="space-between" wrap="nowrap" align="flex-start">
			<Stack gap={4} style={{ minWidth: 0, flex: 1 }}>
				<Group gap="xs">
					<StatusBadge status={turn.status} />
					<Text size="sm" fw={500} ff="monospace">
						{turn.model}
					</Text>
					<Text size="xs" c="dimmed">
						{new Date(turn.createdAt).toLocaleString("de-DE")}
					</Text>
				</Group>
				<Text size="xs" c="dimmed" ff="monospace">
					{turn.id}
				</Text>
			</Stack>
			<CopyButton value={json} timeout={2000}>
				{({ copied, copy }) => (
					<Tooltip
						label={copied ? "Kopiert" : "Als JSON für Support kopieren"}
						withArrow
					>
						<Button
							size="xs"
							variant={copied ? "filled" : "light"}
							color={copied ? "teal" : "blue"}
							leftSection={
								copied ? <IconCheck size={14} /> : <IconCopy size={14} />
							}
							onClick={copy}
						>
							{copied ? "Kopiert" : "Als JSON kopieren"}
						</Button>
					</Tooltip>
				)}
			</CopyButton>
		</Group>
	);
}

// ─── Vital stats ─────────────────────────────────────────────────────

function VitalsRow({ turn }: { turn: TurnRecord }) {
	return (
		<Group gap="md" wrap="wrap">
			<Vital label="Dauer"     value={formatDuration(turn.durationMs)} />
			<Vital label="Steps"     value={String(turn.steps)} />
			<Vital label="Tokens"    value={formatUsage(turn)} />
			<Vital label="Tool-Calls" value={String(turn.toolCalls.length)} />
			{turn.viewHint && (
				<Vital label="View-Hint" value={turn.viewHint} mono />
			)}
			{turn.chatId && (
				<Vital label="Chat" value={shortId(turn.chatId)} mono />
			)}
		</Group>
	);
}

interface VitalProps {
	label: string;
	value: string;
	mono?: boolean;
}

function Vital({ label, value, mono }: VitalProps) {
	return (
		<Stack gap={0}>
			<Text size="xs" c="dimmed" tt="uppercase" fw={600}>
				{label}
			</Text>
			<Text size="sm" ff={mono ? "monospace" : undefined}>
				{value}
			</Text>
		</Stack>
	);
}

// ─── Message blocks ──────────────────────────────────────────────────

/**
 * MessageBlock renders one labelled prose block. When `markdown`
 * is set the body is run through MarkdownView, so LLM answers come
 * out with headings, bold, lists and tables. Plain text (e.g. the
 * user's verbatim question) keeps pre-wrap so manual line breaks
 * survive.
 */
function MessageBlock({
	title,
	body,
	markdown = false,
}: {
	title:     string;
	body:      string;
	markdown?: boolean;
}) {
	return (
		<Stack gap={4}>
			<Text size="xs" c="dimmed" tt="uppercase" fw={600}>
				{title}
			</Text>
			<Paper p="sm" withBorder radius="sm" bg="light-dark(var(--mantine-color-gray-0), var(--mantine-color-dark-7))">
				{markdown ? (
					<MarkdownView source={body} />
				) : (
					<Text size="sm" style={{ whiteSpace: "pre-wrap" }}>
						{body}
					</Text>
				)}
			</Paper>
		</Stack>
	);
}

function ErrorBlock({ message }: { message: string }) {
	return (
		<Stack gap={4}>
			<Group gap={6}>
				<IconAlertCircle size={14} color="var(--mantine-color-red-6)" />
				<Text size="xs" c="red.7" tt="uppercase" fw={600}>
					Fehler
				</Text>
			</Group>
			<Paper
				p="sm"
				withBorder
				radius="sm"
				bg="var(--mantine-color-red-0)"
				style={{ borderColor: "var(--mantine-color-red-3)" }}
			>
				<Text size="sm" c="red.9" ff="monospace" style={{ whiteSpace: "pre-wrap" }}>
					{message}
				</Text>
			</Paper>
		</Stack>
	);
}

// ─── Tool-call timeline ──────────────────────────────────────────────

function ToolCallTimeline({ calls }: { calls: TurnToolCall[] }) {
	if (calls.length === 0) {
		return (
			<Stack gap={4}>
				<Text size="xs" c="dimmed" tt="uppercase" fw={600}>
					Tool-Calls
				</Text>
				<Text size="sm" c="dimmed">
					Keine Tool-Aufrufe in diesem Turn.
				</Text>
			</Stack>
		);
	}

	return (
		<Stack gap={4}>
			<Text size="xs" c="dimmed" tt="uppercase" fw={600}>
				Tool-Calls ({calls.length})
			</Text>
			<Accordion
				multiple
				variant="separated"
				radius="sm"
				chevronPosition="right"
			>
				{calls.map((call, idx) => (
					<Accordion.Item key={call.callId} value={call.callId}>
						<Accordion.Control>
							<Group gap="xs" wrap="nowrap">
								<Text size="xs" c="dimmed" ff="monospace" w={20}>
									#{idx + 1}
								</Text>
								<OkIcon ok={call.ok} />
								<Text size="sm" fw={500} ff="monospace">
									{call.name}
								</Text>
								<Text size="xs" c="dimmed" ff="monospace">
									{formatDuration(call.durationMs)}
								</Text>
							</Group>
						</Accordion.Control>
						<Accordion.Panel>
							<Stack gap="xs">
								<JsonBlock label="Argumente" value={call.args} />
								<JsonBlock label="Ergebnis"   value={call.result} />
							</Stack>
						</Accordion.Panel>
					</Accordion.Item>
				))}
			</Accordion>
		</Stack>
	);
}

function OkIcon({ ok }: { ok: boolean }) {
	return ok ? (
		<IconCheck size={14} color="var(--mantine-color-teal-6)" />
	) : (
		<IconX size={14} color="var(--mantine-color-red-6)" />
	);
}

interface JsonBlockProps {
	label: string;
	value: unknown;
}

/**
 * JsonBlock pretty-prints any JSON-able value. Falls back to
 * String() for things that cannot be serialised (cycles, undefined),
 * which should be rare for tool args/results but not impossible
 * — defensive code stays out of error states.
 */
function JsonBlock({ label, value }: JsonBlockProps) {
	let body: string;
	try {
		body = JSON.stringify(value, null, 2);
	} catch (e) {
		body = `<unserialisable: ${(e as Error).message}>`;
	}

	return (
		<Stack gap={4}>
			<Group justify="space-between" wrap="nowrap">
				<Text size="xs" c="dimmed" tt="uppercase" fw={600}>
					{label}
				</Text>
				<CopyButton value={body} timeout={1500}>
					{({ copied, copy }) => (
						<ActionIcon
							size="xs"
							variant="subtle"
							color={copied ? "teal" : "gray"}
							onClick={copy}
							aria-label={`${label} kopieren`}
						>
							{copied ? <IconCheck size={12} /> : <IconCopy size={12} />}
						</ActionIcon>
					)}
				</CopyButton>
			</Group>
			<Code
				block
				style={{
					fontSize: 12,
					maxHeight: 320,
					overflow: "auto",
					whiteSpace: "pre",
				}}
			>
				{body}
			</Code>
		</Stack>
	);
}

// ─── Status badge (mirrors ActivityListPanel) ────────────────────────

function StatusBadge({ status }: { status: TurnStatus }) {
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

// ─── Helpers ─────────────────────────────────────────────────────────

function formatDuration(ms: number): string {
	if (!Number.isFinite(ms) || ms < 0) return "—";
	if (ms < 1000)  return `${ms}ms`;
	if (ms < 10_000) return `${(ms / 1000).toFixed(1)}s`;
	if (ms < 60_000) return `${Math.round(ms / 1000)}s`;
	const min = Math.floor(ms / 60_000);
	const sec = Math.round((ms % 60_000) / 1000);
	return `${min}m${sec.toString().padStart(2, "0")}s`;
}

function formatUsage(turn: TurnRecord): string {
	const u = turn.usage;
	if (u.totalTokens === 0) return "—";
	return `${u.promptTokens}+${u.completionTokens} = ${u.totalTokens}`;
}

function shortId(id: string): string {
	if (id.length <= 16) return id;
	return `…${id.slice(-12)}`;
}
