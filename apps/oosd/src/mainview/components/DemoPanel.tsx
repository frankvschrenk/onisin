// DemoPanel.tsx — seed panel for the curated demo dataset.
//
// The DSN comes from Settings → Database. The one-time internal-schema
// install moved into Settings → Install schema; the panel here is now
// purely about loading and reloading the demo content.
//
// All seed operations are idempotent: TRUNCATE + re-insert. Each card
// can be run independently and a failure on one card does not block
// the others.

import { useState } from "react";
import {
	Alert,
	Box,
	List,
	Stack,
	Text,
	Title,
} from "@mantine/core";
import {
	IconDatabase,
	IconInfoCircle,
	IconSparkles,
	IconTimeline,
} from "@tabler/icons-react";

import { rpc } from "../rpc";
import { useOosdSettings } from "../store/settings";
import { SeedCard, initialStep, type StepState } from "./SeedCard";

// Props kept for API compatibility with the sidebar caller.
// eslint-disable-next-line @typescript-eslint/no-unused-vars
interface DemoPanelProps { disabled: boolean; dsn: string; }

export function DemoPanel(_props: DemoPanelProps) {
	const { settings } = useOosdSettings();
	const dsn = settings.dbUrl;

	const [demo,     setDemo]     = useState<StepState>(initialStep);
	const [police,   setPolice]   = useState<StepState>(initialStep);
	const [support,  setSupport]  = useState<StepState>(initialStep);
	const [pipeline, setPipeline] = useState<StepState>(initialStep);

	const busy =
		demo.kind     === "running" ||
		police.kind   === "running" ||
		support.kind  === "running" ||
		pipeline.kind === "running";
	const seedDisabled = !dsn.trim() || busy;

	async function onRunDemo() {
		setDemo({ kind: "running" });
		const res = await rpc.runDemoSeed({ dsn });
		if (res.ok) setDemo({ kind: "ok" });
		else setDemo({ kind: "error", message: res.error ?? "unknown" });
	}

	async function onRunPolice() {
		setPolice({ kind: "running" });
		const res = await rpc.runPoliceSeed({ dsn });
		if (res.ok) setPolice({ kind: "ok" });
		else setPolice({ kind: "error", message: res.error ?? "unknown" });
	}

	async function onRunSupport() {
		setSupport({ kind: "running" });
		const res = await rpc.runSupportSeed({ dsn });
		if (res.ok) setSupport({ kind: "ok" });
		else setSupport({ kind: "error", message: res.error ?? "unknown" });
	}

	async function onRunPipeline() {
		setPipeline({ kind: "running" });
		const res = await rpc.runPipelineSeed({ dsn });
		if (res.ok) setPipeline({ kind: "ok" });
		else setPipeline({ kind: "error", message: res.error ?? "unknown" });
	}

	return (
		<Box p="xl" style={{ maxWidth: 760, overflowY: "auto", height: "100%" }}>
			<Stack gap="lg">
				<Box>
					<Title order={2}>Demo</Title>
					<Text c="dimmed" size="sm" mt={4}>
						Curated demo content for a fresh local environment. All steps
						are idempotent — re-running refreshes the data.
					</Text>
				</Box>

				{!dsn.trim() && (
					<Alert
						color="yellow"
						icon={<IconInfoCircle size={16} />}
						title="Database URL missing"
					>
						Set the Database URL in <strong>Settings → Database</strong>
						{" "}first. Once that is done, run{" "}
						<strong>Settings → Install schema</strong> if the internal
						oos schema does not exist yet, then come back here.
					</Alert>
				)}

				<SeedCard
					title="Install demo tables and data"
					subtitle="Run after the internal schema."
					icon={<IconSparkles size={20} />}
					description={
						<>
							Installs the public application schema and a curated demo dataset:
							<List size="sm" mt={4}>
								<List.Item>Reference lookups, 10 demo persons and 12 notes</List.Item>
								<List.Item>
									The bundled <code>person.domain</code>,{" "}
									<code>note.domain</code> and four <code>.view</code> sources
								</List.Item>
							</List>
						</>
					}
					buttonLabel="Install demo data"
					state={demo}
					disabled={seedDisabled}
					onRun={onRunDemo}
				/>

				<SeedCard
					title="Install police event source"
					subtitle="Requires demo tables. Can be run independently."
					icon={<IconDatabase size={20} />}
					description={
						<>
							Seeds the <code>police_incidents</code> table with two complete cases:
							<List size="sm" mt={4}>
								<List.Item>fall-2024-0042 — Burglary at Industriestrasse München (8 events)</List.Item>
								<List.Item>fall-2024-0080 — Augsburg parrot hostage situation (7 events)</List.Item>
							</List>
							Truncates existing police data first.
						</>
					}
					buttonLabel="Install police events"
					state={police}
					disabled={seedDisabled}
					onRun={onRunPolice}
				/>

				<SeedCard
					title="Install support event source"
					subtitle="Requires demo tables. Can be run independently."
					icon={<IconSparkles size={20} />}
					description={
						<>
							Seeds the <code>support_tickets</code> table with three full lifecycles:
							<List size="sm" mt={4}>
								<List.Item>customer-12345 — Shipping: delayed parcel (created → updated → resolved)</List.Item>
								<List.Item>customer-67890 — Billing: double charge (created → updated → resolved)</List.Item>
								<List.Item>department-IT — VPN outage (created → updated × 2 → resolved)</List.Item>
							</List>
							Truncates existing support data first.
						</>
					}
					buttonLabel="Install support events"
					state={support}
					disabled={seedDisabled}
					onRun={onRunSupport}
				/>

				<SeedCard
					title="Install pipeline demo"
					subtitle="Requires demo tables. Can be run independently."
					icon={<IconTimeline size={20} />}
					description={
						<>
							Creates <code>pipeline_documents</code> and <code>pipelines</code> tables with four demo cases:
							<List size="sm" mt={4}>
								<List.Item>10 detailed Schadensfälle (DB + S3, context mode)</List.Item>
								<List.Item>100 short Schadensfälle (DB, mass mode)</List.Item>
								<List.Item>4 demo .pipeline configurations</List.Item>
								<List.Item>S3 documents uploaded to RustFS bucket &lsquo;schaeden&rsquo;</List.Item>
							</List>
						</>
					}
					buttonLabel="Install pipeline demo"
					state={pipeline}
					disabled={seedDisabled}
					onRun={onRunPipeline}
				/>
			</Stack>
		</Box>
	);
}
