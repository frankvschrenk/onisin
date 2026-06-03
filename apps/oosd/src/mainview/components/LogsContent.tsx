// LogsContent.tsx — Settings → Logs section for oosd.
//
// Shows the local Dexie log ring-buffer. The user can filter by level
// and clear all entries. Auto-refreshes every 5 seconds.

import { useEffect, useState } from "react";
import {
	Badge,
	Box,
	Button,
	Group,
	ScrollArea,
	SegmentedControl,
	Stack,
	Table,
	Text,
	Title,
} from "@mantine/core";
import { IconTrash } from "@tabler/icons-react";

import {
	getRecentLogs,
	clearLogs,
	type LogEntry,
} from "../store/log-store";

// ─── Level badge colours ─────────────────────────────────────────────

const LEVEL_COLOR: Record<string, string> = {
	error: "red",
	warn:  "orange",
	info:  "blue",
	debug: "gray",
};

const POLL_MS = 5000;

// ─── Component ───────────────────────────────────────────────────────

export function LogsContent() {
	const [entries,     setEntries]     = useState<LogEntry[]>([]);
	const [levelFilter, setLevelFilter] = useState("all");
	const [clearing,    setClearing]    = useState(false);

	async function load() {
		const rows = await getRecentLogs(200);
		setEntries(rows);
	}

	useEffect(() => {
		void load();
		const t = setInterval(() => { void load(); }, POLL_MS);
		return () => clearInterval(t);
	}, []);

	async function handleClear() {
		setClearing(true);
		await clearLogs();
		setEntries([]);
		setClearing(false);
	}

	const visible = levelFilter === "all"
		? entries
		: entries.filter((e) => e.level === levelFilter);

	return (
		<Box p="lg" style={{ height: "100%", display: "flex", flexDirection: "column" }}>

			{/* Header */}
			<Group justify="space-between" mb="md" align="flex-end">
				<Box>
					<Title order={4} mb={2}>Logs</Title>
					<Text size="sm" c="dimmed">
						Local log ring-buffer — last 500 entries. Auto-refreshes every 5 s.
					</Text>
				</Box>
				<Button
					leftSection={<IconTrash size={14} />}
					variant="subtle"
					color="red"
					size="xs"
					loading={clearing}
					onClick={() => void handleClear()}
				>
					Clear
				</Button>
			</Group>

			{/* Level filter */}
			<SegmentedControl
				size="xs"
				mb="sm"
				value={levelFilter}
				onChange={setLevelFilter}
				data={[
					{ label: "All",   value: "all"   },
					{ label: "Error", value: "error" },
					{ label: "Warn",  value: "warn"  },
					{ label: "Info",  value: "info"  },
					{ label: "Debug", value: "debug" },
				]}
			/>

			{/* Table */}
			<ScrollArea style={{ flex: 1, minHeight: 0 }} type="scroll">
				{visible.length === 0 ? (
					<Text size="sm" c="dimmed" ta="center" mt="xl">
						No log entries.
					</Text>
				) : (
					<Table striped highlightOnHover withTableBorder fz="xs">
						<Table.Thead>
							<Table.Tr>
								<Table.Th w={160}>Time</Table.Th>
								<Table.Th w={60}>Level</Table.Th>
								<Table.Th w={100}>Source</Table.Th>
								<Table.Th>Message</Table.Th>
							</Table.Tr>
						</Table.Thead>
						<Table.Tbody>
							{visible.map((e) => (
								<Table.Tr key={e.id}>
									<Table.Td c="dimmed">
										{new Date(e.ts).toLocaleTimeString()}
									</Table.Td>
									<Table.Td>
										<Badge
											size="xs"
											color={LEVEL_COLOR[e.level] ?? "gray"}
											variant="light"
										>
											{e.level}
										</Badge>
									</Table.Td>
									<Table.Td c="dimmed">{e.source}</Table.Td>
									<Table.Td style={{ wordBreak: "break-word" }}>
										<Stack gap={2}>
											<Text size="xs">{e.message}</Text>
											{e.fields && Object.keys(e.fields).length > 0 && (
												<Text size="xs" c="dimmed" ff="monospace">
													{JSON.stringify(e.fields)}
												</Text>
											)}
										</Stack>
									</Table.Td>
								</Table.Tr>
							))}
						</Table.Tbody>
					</Table>
				)}
			</ScrollArea>
		</Box>
	);
}
