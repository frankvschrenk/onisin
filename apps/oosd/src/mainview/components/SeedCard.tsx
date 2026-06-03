// SeedCard.tsx — shared run-button card used by InstallContent and
// DemoPanel. Both flows look identical: title + subtitle + body, one
// button that runs an idempotent DDL operation, a status badge and
// an error alert on failure. Keeping the card in one place keeps the
// two callers visually in lock-step even when the rest of the page
// around them is restructured.

import {
	Alert,
	Badge,
	Box,
	Button,
	Card,
	Group,
	Text,
} from "@mantine/core";
import {
	IconAlertCircle,
	IconCheck,
	IconLoader,
} from "@tabler/icons-react";

export type StepState =
	| { kind: "idle" }
	| { kind: "running" }
	| { kind: "ok" }
	| { kind: "error"; message: string };

export const initialStep: StepState = { kind: "idle" };

export interface SeedCardProps {
	title:       string;
	subtitle:    string;
	icon:        React.ReactNode;
	description: React.ReactNode;
	buttonLabel: string;
	state:       StepState;
	disabled:    boolean;
	onRun:       () => void;
}

export function SeedCard({
	title, subtitle, icon, description, buttonLabel,
	state, disabled, onRun,
}: SeedCardProps) {
	return (
		<Card withBorder padding="lg" radius="md">
			<Group justify="space-between" mb="xs">
				<Group gap="xs">
					{icon}
					<Box>
						<Text fw={600}>{title}</Text>
						<Text c="dimmed" size="xs">{subtitle}</Text>
					</Box>
				</Group>
				<StateBadge state={state} />
			</Group>

			<Box c="dimmed" mb="md" style={{ fontSize: 14, lineHeight: 1.55 }}>
				{description}
			</Box>

			<Button onClick={onRun} disabled={disabled} loading={state.kind === "running"}>
				{buttonLabel}
			</Button>

			{state.kind === "error" && (
				<Alert
					mt="md"
					color="red"
					icon={<IconAlertCircle size={16} />}
					title="Seed failed"
				>
					<Text size="sm" style={{ fontFamily: "monospace" }}>{state.message}</Text>
				</Alert>
			)}
		</Card>
	);
}

function StateBadge({ state }: { state: StepState }) {
	switch (state.kind) {
		case "idle":
			return <Badge color="gray" variant="light">Not run</Badge>;
		case "running":
			return (
				<Badge color="indigo" variant="light" leftSection={<IconLoader size={12} />}>
					Running
				</Badge>
			);
		case "ok":
			return (
				<Badge color="teal" variant="light" leftSection={<IconCheck size={12} />}>
					Done
				</Badge>
			);
		case "error":
			return (
				<Badge color="red" variant="light" leftSection={<IconAlertCircle size={12} />}>
					Failed
				</Badge>
			);
	}
}
