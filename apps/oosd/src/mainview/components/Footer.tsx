// Footer.tsx — Slim status bar for oosd.
//
// Shows NATS connectivity status. Mirrors the Footer pattern from oos
// for a consistent look across all desktop apps.

import { useEffect, useState } from "react";
import { AppShellFooter, Badge, Group, Text, Tooltip } from "@mantine/core";
import { IconAlertTriangle, IconCircleCheck } from "@tabler/icons-react";
import { rpc } from "../rpc";
import { listModels } from "../llm/models";
import { useOosdSettings } from "../store/settings";

const PING_INTERVAL_MS = 15_000;

type Health =
	| { kind: "loading" }
	| { kind: "ok" }
	| { kind: "fail"; message: string };

export function Footer() {
	const { settings, loaded } = useOosdSettings();
	const [natsHealth, setNatsHealth] = useState<Health>({ kind: "loading" });
	const [llmHealth,  setLlmHealth]  = useState<Health>({ kind: "loading" });

	useEffect(() => {
		if (!loaded || !settings.natsUrl) return;
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

	useEffect(() => {
		if (!loaded || !settings.llmBaseUrl) return;
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

	const natsShort = friendlyUrl(settings.natsUrl);
	const llmShort  = friendlyUrl(settings.llmBaseUrl);

	return (
		<AppShellFooter>
			<Group h="100%" px="md" justify="space-between" gap="md">
				{/* Left: model badge */}
				<Group gap="xs">
					{settings.llmModel && (
						<>
							<Text size="xs" c="dimmed">Model</Text>
							<Badge size="sm" variant="light" color="indigo">
								{settings.llmModel}
							</Badge>
						</>
					)}
				</Group>

				{/* Right: LLM + NATS */}
				<Group gap="md">
					<HealthIndicator health={llmHealth}  label={llmShort} />
					<HealthIndicator health={natsHealth} label={natsShort} />
				</Group>
			</Group>
		</AppShellFooter>
	);
}

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

function friendlyUrl(url: string): string {
	try {
		const u = new URL(url);
		return u.host || url;
	} catch {
		return url.replace(/^nats:\/\//, "").replace(/^https?:\/\//, "");
	}
}
