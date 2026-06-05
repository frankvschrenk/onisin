// ServiceTable.tsx — live table of every Onisin process publishing on
// status.*. One row per { nodeId, service } pair; live / stale / gone reflects
// the time since the last heartbeat tick.
//
// Rows for backend services with an env.show responder (oosai, oosgql — and
// any future service wiring up oos-env) are clickable: the click fires a fresh
// oos.cmd.<svc>.env.show request and reveals the resolved env entries plus
// their provenance underneath the row. The introspectable set is hardcoded on
// purpose — a service that doesn't reply shows an error inside the expanded
// panel, but a service with no responder shouldn't even look expandable.

import type * as React from "react";
import { useEffect, useMemo, useState } from "react";
import { Badge, Code, Group, Loader, Stack, Table, Text, Tooltip } from "@mantine/core";

import { rpc } from "../rpc";
import type { ServiceRow, ServiceStatus } from "../state";

interface Props {
	rows: ServiceRow[];
	/** Wall-clock time at render, driven by the App-level 1Hz tick so the
	 *  "last seen" age stays current even when no fresh heartbeat arrives. */
	now: number;
}

const STATUS_COLOR: Record<ServiceStatus, string> = {
	live: "teal",
	stale: "yellow",
	gone: "red",
};

/** Service names that expose oos.cmd.<svc>.env.show. Others render a plain
 *  non-expandable row. */
const INTROSPECTABLE_SERVICES = new Set(["oosai", "oosgql"]);

/** Number of columns in the main table. Used as colSpan on the expanded detail
 *  row so the panel stretches across all columns. */
const TABLE_COL_COUNT = 9;

/** Entry shape returned by oos.cmd.<svc>.env.show. */
interface EnvEntryView {
	key: string;
	value: string;
	source: string;
}

/** Source-string -> badge colour. Defaults to gray for unknown values. */
function sourceBadgeColor(source: string): string {
	if (source === "default") return "gray";
	if (source === "runtime") return "violet";
	if (source.startsWith("env:")) return "teal";
	return "gray";
}

/** shortNodeId returns the first 8 chars of the Base32 node id, or a dash if
 *  the publisher hasn't initialised its identity yet. */
function shortNodeId(nodeId: string): string {
	if (!nodeId) return "\u2014";
	return nodeId.slice(0, 8);
}

/** ageLabel returns a compact human-readable age string. */
function ageLabel(then: string, now: number): string {
	const ms = now - new Date(then).getTime();
	if (ms < 1_000) return "just now";
	if (ms < 60_000) return `${Math.floor(ms / 1_000)}s ago`;
	if (ms < 3_600_000) return `${Math.floor(ms / 60_000)}m ago`;
	return `${Math.floor(ms / 3_600_000)}h ago`;
}

/** EnvDetail renders the inner expanded panel for one service. Held as its own
 *  top-level component so its useState survives App-level re-renders triggered
 *  by the 1Hz timer and incoming heartbeats. */
function EnvDetail({ service }: { service: string }): React.JSX.Element {
	const [state, setState] = useState<
		| { phase: "loading" }
		| { phase: "ok"; entries: EnvEntryView[] }
		| { phase: "error"; message: string }
	>({ phase: "loading" });

	// One fetch per mount; the row's expansion toggle decides whether to mount
	// this at all, so click -> expand -> fetch falls out for free. Cancellable so
	// a rapid collapse/reopen does not stack stale responses on fresh ones.
	useEffect(() => {
		let cancelled = false;
		void (async () => {
			const res = await rpc.getServiceEnv({ service });
			if (cancelled) return;
			if (res.ok) setState({ phase: "ok", entries: res.reply.entries });
			else setState({ phase: "error", message: res.error });
		})();
		return () => { cancelled = true; };
	}, [service]);

	if (state.phase === "loading") {
		return (
			<Group gap="xs" px="md" py="sm">
				<Loader size="xs" />
				<Text size="sm" c="dimmed">Requesting oos.cmd.{service}.env.show\u2026</Text>
			</Group>
		);
	}
	if (state.phase === "error") {
		return (
			<Stack gap={4} px="md" py="sm">
				<Text size="sm" c="red">No response from {service}.</Text>
				<Code block style={{ fontSize: 11, background: "var(--mantine-color-gray-1)" }}>
					{state.message}
				</Code>
			</Stack>
		);
	}

	return (
		<Table
			layout="fixed"
			striped
			withColumnBorders
			style={{ margin: "4px 12px 8px", fontSize: 12, background: "var(--mantine-color-gray-0)" }}
		>
			<Table.Thead>
				<Table.Tr>
					<Table.Th style={{ width: "30%" }}>Key</Table.Th>
					<Table.Th>Value</Table.Th>
					<Table.Th style={{ width: "30%" }}>Source</Table.Th>
				</Table.Tr>
			</Table.Thead>
			<Table.Tbody>
				{state.entries.map((e) => (
					<Table.Tr key={e.key}>
						<Table.Td>
							<Text ff="monospace" size="xs">{e.key}</Text>
						</Table.Td>
						<Table.Td>
							<Text ff="monospace" size="xs" style={{ wordBreak: "break-all" }}>
								{e.value === "" ? <span style={{ opacity: 0.5 }}>(empty)</span> : e.value}
							</Text>
						</Table.Td>
						<Table.Td>
							<Badge size="xs" variant="light" color={sourceBadgeColor(e.source)} style={{ fontFamily: "monospace" }}>
								{e.source}
							</Badge>
						</Table.Td>
					</Table.Tr>
				))}
			</Table.Tbody>
		</Table>
	);
}

export function ServiceTable({ rows, now }: Props): React.JSX.Element {
	// One row may be expanded at a time, keyed by ServiceRow.key (nodeId or
	// host+pid fallback) so the expansion sticks to a process even if row order
	// changes on the next snapshot.
	const [expandedKey, setExpandedKey] = useState<string | null>(null);

	const body = useMemo(() => rows.flatMap((r) => {
		const introspectable = INTROSPECTABLE_SERVICES.has(r.last.service);
		const isExpanded = expandedKey === r.key;

		const mainRow = (
			<Table.Tr
				key={r.key}
				style={{ cursor: introspectable ? "pointer" : "default" }}
				onClick={introspectable ? () => setExpandedKey((cur) => (cur === r.key ? null : r.key)) : undefined}
			>
				<Table.Td>
					<Badge color={STATUS_COLOR[r.status]} variant="filled" radius="sm">{r.status}</Badge>
				</Table.Td>
				<Table.Td>
					<Group gap={6}>
						<Text fw={600}>{r.last.service}</Text>
						{introspectable && <Text size="xs" c="dimmed">{isExpanded ? "\u25be" : "\u25b8"}</Text>}
					</Group>
				</Table.Td>
				<Table.Td>
					<Tooltip label={r.last.nodeId || "node id not yet initialised"} withArrow>
						<Text ff="monospace" size="sm">{shortNodeId(r.last.nodeId)}</Text>
					</Tooltip>
				</Table.Td>
				<Table.Td>{r.last.host}</Table.Td>
				<Table.Td>
					<Text ff="monospace" size="sm">{r.last.version}</Text>
				</Table.Td>
				<Table.Td>
					<Text ff="monospace" size="sm">{r.last.pid}</Text>
				</Table.Td>
				<Table.Td>
					<Tooltip label={new Date(r.last.startedAt).toLocaleString()} withArrow>
						<Text size="sm">{ageLabel(r.last.startedAt, now)}</Text>
					</Tooltip>
				</Table.Td>
				<Table.Td>
					<Tooltip label={new Date(r.last.ts).toLocaleString()} withArrow>
						<Text size="sm">{ageLabel(r.last.ts, now)}</Text>
					</Tooltip>
				</Table.Td>
				<Table.Td>
					<Text ff="monospace" size="sm" c="dimmed">{r.ticks}</Text>
				</Table.Td>
			</Table.Tr>
		);

		if (!isExpanded) return [mainRow];
		return [
			mainRow,
			<Table.Tr key={`${r.key}__env`}>
				<Table.Td colSpan={TABLE_COL_COUNT} style={{ padding: 0, background: "var(--mantine-color-gray-0)" }}>
					<EnvDetail service={r.last.service} />
				</Table.Td>
			</Table.Tr>,
		];
	}), [rows, now, expandedKey]);

	if (rows.length === 0) {
		return (
			<Stack align="center" justify="center" h="50vh" gap="xs">
				<Text c="dimmed" size="lg">Waiting for heartbeats\u2026</Text>
				<Text c="dimmed" size="sm">Every Onisin process publishes on status.&lt;service&gt; every 5s.</Text>
			</Stack>
		);
	}

	return (
		<Table striped highlightOnHover stickyHeader withTableBorder withColumnBorders>
			<Table.Thead>
				<Table.Tr>
					<Table.Th>Status</Table.Th>
					<Table.Th>Service</Table.Th>
					<Table.Th>Node</Table.Th>
					<Table.Th>Host</Table.Th>
					<Table.Th>Version</Table.Th>
					<Table.Th>PID</Table.Th>
					<Table.Th>Started</Table.Th>
					<Table.Th>Last seen</Table.Th>
					<Table.Th>Ticks</Table.Th>
				</Table.Tr>
			</Table.Thead>
			<Table.Tbody>{body}</Table.Tbody>
		</Table>
	);
}

/** Header summary chip row — one badge per status group. */
export function StatusSummary({ rows }: { rows: ServiceRow[] }): React.JSX.Element {
	const counts = useMemo(() => {
		const c: Record<ServiceStatus, number> = { live: 0, stale: 0, gone: 0 };
		for (const r of rows) c[r.status]++;
		return c;
	}, [rows]);

	return (
		<Group gap="xs">
			<Badge color="teal" variant="light">{counts.live} live</Badge>
			<Badge color="yellow" variant="light">{counts.stale} stale</Badge>
			<Badge color="red" variant="light">{counts.gone} gone</Badge>
		</Group>
	);
}
