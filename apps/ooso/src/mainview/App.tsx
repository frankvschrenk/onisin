// App.tsx — ooso top-level layout (Tauri / Pfad C).
//
// The webview owns the process table directly now: a status.* subscription
// feeds ingest(), a 1Hz timer ages the rows and advances "now", and ooso
// publishes its own status.ooso heartbeat. Changing the bus URL re-runs the
// NATS effect against the new ws endpoint.

import type * as React from "react";
import { useEffect, useState } from "react";
import { AppShell, Container, Group, Stack, Text, TextInput, Title } from "@mantine/core";
import { IconActivityHeartbeat } from "@tabler/icons-react";

import { getWsUrl, setWsUrl, subscribeStatus } from "./nats";
import { ingest, snapshot, type HeartbeatPayload, type ServiceRow } from "./state";
import { getIdentity } from "./identity";
import { startHeartbeat } from "./heartbeat";
import { ServiceTable, StatusSummary } from "./components/ServiceTable";

export function App(): React.JSX.Element {
	const [rows, setRows] = useState<ServiceRow[]>([]);
	const [now, setNow] = useState<number>(() => Date.now());
	const [natsUrl, setNatsUrl] = useState<string>(() => getWsUrl());
	const [draftUrl, setDraftUrl] = useState<string>(() => getWsUrl());

	// NATS lifecycle — re-runs when the bus URL changes. Subscribes to status.*
	// (feeding the local table) and starts ooso's own heartbeat once the native
	// identity is known. waitOnFirstConnect keeps this pending until the bus is
	// reachable, so a cold start before nats-server is up still wires up.
	useEffect(() => {
		let cancelled = false;
		let unsub: () => void = () => {};
		let stopHeartbeat: () => void = () => {};
		void (async () => {
			try {
				const id = await getIdentity();
				if (cancelled) return;
				unsub = await subscribeStatus<HeartbeatPayload>((hb) => {
					ingest(hb);
					setRows(snapshot());
				});
				if (cancelled) { unsub(); return; }
				stopHeartbeat = startHeartbeat(id);
			} catch (err) {
				console.warn(`[ooso] nats setup failed: ${String(err)}`);
			}
		})();
		return () => { cancelled = true; unsub(); stopHeartbeat(); };
	}, [natsUrl]);

	// 1Hz tick — advance "now" and re-read the snapshot so the age columns keep
	// ticking and live->stale->gone transitions show even when no new heartbeat
	// has arrived.
	useEffect(() => {
		const id = setInterval(() => {
			setNow(Date.now());
			setRows(snapshot());
		}, 1_000);
		return () => clearInterval(id);
	}, []);

	async function applyNatsUrl(): Promise<void> {
		const trimmed = draftUrl.trim();
		if (!trimmed || trimmed === natsUrl) return;
		await setWsUrl(trimmed);
		setNatsUrl(trimmed); // re-runs the NATS effect against the new bus
	}

	return (
		<AppShell padding="md" header={{ height: 64 }}>
			<AppShell.Header px="md">
				<Group h="100%" justify="space-between" align="center">
					<Group gap="xs">
						<IconActivityHeartbeat size={22} color="var(--mantine-color-teal-6)" />
						<Title order={3}>Onisin Operator</Title>
						<Text c="dimmed" size="sm">live process console</Text>
					</Group>
					<Group gap="md">
						<StatusSummary rows={rows} />
						<TextInput
							w={280}
							size="sm"
							placeholder="ws://localhost:4223"
							value={draftUrl}
							onChange={(e) => setDraftUrl(e.currentTarget.value)}
							onBlur={() => { void applyNatsUrl(); }}
							onKeyDown={(e) => { if (e.key === "Enter") { void applyNatsUrl(); } }}
						/>
					</Group>
				</Group>
			</AppShell.Header>
			<AppShell.Main>
				<Container size="xl" px={0}>
					<Stack gap="md">
						<ServiceTable rows={rows} now={now} />
					</Stack>
				</Container>
			</AppShell.Main>
		</AppShell>
	);
}
