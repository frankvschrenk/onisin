// Bubble.tsx — One chat message rendered as a styled card.
//
// Three visual variants by role:
//   user      — accent-tinted, right-leaning header.
//   assistant — surface card with markdown body. While the turn
//               is still running, useChat keeps the bubble's text
//               at the sentinel "…" — we render Mantine's dots
//               loader in that case so the user sees that the
//               assistant is working.
//   tool      — slim mono card; for tool-call summaries inline in
//               the conversation. Not used in the seed data yet.
//
// Markdown rendering routes through MarkdownView so the look matches
// other panels that display LLM output (event answers, pipeline chunks,
// activity details). GFM stays on (tables, task lists, strikethrough).

import { Box, Group, Loader, Paper, Text, UnstyledButton } from "@mantine/core";

import { MarkdownReadView } from "./MarkdownReadView";
import { openActivityDetail } from "../store/tabs";
import type { ChatMessage } from "../types";

interface BubbleProps {
	message: ChatMessage;
}

// Sentinel that useChat writes into the placeholder bubble for as
// long as a turn is in flight. Kept in sync with hooks/useChat.ts.
const PENDING_SENTINEL = "…";

export function Bubble({ message }: BubbleProps) {
	const isUser = message.role === "user";
	const isTool = message.role === "tool";
	const isPending =
		message.role === "assistant" && message.text === PENDING_SENTINEL;

	const bg = isUser
		? "var(--mantine-color-brand-0)"
		: isTool
			? "light-dark(var(--mantine-color-gray-0), var(--mantine-color-dark-7))"
			: "light-dark(var(--mantine-color-gray-1), var(--mantine-color-dark-6))";

	const roleLabel = isUser
		? "Du"
		: isTool
			? "Tool"
			: "Assistant";

	return (
		<Paper
			withBorder={false}
			radius="md"
			p="sm"
			style={{
				background: bg,
				alignSelf: isUser ? "flex-end" : "flex-start",
				maxWidth: "92%",
			}}
		>
			<Group justify="space-between" mb={4} gap="xs" wrap="nowrap">
				<Text size="xs" fw={600}>
					{roleLabel}
				</Text>
				<Text size="xs" c="dimmed">
					{message.ts}
				</Text>
			</Group>
			<Box className="bubble-body">
				{isPending ? (
					<Loader type="dots" size="sm" color="gray" />
				) : (
					<MarkdownReadView source={message.text} />
				)}
			</Box>
			{!isPending && message.role === "assistant" && message.turnId && (
				<TelemetryFooter message={message} />
			)}
		</Paper>
	);
}

/**
 * TelemetryFooter renders the dim "model · tokens · duration" line
 * under an assistant bubble once the turn has finished. The whole
 * footer is a button — clicking opens the corresponding Activity
 * detail tab so admins can drill in without leaving the chat.
 *
 * The footer omits silently when usage.totalTokens is 0 (some local
 * servers don't return usage info). The duration alone is still
 * useful, so we render whatever we have.
 */
function TelemetryFooter({ message }: { message: ChatMessage }) {
	const turnId = message.turnId!;
	const parts: string[] = [];
	if (message.model) parts.push(message.model);
	if (message.usage && message.usage.totalTokens > 0) {
		parts.push(`${message.usage.totalTokens} tok`);
	}
	if (typeof message.durationMs === "number") {
		parts.push(formatDuration(message.durationMs));
	}
	if (parts.length === 0) return null;

	return (
		<UnstyledButton
			onClick={() => openActivityDetail(turnId)}
			style={{ marginTop: 6, display: "block" }}
			aria-label="Turn-Details öffnen"
		>
			<Text size="xs" c="dimmed">
				{parts.join(" · ")}
			</Text>
		</UnstyledButton>
	);
}

/**
 * formatDuration is intentionally duplicated here (also lives in
 * the activity panels) to keep the bubble file self-contained —
 * three callers pulling in a shared util just for this one
 * one-liner would be more weight than the duplication. If a
 * fourth caller appears, factor it out.
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
