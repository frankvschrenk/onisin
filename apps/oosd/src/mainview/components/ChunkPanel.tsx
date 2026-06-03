// ChunkPanel.tsx — Live preview of the LLM-facing chunk.
//
// Re-parses the active source on every change and renders the
// resulting chunk text the same way it ends up in pgvector. This
// is the place to look when you want to know what the LLM
// retrieves about a domain or view.
//
// Architecture: parsing goes through the diagnostics worker
// (parseDomainInWorker / parseViewInWorker) to keep Langium out
// of the main-thread bundle — vscode-jsonrpc's cancellation
// namespace import trips Electrobun's main-thread bundler ("Can't
// find variable: exports_cancellation"). The chunk renderers
// themselves are pure functions of the runtime def with no Langium
// dependency, so they run on the main thread; we import them
// through `oos-dsls-ts/renderers` which is a Langium-free entry
// point that exists for exactly this kind of consumer.
//
// Failure modes:
//   - source still empty after a fresh select  → friendly hint
//   - parser produced no def or fatal errors   → show the errors
//   - parser produced a def with warnings      → render chunk + warnings
//
// We don't try to be smart about partial chunks: if the AST root
// is missing, the chunk would lie about the schema and that's
// worse than showing the user the underlying error.

import { useEffect, useState } from "react";

import { Box, Group, ScrollArea, Stack, Text } from "@mantine/core";
import Editor from "@monaco-editor/react";
import {
	renderLLMChunk,
	renderViewChunk,
} from "oos-dsls-ts/renderers";
import type { Diagnostic } from "vscode-languageserver-types";

import {
	parseDomainInWorker,
	parseViewInWorker,
} from "../../lang/worker/parse-client";
import type { Kind } from "../types";

// ─── Public component ────────────────────────────────────────────────

interface ChunkPanelProps {
	kind:     Kind;
	source:   string;
	selected: string | null;
}

type State =
	| { kind: "empty" }
	| { kind: "ok";       chunk: string; diagnostics: Diagnostic[] }
	| { kind: "no-def";   diagnostics: Diagnostic[] }
	| { kind: "error";    message: string };

export function ChunkPanel({ kind, source, selected }: ChunkPanelProps) {
	const [state, setState] = useState<State>({ kind: "empty" });

	useEffect(() => {
		if (!source.trim()) {
			setState({ kind: "empty" });
			return;
		}
		let cancelled = false;
		void (async () => {
			try {
				const next = await renderForSource(kind, source, selected);
				if (cancelled) return;
				setState(next);
			} catch (err) {
				if (cancelled) return;
				const msg = err instanceof Error ? err.message : String(err);
				setState({ kind: "error", message: msg });
			}
		})();
		return () => {
			cancelled = true;
		};
	}, [kind, source, selected]);

	if (!selected && state.kind === "empty") {
		return <Hint>No selection — pick a {kind} from the list.</Hint>;
	}

	if (state.kind === "empty") {
		return <Hint>Source is empty — start typing to see the chunk.</Hint>;
	}

	if (state.kind === "error") {
		return (
			<Hint tone="error">
				Renderer threw: {state.message}
			</Hint>
		);
	}

	if (state.kind === "no-def") {
		return (
			<Stack gap="md" p="md">
				<Hint tone="error">
					Source could not be parsed — the chunk needs a complete
					DSL tree. Fix the errors below and the preview comes
					back.
				</Hint>
				<DiagnosticList diagnostics={state.diagnostics} />
			</Stack>
		);
	}

	const errors   = state.diagnostics.filter((d) => d.severity === 1);
	const warnings = state.diagnostics.filter((d) => d.severity === 2);

	return (
		<ScrollArea style={{ height: "100%" }} offsetScrollbars>
			<Stack gap="sm" p="md">
				<ChunkHeader
					length={state.chunk.length}
					errorCount={errors.length}
					warningCount={warnings.length}
				/>
				{warnings.length > 0 && (
					<DiagnosticList diagnostics={warnings} kind="warning" />
				)}
				{errors.length > 0 && (
					<DiagnosticList diagnostics={errors} kind="error" />
				)}
				<Box style={{ border: "1px solid var(--mantine-color-default-border)", borderRadius: 4, overflow: "hidden" }}>
					<Editor
						height={Math.max(300, state.chunk.split("\n").length * 19)}
						defaultLanguage="plaintext"
						value={state.chunk}
						options={{
							readOnly:              true,
							minimap:               { enabled: false },
							scrollBeyondLastLine:  false,
							fontSize:              12,
							lineNumbers:           "on",
							wordWrap:              "on",
							folding:               false,
							automaticLayout:       true,
							scrollbar:             { alwaysConsumeMouseWheel: false },
						}}
					/>
				</Box>
			</Stack>
		</ScrollArea>
	);
}

// ─── Render dispatch ─────────────────────────────────────────────────

/**
 * renderForSource ships the source to the diagnostics worker, then
 * picks the right chunk renderer for the kind on the main thread.
 *
 * The worker URI is a stable in-memory id derived from the row
 * selection; reusing it across keystrokes is fine because the worker
 * only uses it for diagnostic locations, not for caching.
 *
 * Domain chunks are fully derived from the parsed def. View chunks
 * additionally need the verbatim source (the renderer emits a header
 * and then appends the raw DSL), so renderViewChunk takes both.
 */
async function renderForSource(
	kind: Kind,
	source: string,
	selected: string | null,
): Promise<State> {
	const uri = `inmemory://oosd/${kind}/${selected ?? "scratch"}`;

	if (kind === "domain") {
		const result = await parseDomainInWorker(source, uri);
		if (!result.def) {
			return { kind: "no-def", diagnostics: result.diagnostics };
		}
		return {
			kind: "ok",
			chunk: renderLLMChunk(result.def),
			diagnostics: result.diagnostics,
		};
	}

	const result = await parseViewInWorker(source, uri);
	if (!result.def) {
		return { kind: "no-def", diagnostics: result.diagnostics };
	}
	return {
		kind: "ok",
		chunk: renderViewChunk(result.def, source),
		diagnostics: result.diagnostics,
	};
}

// ─── Sub-components ──────────────────────────────────────────────────

function ChunkHeader({
	length,
	errorCount,
	warningCount,
}: {
	length:       number;
	errorCount:   number;
	warningCount: number;
}) {
	const tokens = approximateTokens(length);
	return (
		<Group justify="space-between" wrap="nowrap" align="flex-start">
			<Stack gap={2}>
				<Text size="xs" c="dimmed" tt="uppercase" fw={600}>
					LLM chunk preview
				</Text>
				<Text size="xs" c="dimmed">
					This is the exact text the agent retrieves about this
					row. Embedded into pgvector by the oosai pipeline.
				</Text>
			</Stack>
			<Group gap="xs">
				<Stat label="Bytes"  value={length.toLocaleString("en-US")} />
				<Stat label="≈ Tokens" value={tokens.toLocaleString("en-US")} />
				{warningCount > 0 && (
					<Stat label="Warn" value={String(warningCount)} tone="warn" />
				)}
				{errorCount > 0 && (
					<Stat label="Err" value={String(errorCount)} tone="error" />
				)}
			</Group>
		</Group>
	);
}

function Stat({
	label,
	value,
	tone,
}: {
	label: string;
	value: string;
	tone?: "warn" | "error";
}) {
	const colour =
		tone === "error" ? "var(--mantine-color-red-7)"
		: tone === "warn"  ? "var(--mantine-color-yellow-8)"
		: "var(--mantine-color-default-color)";
	return (
		<Stack gap={0} align="flex-end">
			<Text size="xs" c="dimmed" tt="uppercase" fw={600}>
				{label}
			</Text>
			<Text size="sm" ff="monospace" style={{ color: colour }}>
				{value}
			</Text>
		</Stack>
	);
}

function DiagnosticList({
	diagnostics,
	kind = "error",
}: {
	diagnostics: Diagnostic[];
	kind?: "error" | "warning";
}) {
	if (diagnostics.length === 0) return null;
	const colour =
		kind === "error"
			? "var(--mantine-color-red-1)"
			: "var(--mantine-color-yellow-1)";
	const border =
		kind === "error"
			? "var(--mantine-color-red-4)"
			: "var(--mantine-color-yellow-5)";
	return (
		<Box
			style={{
				background: colour,
				border: `1px solid ${border}`,
				borderRadius: 4,
				padding: "6px 10px",
			}}
		>
			<Text size="xs" fw={600} mb={4} tt="uppercase">
				{kind === "error" ? "Errors" : "Warnings"}
			</Text>
			<Stack gap={2}>
				{diagnostics.map((d, i) => (
					<Text key={i} size="xs" ff="monospace">
						{formatDiagnostic(d)}
					</Text>
				))}
			</Stack>
		</Box>
	);
}

function Hint({
	children,
	tone = "info",
}: {
	children: React.ReactNode;
	tone?: "info" | "error";
}) {
	const colour =
		tone === "error"
			? "var(--mantine-color-red-7)"
			: "var(--mantine-color-dimmed)";
	return (
		<Box p="md">
			<Text size="sm" style={{ color: colour }}>
				{children}
			</Text>
		</Box>
	);
}

// ─── Helpers ─────────────────────────────────────────────────────────

/**
 * approximateTokens converts a byte length into a rough token count.
 * 4 bytes per token is the GPT-style rule of thumb; granite-embedding
 * (which is what we actually use) is roughly comparable. We aren't
 * trying to be exact — the figure exists to give the author a feel
 * for how big the chunk is getting.
 */
function approximateTokens(bytes: number): number {
	return Math.round(bytes / 4);
}

function formatDiagnostic(d: Diagnostic): string {
	const line = d.range.start.line + 1;
	const col  = d.range.start.character + 1;
	return `${line}:${col}  ${d.message}`;
}
