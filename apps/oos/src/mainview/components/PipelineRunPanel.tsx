// PipelineRunPanel.tsx — execution result panel for a single pipeline run.
//
// Persistence: the run state is written back into the tab payload via
// updatePipelineRunTab so it survives unmount/remount cycles. Three
// pieces of state are tracked across mounts: status, output, runId.
// The runId is the key insight — once oosai has accepted the run, the
// bun side keeps its NATS subscription open until the result arrives,
// even after the React component unmounts. A remount during a running
// pipeline must therefore reuse that runId, not call rpc.runPipeline
// again, otherwise a second background run is launched and the first
// result is dropped on the floor (subscribePipelineRunResult filters
// by runId).
//
// Three mount scenarios:
//   - hasResult       → final markdown shown immediately, no new run.
//   - already running → status=running + initialRunId set: resubscribe.
//   - cold start      → no result, no runId: rpc.runPipeline() now.
//
// Flow:
//   1. Cold start: rpc.runPipeline() — returns immediately with runId.
//   2. runId persisted into tab payload via updatePipelineRunTab.
//   3. oosai runs in background (no timeout).
//   4. pipelineRunResult push → result stored in tab payload + editor.
//   5. On remount with stored output: editor filled from payload, no new run.
//   6. On remount during run: subscribe to existing runId, wait for push.

import { useEffect, useRef, useState } from "react";
import {
	MDXEditor,
	BoldItalicUnderlineToggles,
	UndoRedo,
	BlockTypeSelect,
	CreateLink,
	InsertTable,
	ListsToggle,
	toolbarPlugin,
	headingsPlugin,
	listsPlugin,
	quotePlugin,
	thematicBreakPlugin,
	tablePlugin,
	markdownShortcutPlugin,
	linkPlugin,
	linkDialogPlugin,
	type MDXEditorMethods,
} from "@mdxeditor/editor";
import {
	Alert,
	Box,
	Group,
	Loader,
	Progress,
	Stack,
	Text,
	ThemeIcon,
} from "@mantine/core";
import {
	IconAlertCircle,
	IconCheck,
	IconTimeline,
} from "@tabler/icons-react";

import { rpc }                            from "../rpc";
import {
	subscribePipelineRunResult,
	subscribePipelineRunProgress,
	type PipelineRunProgressEvent,
}                                          from "../rpc";
import { updatePipelineRunTab }           from "../store/tabs";

// ── Props ───────────────────────────────────────────────────────────────────

interface Props {
	tabId:          string;
	pipelineName:   string;
	initialStatus:  "idle" | "running" | "done" | "error";
	initialOutput:  string;
	initialError?:  string;
	initialUsage?:  { promptTokens: number; completionTokens: number; totalTokens: number };
	/**
	 * runId of a pipeline run that was launched in a previous mount.
	 * When set on mount and no result is stored yet, the panel does
	 * not start a new run — it resubscribes to this id and waits.
	 */
	initialRunId?:  string;
}

// ── Markdown cleaner ────────────────────────────────────────────────────────

function stripReasoningBlocks(md: string): string {
	let clean = md
		.replace(/<er_thought>[\s\S]*?<\/er_thought>/gi, "")
		.replace(/<think>[\s\S]*?<\/think>/gi, "");
	if (/^---er_thought/i.test(clean.trimStart())) {
		const blankLine = clean.indexOf("\n\n");
		clean = blankLine !== -1 ? clean.slice(blankLine).trim() : "";
	}
	return clean.trim();
}

// ── Component ────────────────────────────────────────────────────────────────

export function PipelineRunPanel({
	tabId,
	pipelineName,
	initialStatus,
	initialOutput,
	initialError,
	initialUsage,
	initialRunId,
}: Props) {
	// Three scenarios on mount:
	//   1. Output is stored → final result, no new run.
	//   2. No output but a runId is persisted → a run is already in
	//      flight on the bun side. Resubscribe without starting a new one.
	//   3. No output and no runId → cold start, kick off the run.
	const hasResult       = initialOutput.trim().length > 0;
	const hasPendingRunId = !hasResult && !!initialRunId;
	const startState =
		hasResult                       ? "done"    :
		initialStatus === "error"       ? "error"   :
		hasPendingRunId                 ? "running" :
		                                  "running";

	const [status, setStatus] = useState<"idle" | "running" | "done" | "error">(startState);
	const [error,  setError]  = useState<string | undefined>(initialError);
	const [usage,  setUsage]  = useState(initialUsage);
	const [runId,  setRunId]  = useState<string | null>(initialRunId ?? null);
	const [progress, setProgress] = useState<PipelineRunProgressEvent | null>(null);
	const startedRef          = useRef(false);
	const editorRef           = useRef<MDXEditorMethods>(null);

	// initialMarkdown is computed once at mount — MDXEditor gets it
	// as the initial `markdown` prop so no setMarkdown race on mount.
	const initialMarkdown = hasResult ? stripReasoningBlocks(initialOutput) : "";

	// Cold start only — a pending runId means oosai is already running
	// the pipeline and bun is holding the NATS subscription open, so
	// kicking off a second run would duplicate work and drop the first
	// result (subscribePipelineRunResult filters by runId).
	useEffect(() => {
		if (startedRef.current || hasResult || hasPendingRunId) return;
		startedRef.current = true;
		void start();
	}, []);

	// Subscribe to live per-step progress while the run is in flight.
	useEffect(() => {
		if (!runId) return;
		return subscribePipelineRunProgress((event) => {
			if (event.runId !== runId) return;
			setProgress(event);
		});
	}, [runId]);

	// Subscribe to push result.
	useEffect(() => {
		if (!runId) return;
		return subscribePipelineRunResult((result) => {
			if (result.runId !== runId) return;
			if (result.ok) {
				const clean = stripReasoningBlocks(result.output);
				editorRef.current?.setMarkdown(clean);
				setStatus("done");
				if (result.usage) setUsage(result.usage);
				// Persist into tab payload so remount shows the result.
				updatePipelineRunTab(tabId, { status: "done", output: result.output });
			} else {
				setError(result.error ?? "unknown error");
				setStatus("error");
				updatePipelineRunTab(tabId, { status: "error", error: result.error });
			}
		});
	}, [runId, tabId]);

	async function start() {
		setStatus("running");
		setError(undefined);
		setRunId(null);
		setProgress(null);
		try {
			const res = await rpc.runPipeline({ name: pipelineName });
			if (!res.accepted) {
				setError(res.error ?? "Pipeline rejected");
				setStatus("error");
				updatePipelineRunTab(tabId, { status: "error", error: res.error });
				return;
			}
			setRunId(res.runId);
			// Persist runId so a remount during the run can resubscribe
			// to this same run instead of starting a second one.
			updatePipelineRunTab(tabId, { status: "running", runId: res.runId });
		} catch (err) {
			const msg = err instanceof Error ? err.message : String(err);
			setError(msg);
			setStatus("error");
			updatePipelineRunTab(tabId, { status: "error", error: msg });
		}
	}

	return (
		<Box style={{ height: "100%", display: "flex", flexDirection: "column", overflow: "hidden" }}>

			{/* Header */}
			<Box
				px="lg" py="sm"
				style={{ borderBottom: "1px solid var(--mantine-color-default-border)", flexShrink: 0 }}
			>
				<Group gap="sm">
					<ThemeIcon variant="light" color="blue" size="md">
						<IconTimeline size={16} />
					</ThemeIcon>
					<Stack gap={0} style={{ flex: 1 }}>
						<Text fw={600} size="sm">{pipelineName}</Text>
						<Text size="xs" c="dimmed">Pipeline Run</Text>
					</Stack>
					{status === "running" && (
						<Group gap="xs">
							<Loader size="xs" />
							<Text size="xs" c="dimmed">läuft…</Text>
						</Group>
					)}
					{status === "done" && (
						<Group gap="xs">
							{usage && (
								<Text size="xs" c="dimmed">
									{usage.totalTokens.toLocaleString()} tokens
								</Text>
							)}
							<ThemeIcon color="green" variant="light" size="sm">
								<IconCheck size={12} />
							</ThemeIcon>
						</Group>
					)}
				</Group>
			</Box>

			{status === "error" && error && (
				<Box px="lg" pt="sm" style={{ flexShrink: 0 }}>
					<Alert icon={<IconAlertCircle size={16} />} color="red">{error}</Alert>
				</Box>
			)}

			{status === "running" && (
				<Box px="lg" pt="md" style={{ flexShrink: 0 }}>
					<RunProgress progress={progress} />
				</Box>
			)}

			{/* MDXEditor — Markdown renderer mit Toolbar */}
			<Box style={{ flex: 1, overflow: "auto", minHeight: 0 }}>
				<MDXEditor
					ref={editorRef}
					markdown={initialMarkdown}
					plugins={[
						toolbarPlugin({
							toolbarContents: () => (
								<>
									<UndoRedo />
									<BlockTypeSelect />
									<BoldItalicUnderlineToggles />
									<ListsToggle />
									<CreateLink />
									<InsertTable />
								</>
							),
						}),
						headingsPlugin(),
						listsPlugin(),
						quotePlugin(),
						thematicBreakPlugin(),
						tablePlugin(),
						linkPlugin(),
						linkDialogPlugin(),
						markdownShortcutPlugin(),
					]}
				/>
			</Box>
		</Box>
	);
}

// ── RunProgress ────────────────────────────────────────────────────────────
//
// Renders a step- and row-aware progress bar based on the latest
// progress event from oosai. Before the first event arrives we still
// show a placeholder so the user has feedback that something is happening.

interface RunProgressProps {
	progress: PipelineRunProgressEvent | null;
}

function RunProgress({ progress }: RunProgressProps) {
	if (!progress) {
		return (
			<Stack gap="xs">
				<Text size="sm" c="dimmed">
					Pipeline startet…
				</Text>
				<Progress value={0} animated striped size="sm" />
			</Stack>
		);
	}

	const { stepIndex, stepName, stepKind, rowIndex, totalRows } = progress;
	const totalSteps = progress.totalSteps ?? 0;

	// Row-aware percentage: complete steps + fractional progress in the current step.
	const rowFraction = totalRows > 0 ? (rowIndex + 1) / totalRows : 1;
	const pct = totalSteps > 0
		? Math.min(100, Math.round(((stepIndex + rowFraction) / totalSteps) * 100))
		: 0;

	const stepLabel = totalSteps > 0
		? `Schritt ${stepIndex + 1} / ${totalSteps}`
		: `Schritt ${stepIndex + 1}`;
	const rowLabel = totalRows > 1
		? ` · Zeile ${rowIndex + 1} / ${totalRows}`
		: "";

	return (
		<Stack gap="xs">
			<Group justify="space-between" wrap="nowrap">
				<Text size="sm">
					<Text span fw={600}>{stepLabel}</Text>
					<Text span c="dimmed">{rowLabel}</Text>
					<Text span c="dimmed" size="xs"> — {stepKind} {stepName}</Text>
				</Text>
				<Text size="xs" c="dimmed" ff="monospace">{pct}%</Text>
			</Group>
			<Progress value={pct} animated striped size="sm" />
		</Stack>
	);
}
