// ResultTable.tsx — Generic renderer for a GraphQL read-query payload.
//
// The shape we expect is the one oosgql produces for any read of one
// domain:
//
//     { person: [{ id: 1, firstname: "Anna", … }, …] }
//
// We render the array as a table and infer the columns from the union
// of every row's keys, in the order they first appear. Long string
// cells are clipped; null/undefined render as a dimmed em-dash so the
// reader can tell "missing" apart from empty string.
//
// When the payload is anything else (no array, multiple top-level
// keys, plain object, or an empty result) we fall back to a JSON
// pretty-print. This is rare in the read-only first cut but worth
// having so a reply tab never goes blank.
//
// The full GraphQL query the LLM produced is shown collapsed at the
// top so the user can see exactly what ran. It also reads as a useful
// debugging aid while the loop is being tuned.

import { useState } from "react";
import { Box, Code, Group, Stack, Text } from "@mantine/core";

interface ResultTableProps {
	contextName: string;
	query:       string;
	data:        unknown;
}

export function ResultTable({ contextName, query, data }: ResultTableProps) {
	const rows = extractRows(data);

	return (
		<Box p="lg" style={{ overflow: "auto", height: "100%" }}>
			<Stack gap="md">
				<QueryPreview query={query} />

				{rows
					? <Table contextName={contextName} rows={rows} />
					: <JsonFallback data={data} />}
			</Stack>
		</Box>
	);
}

// ─── Header with the GraphQL query ───────────────────────────────────

function QueryPreview({ query }: { query: string }) {
	const [open, setOpen] = useState(false);
	return (
		<Box>
			<Group gap="xs" mb={4}>
				<Text size="xs" c="dimmed" fw={600} tt="uppercase">
					GraphQL
				</Text>
				<Text
					size="xs"
					c="brand"
					style={{ cursor: "pointer", userSelect: "none" }}
					onClick={() => setOpen((v) => !v)}
				>
					{open ? "verbergen" : "anzeigen"}
				</Text>
			</Group>
			{open && (
				<Code block style={{ whiteSpace: "pre-wrap", fontSize: 12 }}>
					{query}
				</Code>
			)}
		</Box>
	);
}

// ─── Table renderer ──────────────────────────────────────────────────

interface TableProps {
	contextName: string;
	rows:        Array<Record<string, unknown>>;
}

function Table({ contextName, rows }: TableProps) {
	if (rows.length === 0) {
		return (
			<Text c="dimmed">
				Keine Datensätze für „{contextName}".
			</Text>
		);
	}
	const columns = orderedColumns(rows);
	return (
		<Box style={{ overflowX: "auto" }}>
			<table
				style={{
					width:          "100%",
					borderCollapse: "collapse",
					fontSize:       13,
				}}
			>
				<thead>
					<tr style={{ textAlign: "left", color: "var(--mantine-color-gray-7)" }}>
						{columns.map((col) => (
							<th key={col} style={cellStyle}>{col}</th>
						))}
					</tr>
				</thead>
				<tbody>
					{rows.map((row, i) => (
						<tr
							key={(row.id as string | number | undefined) ?? i}
							style={{ borderTop: "1px solid light-dark(var(--mantine-color-gray-2), var(--mantine-color-dark-5))" }}
						>
							{columns.map((col) => (
								<td key={col} style={cellStyle}>
									{renderCell(row[col])}
								</td>
							))}
						</tr>
					))}
				</tbody>
			</table>
			<Text size="xs" c="dimmed" mt="sm">
				{rows.length} {rows.length === 1 ? "Eintrag" : "Einträge"}
			</Text>
		</Box>
	);
}

const cellStyle: React.CSSProperties = {
	padding:        "8px 12px",
	verticalAlign:  "top",
	maxWidth:       320,
	overflow:       "hidden",
	textOverflow:   "ellipsis",
	whiteSpace:     "nowrap",
};

/**
 * renderCell turns one cell value into a React node, with a light
 * presentation pass: nullish becomes a dimmed em-dash, booleans
 * render as their German labels, objects as JSON.
 */
function renderCell(value: unknown): React.ReactNode {
	if (value === null || value === undefined) {
		return <span style={{ color: "var(--mantine-color-gray-5)" }}>—</span>;
	}
	if (typeof value === "boolean") return value ? "ja" : "nein";
	if (typeof value === "object") return JSON.stringify(value);
	return String(value);
}

/**
 * orderedColumns gathers every key seen in any row and returns them
 * in first-seen order. `id` is hoisted to the front when present so
 * the user always sees the identifier first.
 */
function orderedColumns(rows: Array<Record<string, unknown>>): string[] {
	const seen = new Set<string>();
	const out: string[] = [];
	for (const row of rows) {
		for (const k of Object.keys(row)) {
			if (!seen.has(k)) {
				seen.add(k);
				out.push(k);
			}
		}
	}
	const idx = out.indexOf("id");
	if (idx > 0) {
		out.splice(idx, 1);
		out.unshift("id");
	}
	return out;
}

// ─── Fallback ────────────────────────────────────────────────────────

function JsonFallback({ data }: { data: unknown }) {
	return (
		<Code block style={{ whiteSpace: "pre-wrap", fontSize: 12 }}>
			{JSON.stringify(data, null, 2)}
		</Code>
	);
}

// ─── Shape detection ─────────────────────────────────────────────────

/**
 * extractRows recognises the canonical `{ <key>: [row, …] }` envelope
 * and returns the row array. Anything else (multiple keys, a single
 * non-array value, primitives) falls back to the JSON pretty-printer.
 */
function extractRows(data: unknown): Array<Record<string, unknown>> | null {
	if (!data || typeof data !== "object") return null;
	const entries = Object.entries(data as Record<string, unknown>);
	if (entries.length !== 1) return null;
	const [, value] = entries[0]!;
	if (!Array.isArray(value)) return null;
	return value.map((row) =>
		row && typeof row === "object" ? (row as Record<string, unknown>) : { value: row },
	);
}
