// EventResultPanel.tsx — Renderer for an `event_result` tab.
//
// Layout, top to bottom:
//
//   ┌─────────────────────────────────────────┐
//   │  Header: question + meta (model · stream│
//   │          · mapping · hit count)         │
//   ├─────────────────────────────────────────┤
//   │  Markdown answer                        │
//   ├─────────────────────────────────────────┤
//   │  Sources — one card per hit, with the   │
//   │  source label, type, score, text and    │
//   │  any structured metadata folded out     │
//   │  underneath.                            │
//   └─────────────────────────────────────────┘
//
// The whole panel is read-only. Re-asking is done in the chat
// composer, not here. That keeps the result tab immutable: every
// question gets its own tab, scrollable history without surprises.

import {
	Badge,
	Box,
	Card,
	Group,
	ScrollArea,
	Stack,
	Text,
} from "@mantine/core";

import { MarkdownView } from "./MarkdownView";
import type { EventHit } from "../event-types";

interface EventResultPanelProps {
	mapping:  string;
	streamId: string;
	question: string;
	answer:   string;
	hits:     EventHit[];
	model:    string;
}

export function EventResultPanel({
	mapping,
	streamId,
	question,
	answer,
	hits,
	model,
}: EventResultPanelProps) {
	// Render the LLM answer as Markdown via react-markdown — same
	// renderer Bubble and DocsPanel use, so the styling stays
	// consistent across the app.
	const answerSource = answer || "_No answer._";

	return (
		<ScrollArea style={{ height: "100%" }} type="hover">
			<Box p="lg">
				{/* Header: question + meta */}
				<Stack gap="xs" mb="md">
					<Text size="lg" fw={600}>
						{question}
					</Text>
					<Group gap="xs">
						<Badge color="brand" variant="light">
							{mapping}
						</Badge>
						<Badge color="grape" variant="light">
							{streamId || "alle Streams"}
						</Badge>
						<Badge color="gray" variant="light">
							{model}
						</Badge>
						<Badge color="teal" variant="light">
							{hits.length} {hits.length === 1 ? "Treffer" : "Treffer"}
						</Badge>
					</Group>
				</Stack>

				{/* Markdown answer */}
				<Card withBorder p="md" mb="lg" radius="md">
					<MarkdownView source={answerSource} />
				</Card>

				{/* Sources */}
				{hits.length > 0 && (
					<Stack gap="sm">
						<Text size="sm" fw={600} c="dimmed">
							Quellen
						</Text>
						{hits.map((h, i) => (
							<SourceCard key={`${h.sourceId}-${i}`} index={i + 1} hit={h} />
						))}
					</Stack>
				)}
			</Box>
		</ScrollArea>
	);
}

interface SourceCardProps {
	index: number;
	hit:   EventHit;
}

function SourceCard({ index, hit }: SourceCardProps) {
	// Filter out empty / null metadata fields so the card stays
	// focused. JSON.stringify renders nested objects readably.
	const metaEntries = Object.entries(hit.metadata).filter(
		([, v]) => v !== null && v !== undefined && v !== "",
	);

	return (
		<Card withBorder p="sm" radius="md">
			<Stack gap="xs">
				<Group gap="xs" justify="space-between">
					<Group gap="xs">
						<Badge color="blue" variant="filled">
							[{index}]
						</Badge>
						<Text size="sm" fw={600}>
							{hit.eventType}
						</Text>
					</Group>
					<Group gap="xs">
						<Text size="xs" c="dimmed">
							stream: {hit.streamId}
						</Text>
						<Badge size="sm" variant="light" color="gray">
							score {hit.score.toFixed(2)}
						</Badge>
					</Group>
				</Group>
				<Text size="sm">{hit.textContent}</Text>
				{metaEntries.length > 0 && (
					<Box
						style={{
							borderTop: "1px solid var(--mantine-color-gray-3)",
							paddingTop: 8,
						}}
					>
						<Stack gap={2}>
							{metaEntries.map(([k, v]) => (
								<Group key={k} gap="xs">
									<Text size="xs" c="dimmed" style={{ minWidth: 120 }}>
										{k}
									</Text>
									<Text size="xs" style={{ fontFamily: "monospace" }}>
										{formatMetaValue(v)}
									</Text>
								</Group>
							))}
						</Stack>
					</Box>
				)}
			</Stack>
		</Card>
	);
}

function formatMetaValue(v: unknown): string {
	if (typeof v === "string")  return v;
	if (typeof v === "number" || typeof v === "boolean") return String(v);
	try {
		return JSON.stringify(v);
	} catch {
		return String(v);
	}
}
