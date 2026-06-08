// PipelineStepTimeline.tsx — Per-step inspector for a pipeline_run turn.
//
// Rendered inside ActivityDetailPanel when the turn's view_hint is
// "pipeline_run". Loads the rows persisted by the runner and shows
// each step as an accordion item: kind + name in the header,
// chunks + usage + duration in the body.
//
// Chunks are rendered through MarkdownView at "xs" density so the
// LLM output reads naturally (headings, bold, lists) while still
// being faithful to what the model produced. The verbatim source
// stays one click away via the per-chunk copy button.

import {
	Accordion,
	ActionIcon,
	Badge,
	Box,
	Center,
	CopyButton,
	Group,
	Loader,
	Stack,
	Text,
} from "@mantine/core";
import { IconCheck, IconCopy } from "@tabler/icons-react";

import { MarkdownView } from "./MarkdownView";
import {
	useTurnPipelineSteps,
	type PipelineStepRecord,
} from "../store/pipeline-steps";

interface PipelineStepTimelineProps {
	turnId: string;
}

export function PipelineStepTimeline({ turnId }: PipelineStepTimelineProps) {
	const { steps, loaded } = useTurnPipelineSteps(turnId);

	if (!loaded) {
		return (
			<Center py="sm">
				<Loader type="dots" size="xs" />
			</Center>
		);
	}

	if (steps.length === 0) {
		return (
			<Stack gap={4}>
				<SectionTitle label="Pipeline-Schritte" />
				<Text size="sm" c="dimmed">
					Keine Schritt-Daten gespeichert.
				</Text>
			</Stack>
		);
	}

	return (
		<Stack gap={4}>
			<SectionTitle label={`Pipeline-Schritte (${steps.length})`} />
			<Accordion
				multiple
				variant="separated"
				radius="sm"
				chevronPosition="right"
			>
				{steps.map(s => (
					<Accordion.Item key={s.stepIndex} value={String(s.stepIndex)}>
						<Accordion.Control>
							<StepHeader step={s} />
						</Accordion.Control>
						<Accordion.Panel>
							<StepBody step={s} />
						</Accordion.Panel>
					</Accordion.Item>
				))}
			</Accordion>
		</Stack>
	);
}

// ─── Step header (Accordion.Control) ───────────────────────────

function StepHeader({ step }: { step: PipelineStepRecord }) {
	return (
		<Group gap="xs" wrap="nowrap">
			<Text size="xs" c="dimmed" ff="monospace" w={24}>
				#{step.stepIndex + 1}
			</Text>
			<KindBadge kind={step.stepKind} />
			<Text size="sm" fw={500} ff="monospace">
				{step.stepName}
			</Text>
			<Text size="xs" c="dimmed" style={{ flex: 1, minWidth: 0 }} truncate="end">
				{step.summary}
			</Text>
			<Text size="xs" c="dimmed" ff="monospace">
				{formatDuration(step.durationMs)}
			</Text>
			{step.usage.totalTokens > 0 && (
				<Text size="xs" c="dimmed" ff="monospace">
					{step.usage.totalTokens} tok
				</Text>
			)}
		</Group>
	);
}

// ─── Step body (Accordion.Panel) ───────────────────────────────

function StepBody({ step }: { step: PipelineStepRecord }) {
	return (
		<Stack gap="xs">
			{step.usage.totalTokens > 0 && (
				<Text size="xs" c="dimmed" ff="monospace">
					prompt {step.usage.promptTokens} + completion {step.usage.completionTokens} = {step.usage.totalTokens} tokens
				</Text>
			)}
			{step.chunks.length === 0 ? (
				<Text size="sm" c="dimmed">(keine Ausgabe)</Text>
			) : (
				step.chunks.map((chunk, i) => (
					<ChunkBlock
						key={i}
						label={step.chunks.length > 1 ? `Chunk ${i + 1} / ${step.chunks.length}` : "Ausgabe"}
						body={chunk}
					/>
				))
			)}
		</Stack>
	);
}

// ─── Chunk renderer ─────────────────────────────────────────

function ChunkBlock({ label, body }: { label: string; body: string }) {
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
			<Box
				style={{
					background: "light-dark(var(--mantine-color-gray-0), var(--mantine-color-dark-7))",
					border: "1px solid var(--mantine-color-default-border)",
					borderRadius: 4,
					padding: "var(--mantine-spacing-sm)",
					maxHeight: 320,
					overflow: "auto",
				}}
			>
				<MarkdownView source={body} size="xs" />
			</Box>
		</Stack>
	);
}

// ─── Bits ───────────────────────────────────────────────────────

function SectionTitle({ label }: { label: string }) {
	return (
		<Text size="xs" c="dimmed" tt="uppercase" fw={600}>
			{label}
		</Text>
	);
}

function KindBadge({ kind }: { kind: string }) {
	const color = KIND_COLOR[kind] ?? "gray";
	return (
		<Badge size="xs" variant="light" color={color}>
			{kind || "step"}
		</Badge>
	);
}

const KIND_COLOR: Record<string, string> = {
	where:    "blue",
	semantic: "violet",
	duck:     "indigo",
	llm:      "teal",
};

function formatDuration(ms: number): string {
	if (!Number.isFinite(ms) || ms < 0) return "—";
	if (ms < 1000)   return `${ms}ms`;
	if (ms < 10_000) return `${(ms / 1000).toFixed(1)}s`;
	if (ms < 60_000) return `${Math.round(ms / 1000)}s`;
	const min = Math.floor(ms / 60_000);
	const sec = Math.round((ms % 60_000) / 1000);
	return `${min}m${sec.toString().padStart(2, "0")}s`;
}
