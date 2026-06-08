// Footer.tsx — Slim status bar at the bottom of the window.
//
// Left:  current chat model badge.
// Right: NATS health · LLM endpoint health.
//
// Both health checks run on a 15-second interval and re-trigger
// immediately whenever the relevant setting changes.

import { useEffect, useState } from "react";
import { AppShellFooter, Badge, Group, Switch, Text, Tooltip } from "@mantine/core";
import { IconAlertTriangle, IconCircleCheck } from "@tabler/icons-react";

import { listModels }     from "../llm/models";
import { useAppSettings } from "../store/settings";
import { useAuthState }   from "../store/auth";
import { useUiState }     from "../store/ui-state";
import { getLocalNodeId } from "../store/node-id";
import { rpc } from "../rpc";

const PING_INTERVAL_MS = 15_000;

type Health =
	| { kind: "loading" }
	| { kind: "ok" }
	| { kind: "fail"; message: string };

export function Footer() {
	const { settings, loaded } = useAppSettings();
	const auth    = useAuthState();
	const nodeId  = getLocalNodeId();
	// Ask-mode "Editor" switch: route the next Ask answer into a new
	// editable Markdown tab instead of a chat bubble. Only visible in
	// Ask mode so the footer stays uncluttered in the other modes.
	const { state: uiState, save: saveUi, loaded: uiLoaded } = useUiState();
	const [llmHealth,  setLlmHealth]  = useState<Health>({ kind: "loading" });
	const [natsHealth, setNatsHealth] = useState<Health>({ kind: "loading" });

	// LLM endpoint health — pings GET /v1/models.
	useEffect(() => {
		if (!loaded) return;
		let cancelled = false;
		const ping = async () => {
			try {
				await listModels(settings.llmBaseUrl, settings.llmApiKey);
				if (!cancelled) setLlmHealth({ kind: "ok" });
			} catch (err) {
				if (cancelled) return;
				const msg = err instanceof Error ? err.message : String(err);
				setLlmHealth({ kind: "fail", message: msg });
			}
		};
		void ping();
		const handle = setInterval(() => void ping(), PING_INTERVAL_MS);
		return () => { cancelled = true; clearInterval(handle); };
	}, [loaded, settings.llmBaseUrl, settings.llmApiKey]);

	// NATS health — asks Bun to ping via a lightweight RPC call.
	useEffect(() => {
		if (!loaded) return;
		let cancelled = false;
		const ping = async () => {
			try {
				const result = await rpc.pingNats({ url: settings.natsUrl });
				if (cancelled) return;
				if (result.ok) setNatsHealth({ kind: "ok" });
				else           setNatsHealth({ kind: "fail", message: result.error ?? "unreachable" });
			} catch (err) {
				if (cancelled) return;
				const msg = err instanceof Error ? err.message : String(err);
				setNatsHealth({ kind: "fail", message: msg });
			}
		};
		void ping();
		const handle = setInterval(() => void ping(), PING_INTERVAL_MS);
		return () => { cancelled = true; clearInterval(handle); };
	}, [loaded, settings.natsUrl]);

	const endpointShort = friendlyEndpoint(settings.llmBaseUrl);
	const natsShort     = friendlyEndpoint(settings.natsUrl);

	return (
		<AppShellFooter>
			<Group h="100%" px="md" justify="space-between" gap="md">
				{/* Left: user + model */}
				<Group gap="xs">
					{auth && (
						<>
							<Text size="xs" c="dimmed">{auth.username}</Text>
							<Badge size="sm" variant="light" color={auth.role === "admin" ? "red" : auth.role === "manager" ? "orange" : "gray"}>
								{auth.role || "user"}
							</Badge>
							<Text size="xs" c="dimmed">·</Text>
						</>
					)}
					<Text size="xs" c="dimmed">Model</Text>
					<Badge size="sm" variant="light" color="brand">
						{settings.llmModel || "—"}
					</Badge>
					{nodeId && (
						<>
							<Text size="xs" c="dimmed">·</Text>
							<Tooltip label={nodeId} position="top" withArrow>
								<Badge
									size="sm"
									variant="outline"
									color="gray"
									style={{ cursor: "default", fontFamily: "monospace" }}
								>
									{nodeId.slice(0, 8)}…
								</Badge>
							</Tooltip>
						</>
					)}
				</Group>

				{/* Right: editor toggle (ask mode only) + NATS + LLM status */}
				<Group gap="md">
					{uiLoaded && uiState.mode === "ask" && (
						<Tooltip
							label="Antwort als bearbeitbaren Markdown-Tab öffnen statt als Chat-Bubble"
							position="top"
							withArrow
						>
							<Switch
								size="xs"
								label="Editor"
								checked={uiState.askToTab}
								onChange={(e) => void saveUi({ ...uiState, askToTab: e.currentTarget.checked })}
							/>
						</Tooltip>
					)}
					<HealthIndicator
						health={natsHealth}
						label={natsShort}
				/>
					<HealthIndicator
						health={llmHealth}
						label={endpointShort}
				/>
				</Group>
			</Group>
		</AppShellFooter>
	);
}

// ── HealthIndicator ────────────────────────────────────────────────

function HealthIndicator({
	health, label, okLabel,
}: {
	health:   Health;
	label:    string;
	okLabel?: string;
}) {
	if (health.kind === "ok") {
		return (
			<Group gap={4}>
				<IconCircleCheck size={14} color="var(--mantine-color-green-5)" />
				<Text size="xs" c="dimmed">{okLabel ?? label}</Text>
			</Group>
		);
	}
	if (health.kind === "loading") {
		return <Text size="xs" c="dimmed">{label} · checking…</Text>;
	}
	return (
		<Tooltip label={health.message} position="top" withArrow>
			<Group gap={4}>
				<IconAlertTriangle size={14} color="var(--mantine-color-red-5)" />
				<Text size="xs" c="red.5">{label} · offline</Text>
			</Group>
		</Tooltip>
	);
}

function friendlyEndpoint(url: string): string {
	try {
		const u = new URL(url);
		return u.host || url;
	} catch {
		return url.replace(/^https?:\/\//, "").replace(/\/+$/, "").replace(/^nats:\/\//, "");
	}
}
