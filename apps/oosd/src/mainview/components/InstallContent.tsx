// InstallContent.tsx — "Install schema" section under Settings.
//
// Runs the one-time internal-schema seed: creates the oos.* tables,
// the vector tables and the event_mappings registry. Lives in
// Settings because it is a prerequisite step like setting the LLM
// or the Database URL — not part of the demo dataset.
//
// DSN is read from Settings → Database. If it is empty the run
// button is disabled and a hint tells the user where to set it.

import { useState } from "react";
import {
	Alert,
	Box,
	List,
	Stack,
	Text,
	Title,
} from "@mantine/core";
import { IconDatabase, IconInfoCircle } from "@tabler/icons-react";

import { rpc } from "../rpc";
import { useOosdSettings } from "../store/settings";
import { SeedCard, initialStep, type StepState } from "./SeedCard";

export function InstallContent() {
	const { settings } = useOosdSettings();
	const dsn = settings.dbUrl;

	const [step, setStep] = useState<StepState>(initialStep);
	const running = step.kind === "running";
	const disabled = !dsn.trim() || running;

	async function onRun() {
		setStep({ kind: "running" });
		const res = await rpc.runInternalSeed({ dsn });
		if (res.ok) setStep({ kind: "ok" });
		else setStep({ kind: "error", message: res.error ?? "unknown" });
	}

	return (
		<Box p="lg" maw={620}>
			<Title order={4} mb={4}>Install schema</Title>
			<Text size="sm" c="dimmed" mb="lg">
				One-time setup for a fresh database. Idempotent — re-running
				refreshes the schema without harming existing data.
			</Text>

			<Stack gap="md">
				{!dsn.trim() && (
					<Alert
						color="yellow"
						icon={<IconInfoCircle size={16} />}
						title="Database URL missing"
					>
						Set the Database URL in <strong>Settings → Database</strong>
						{" "}first — schema installation needs a direct connection.
					</Alert>
				)}

				<SeedCard
					title="Install internal schema"
					subtitle="Required once before first start."
					icon={<IconDatabase size={20} />}
					description={
						<>
							Creates the <code>oos</code> schema and everything needed to run:
							<List size="sm" mt={4}>
								<List.Item>
									<code>oos.domain</code>, <code>oos.view</code>,{" "}
									<code>oos.config</code>, <code>oos.global_prompt</code>
								</List.Item>
								<List.Item>
									Vector tables (<code>oos_domain_schema</code>,{" "}
									<code>oos_view_schema</code>,{" "}
									<code>oos_global_schema</code>)
								</List.Item>
								<List.Item>
									<code>oos.event_mappings</code> registry
								</List.Item>
							</List>
						</>
					}
					buttonLabel="Install internal schema"
					state={step}
					disabled={disabled}
					onRun={onRun}
				/>
			</Stack>
		</Box>
	);
}
