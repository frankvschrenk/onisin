// QueryInspector.tsx — Collapsed-by-default panel that exposes the
// raw query strings a tab is using.
//
// One panel per tab. Multiple queries (e.g. a list view's GraphQL
// query, or a detail tab's data + meta queries) live as labelled
// entries inside the same panel; each carries its own copy button.
//
// Renders nothing when the entries list is empty — the panel only
// shows up when there is something diagnostic to expose.
//
// History note: the original inspector lived inline in ViewRenderer
// and only handled a single query. It was lifted out so DetailRenderer
// can use the same control to surface its meta-query. Mantine 9
// renamed the Collapse trigger prop from `in` to `expanded`; that
// detail is captured here too because it surprised us once.

import {
	ActionIcon,
	Box,
	Code,
	Collapse,
	CopyButton,
	Group,
	Paper,
	Stack,
	Text,
	Tooltip,
} from "@mantine/core";
import {
	IconCheck,
	IconChevronDown,
	IconChevronRight,
	IconCopy,
} from "@tabler/icons-react";
import { useState, type ReactElement } from "react";

/** One labelled query string inside the inspector. */
export interface QueryEntry {
	/** Short label shown above the code block, e.g. "Daten" / "Meta". */
	label: string;
	/** The query string itself; empty entries are dropped silently. */
	value: string;
	/**
	 * Optional language hint for future syntax-highlighting. Not used
	 * today — Code renders plain text — but kept on the type so we can
	 * switch to a highlighter later without a breaking interface.
	 */
	language?: "graphql" | "url" | "text";
}

/** Props for QueryInspector. */
export interface QueryInspectorProps {
	/** Header text; defaults to "GraphQL Query". */
	heading?:    string;
	/** Entries to render, in order. Empty entries are filtered out. */
	entries:     readonly QueryEntry[];
	/** Open the panel by default. False keeps the chrome compact. */
	defaultOpen?: boolean;
}

/**
 * QueryInspector renders a collapsed-by-default panel showing the
 * raw query strings active for this tab. One header + one collapse
 * region. Each entry has its own label and copy button.
 */
export function QueryInspector(props: QueryInspectorProps): ReactElement | null {
	const { heading = "GraphQL Query", entries, defaultOpen = false } = props;
	const visible = entries.filter((e) => e.value.trim().length > 0);
	const [open, setOpen] = useState(defaultOpen);
	if (visible.length === 0) return null;

	return (
		<Paper
			withBorder
			radius="sm"
			mb="md"
			style={{
				background: "light-dark(var(--mantine-color-gray-0), var(--mantine-color-dark-7))",
				borderColor: "var(--mantine-color-gray-3)",
			}}
		>
			<Group
				justify="space-between"
				wrap="nowrap"
				gap="xs"
				p="xs"
				onClick={() => setOpen((v) => !v)}
				style={{ cursor: "pointer", userSelect: "none" }}
			>
				<Group gap={6} wrap="nowrap">
					{open ? (
						<IconChevronDown size={14} />
					) : (
						<IconChevronRight size={14} />
					)}
					<Text size="xs" fw={600} c="gray.7">
						{heading}
					</Text>
					{visible.length > 1 ? (
						<Text size="xs" c="gray.5">
							({visible.length} Einträge)
						</Text>
					) : null}
				</Group>
			</Group>
			<Collapse expanded={open}>
				<Stack gap="sm" px="xs" pb="xs">
					{visible.map((entry, i) => (
						<QueryEntryView key={i} entry={entry} />
					))}
				</Stack>
			</Collapse>
		</Paper>
	);
}

/** One entry inside the inspector — label, copy button, code block. */
function QueryEntryView({ entry }: { entry: QueryEntry }): ReactElement {
	return (
		<Box>
			<Group justify="space-between" wrap="nowrap" gap="xs" mb={4}>
				<Text size="xs" fw={500} c="gray.6">
					{entry.label}
				</Text>
				<CopyButton value={entry.value} timeout={1500}>
					{({ copied, copy }) => (
						<Tooltip
							label={copied ? "Kopiert" : "Kopieren"}
							withArrow
							position="left"
						>
							<ActionIcon
								variant="subtle"
								color="gray"
								size="sm"
								onClick={(e) => {
									e.stopPropagation();
									copy();
								}}
								aria-label={`${entry.label} kopieren`}
							>
								{copied ? (
									<IconCheck size={14} />
								) : (
									<IconCopy size={14} />
								)}
							</ActionIcon>
						</Tooltip>
					)}
				</CopyButton>
			</Group>
			<Code
				block
				style={{
					fontSize: 12,
					whiteSpace: "pre-wrap",
					wordBreak: "break-word",
				}}
			>
				{entry.value}
			</Code>
		</Box>
	);
}
