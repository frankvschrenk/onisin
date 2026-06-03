// SettingsPanel.tsx — Settings panel for oosd.
//
// Layout: narrow left section list, content area on the right.
// Sections:
//   connection — NATS URL
//   llm        — LLM base URL, API key, model
//   database   — DB connection URL for DDL operations

import { useCallback, useEffect, useState } from "react";
import { listModels } from "../llm/models";
import {
	Box,
	Button,
	Divider,
	Group,
	PasswordInput,
	Select,
	Stack,
	Text,
	TextInput,
	Title,
	Tooltip,
	UnstyledButton,
} from "@mantine/core";
import { IconCheck, IconRefresh } from "@tabler/icons-react";

import { saveOosdSettings, useOosdSettings, type OosdSettings } from "../store/settings";
import { LogsContent } from "./LogsContent";
import { InstallContent } from "./InstallContent";
import { rpc } from "../rpc";

// ─── Section registry ─────────────────────────────────────────────────

type SectionId = "connection" | "llm" | "database" | "install" | "logs";

interface Section {
	id:       SectionId;
	label:    string;
	subtitle: string;
}

// "install" sits directly under "database" because it depends on it:
// schema installation needs the Database URL set above.
const SECTIONS: Section[] = [
	{ id: "connection", label: "Connection",     subtitle: "Backend services" },
	{ id: "llm",        label: "LLM",            subtitle: "Language model" },
	{ id: "database",   label: "Database",       subtitle: "DB connection" },
	{ id: "install",    label: "Install schema", subtitle: "One-time setup" },
	{ id: "logs",       label: "Logs",           subtitle: "Local log viewer" },
];

// ─── Panel ────────────────────────────────────────────────────────────

export function SettingsPanel() {
	const [activeSection, setActiveSection] = useState<SectionId>("connection");

	return (
		<Box
			style={{
				display: "grid",
				gridTemplateColumns: "220px 1fr",
				height: "100%",
				minHeight: 0,
			}}
		>
			{/* Left: section list */}
			<Box
				style={{
					borderRight: "1px solid var(--mantine-color-default-border)",
					display: "flex",
					flexDirection: "column",
					minHeight: 0,
				}}
			>
				<Text size="sm" fw={600} c="dimmed" px="sm" py="xs">
					Settings
				</Text>
				<Divider />
				<Stack gap={0} p={4}>
					{SECTIONS.map((s) => (
						<SectionRow
							key={s.id}
							section={s}
							active={activeSection === s.id}
							onClick={() => setActiveSection(s.id)}
						/>
					))}
				</Stack>
			</Box>

			{/* Right: content */}
			<Box style={{ overflow: "auto" }}>
				{activeSection === "connection" && <ConnectionContent />}
				{activeSection === "llm"        && <LlmContent />}
				{activeSection === "database"   && <DatabaseContent />}
				{activeSection === "install"    && <InstallContent />}
				{activeSection === "logs"       && <LogsContent />}
			</Box>
		</Box>
	);
}

// ─── SectionRow ───────────────────────────────────────────────────────

function SectionRow({
	section,
	active,
	onClick,
}: {
	section: Section;
	active:  boolean;
	onClick: () => void;
}) {
	return (
		<UnstyledButton
			onClick={onClick}
			style={{
				width: "100%",
				padding: "6px 8px",
				borderRadius: 4,
				borderLeft: active
					? "3px solid var(--mantine-color-indigo-6)"
					: "3px solid transparent",
				background: active ? "var(--mantine-color-indigo-0)" : "transparent",
				transition: "background 80ms ease",
			}}
		>
			<Text size="sm" fw={active ? 600 : 500} c={active ? "indigo.7" : undefined}>
				{section.label}
			</Text>
			<Text size="xs" c="dimmed">{section.subtitle}</Text>
		</UnstyledButton>
	);
}

// ─── SaveRow — shared save button + saved indicator ───────────────────

function SaveRow({
	saved,
	onSave,
}: {
	saved:  boolean;
	onSave: () => void;
}) {
	return (
		<Group mt="xl" justify="flex-end">
			{saved && (
				<Group gap={4}>
					<IconCheck size={14} color="var(--mantine-color-green-6)" />
					<Text size="sm" c="green.6">Saved</Text>
				</Group>
			)}
			<Button onClick={onSave}>
				Save
			</Button>
		</Group>
	);
}

// ─── ConnectionContent ────────────────────────────────────────────────

function ConnectionContent() {
	const { settings: current } = useOosdSettings();
	const [natsUrl, setNatsUrl] = useState(current.natsUrl);
	const [saved,   setSaved]   = useState(false);

	useEffect(() => { setNatsUrl(current.natsUrl); }, [current.natsUrl]);

	function handleSave() {
		void saveOosdSettings({ ...current, natsUrl });
		setSaved(true);
		setTimeout(() => setSaved(false), 2000);
	}


	return (
		<Box p="lg" maw={560}>
			<Title order={4} mb={4}>Connection</Title>
			<Text size="sm" c="dimmed" mb="lg">
				NATS server URL. All communication between oosd and the backend
				goes through NATS.
			</Text>

			<Stack gap="md">
				<TextInput
					label="NATS URL"
					description="NATS server for all backend communication"
					placeholder="nats://localhost:4222"
					value={natsUrl}
					onChange={(e) => setNatsUrl(e.currentTarget.value)}
				/>
			</Stack>

			<SaveRow saved={saved} onSave={handleSave} />
		</Box>
	);
}

// ─── LlmContent ───────────────────────────────────────────────────────

const REFRESH_DEBOUNCE_MS = 400;

function LlmContent() {
	const { settings: current, loaded } = useOosdSettings();
	const [llmBaseUrl, setLlmBaseUrl] = useState(current.llmBaseUrl);
	const [llmApiKey,  setLlmApiKey]  = useState(current.llmApiKey);
	const [llmModel,   setLlmModel]   = useState(current.llmModel);
	const [saved,      setSaved]      = useState(false);
	const [models,        setModels]  = useState<string[]>([]);
	const [modelsLoading, setMLoading] = useState(false);
	const [modelsError,   setMError]   = useState<string | null>(null);

	useEffect(() => {
		setLlmBaseUrl(current.llmBaseUrl);
		setLlmApiKey(current.llmApiKey);
		setLlmModel(current.llmModel);
	}, [current]);

	const refreshModels = useCallback(async (base: string, key: string) => {
		setMLoading(true);
		setMError(null);
		try {
			const ids = await listModels(base, key);
			setModels(ids);
			if (ids.length > 0 && !ids.includes(llmModel)) setLlmModel("");
		} catch (err) {
			setModels([]);
			setMError(err instanceof Error ? err.message : String(err));
		} finally {
			setMLoading(false);
		}
	}, [llmModel]);

	useEffect(() => {
		const h = setTimeout(() => void refreshModels(llmBaseUrl, llmApiKey), REFRESH_DEBOUNCE_MS);
		return () => clearTimeout(h);
	}, [llmBaseUrl, llmApiKey, refreshModels]);

	function handleSave() {
		void saveOosdSettings({ ...current, llmBaseUrl, llmApiKey, llmModel });
		setSaved(true);
		setTimeout(() => setSaved(false), 2000);
	}


	return (
		<Box p="lg" maw={560}>
			<Title order={4} mb={4}>LLM</Title>
			<Text size="sm" c="dimmed" mb="lg">
				Anything that speaks the OpenAI-compatible API works here —
				vLLM, OpenAI, Anthropic via a proxy, or any local endpoint like Ollama.
			</Text>

			<Stack gap="md">
				<TextInput
					label="LLM base URL"
					placeholder="http://localhost:11434"
					value={llmBaseUrl}
					onChange={(e) => setLlmBaseUrl(e.currentTarget.value)}
				/>

				<PasswordInput
					label="API key"
					placeholder="leave blank for local endpoints"
					value={llmApiKey}
					onChange={(e) => setLlmApiKey(e.currentTarget.value)}
				/>

				<Group align="flex-end" gap="xs" wrap="nowrap">
					<Select
						label="Chat model"
						placeholder={
							modelsLoading
								? "loading…"
								: models.length === 0
								? "no models found"
								: "select a model"
						}
						value={llmModel || null}
						onChange={(v) => setLlmModel(v ?? "")}
						data={models}
						searchable
						nothingFoundMessage="No matching model"
						style={{ flex: 1 }}
						disabled={modelsLoading || models.length === 0}
					/>
					<Tooltip label="Refresh model list" withArrow>
						<Button
							variant="default"
							onClick={() => void refreshModels(llmBaseUrl, llmApiKey)}
							loading={modelsLoading}
							px="xs"
						>
							<IconRefresh size={16} />
						</Button>
					</Tooltip>
				</Group>

				{modelsError && (
					<Text size="xs" c="red">{modelsError}</Text>
				)}
			</Stack>

			<SaveRow saved={saved} onSave={handleSave} />
		</Box>
	);
}

// ─── DatabaseContent ──────────────────────────────────────────────────

function DatabaseContent() {
	const { settings: current } = useOosdSettings();
	const [dbUrl, setDbUrl] = useState(current.dbUrl);
	const [saved, setSaved] = useState(false);

	useEffect(() => { setDbUrl(current.dbUrl); }, [current.dbUrl]);


	function handleSave() {
		void saveOosdSettings({ ...current, dbUrl });
		setSaved(true);
		setTimeout(() => setSaved(false), 2000);
	}


	return (
		<Box p="lg" maw={560}>
			<Title order={4} mb={4}>Database</Title>
			<Text size="sm" c="dimmed" mb="lg">
				Connection URL for direct database operations — schema
				installation and table creation from domain DSL. Data queries
				go through NATS, not this connection.
			</Text>

			<Stack gap="md">
				<PasswordInput
					label="Database URL"
					description="Contains credentials — shown only on request"
					placeholder="postgres://user:pass@localhost:5432/dbname?sslmode=disable"
					value={dbUrl}
					onChange={(e) => setDbUrl(e.currentTarget.value)}
				/>
			</Stack>

			<SaveRow saved={saved} onSave={handleSave} />
		</Box>
	);
}
