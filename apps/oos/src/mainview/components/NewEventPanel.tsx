// NewEventPanel.tsx — Insert a new event into a source table.
//
// Workflow:
//   1. Pick a mapping (police, warehouse, …)
//   2. Pick a stream from the mapping's event_streams
//   3. Pick an event_type — loads the Grammar source from oosai
//   4. Grammar is shown read-only so the user knows what fields exist
//   5. User types a free-text description
//   6. "Fill with LLM" button: calls the LLM with the Grammar + free text,
//      gets back a structured event object, shows it in the Monaco editor
//   7. User reviews / edits the result in Monaco
//   8. "Save" → insertEvent → POST /event/insert → notify trigger fires
//
// The LLM call goes through the bun gateway (NATS rpc). It does NOT
// go through the agent loop — it is a
// simple single-shot completion that returns JSON.
//
// State is entirely local — this panel does not touch the global
// ui-state (mapping/streamId) which belongs to events-mode chat.

import { useCallback, useEffect, useMemo, useState } from "react";
import Editor from "@monaco-editor/react";
import {
	Alert,
	Box,
	Button,
	Divider,
	Group,
	Loader,
	ScrollArea,
	Select,
	Stack,
	Text,
	Textarea,
	Title,
} from "@mantine/core";
import { IconCheck, IconPlayerPlay, IconX } from "@tabler/icons-react";
import { OnisinEditor } from "./OnisinEditor";

import { rpc } from "../rpc";
import { useAppSettings } from "../store/settings";
import type { EventMapping } from "../event-types";
import type { StreamSummary } from "../event-types";

// ─── Types ────────────────────────────────────────────────────────────

interface EventTypeSchemaRow {
	id:               number;
	event_mapping_id: number;
	event_type:       string;
	source:           string;
}

// ─── Component ───────────────────────────────────────────────────────

export function NewEventPanel() {
	const { settings, loaded: settingsLoaded } = useAppSettings();

	// ── Step 1: mapping ──
	const [mappings,        setMappings]        = useState<EventMapping[]>([]);
	const [mappingsLoading, setMappingsLoading] = useState(false);
	const [selectedMapping, setSelectedMapping] = useState<string | null>(null);

	// ── Step 2: stream ──
	const [streams,        setStreams]        = useState<StreamSummary[]>([]);
	const [streamsLoading, setStreamsLoading] = useState(false);
	const [selectedStream, setSelectedStream] = useState<string | null>(null);

	// ── Step 3: event type ──
	const [schemas,        setSchemas]        = useState<EventTypeSchemaRow[]>([]);
	const [schemasLoading, setSchemasLoading] = useState(false);
	const [selectedType,   setSelectedType]   = useState<string | null>(null);
	const [grammarSource,  setGrammarSource]  = useState<string>("");

	// ── Step 5-6: free text + LLM fill ──
	const [freeText,   setFreeText]   = useState("");
	const [filling,    setFilling]    = useState(false);
	const [fillError,  setFillError]  = useState<string | null>(null);

	// ── Step 7: editable result ──
	const [eventJson, setEventJson] = useState("");

	// ── Step 8: save ──
	const [saving,   setSaving]   = useState(false);
	const [saveMsg,  setSaveMsg]  = useState<{ ok: boolean; msg: string } | null>(null);

	// ─── Load mappings ────────────────────────────────────────────

	const loadMappings = useCallback(async () => {
		if (!settingsLoaded) return;
		setMappingsLoading(true);
		try {
			const res = await rpc.getEventMappings({});
			if (res.error) return;
			const body = JSON.parse(res.json) as { mappings?: EventMapping[] };
			setMappings(body.mappings ?? []);
		} finally {
			setMappingsLoading(false);
		}
	}, [settingsLoaded]);

	useEffect(() => { void loadMappings(); }, [loadMappings]);

	// ─── Load streams when mapping changes ───────────────────────

	useEffect(() => {
		if (!selectedMapping || !settingsLoaded) {
			setStreams([]);
			setSelectedStream(null);
			return;
		}
		let cancelled = false;
		setStreamsLoading(true);
		void rpc.getEventStreams({
			mapping:  selectedMapping,
			limit:    500,
		}).then((res) => {
			if (cancelled) return;
			if (!res.error) {
				const body = JSON.parse(res.json) as { streams?: StreamSummary[] };
				setStreams(body.streams ?? []);
			}
		}).finally(() => {
			if (!cancelled) setStreamsLoading(false);
		});
		return () => { cancelled = true; };
	}, [selectedMapping, settingsLoaded]);

	// ─── Load schemas when mapping changes ───────────────────────

	useEffect(() => {
		if (!selectedMapping || !settingsLoaded) {
			setSchemas([]);
			setSelectedType(null);
			setGrammarSource("");
			return;
		}
		let cancelled = false;
		setSchemasLoading(true);
		void rpc.getEventSchemas({
			mapping:  selectedMapping,
		}).then((res) => {
			if (cancelled) return;
			if (!res.error) {
				const body = JSON.parse(res.json) as { schemas?: EventTypeSchemaRow[] };
				setSchemas(body.schemas ?? []);
			}
		}).finally(() => {
			if (!cancelled) setSchemasLoading(false);
		});
		return () => { cancelled = true; };
	}, [selectedMapping, settingsLoaded]);

	// ─── Load grammar when event type changes ────────────────────

	useEffect(() => {
		if (!selectedMapping || !selectedType || !settingsLoaded) {
			setGrammarSource("");
			setEventJson("");
			return;
		}
		let cancelled = false;
		void rpc.getEventSchema({
			mapping:   selectedMapping,
			eventType: selectedType,
		}).then((res) => {
			if (cancelled) return;
			if (!res.error) {
				const body = JSON.parse(res.json) as { source?: string };
				setGrammarSource(body.source ?? "");
			}
		});
		return () => { cancelled = true; };
	}, [selectedMapping, selectedType, settingsLoaded]);

	// ─── LLM fill ────────────────────────────────────────────────
	//
	// Sends a single-shot completion via the bun gateway. The prompt
	// instructs the LLM to extract structured data from the free text
	// according to the Grammar, and return ONLY a JSON object.

	async function handleFill() {
		if (!selectedType || !grammarSource || !freeText.trim()) return;
		setFilling(true);
		setFillError(null);
		setEventJson("");
		try {
			const systemPrompt = [
				"You are a data-extraction assistant. The user provides a free-text description of an event.",
				"Extract the event fields according to the Grammar below and return ONLY a JSON object.",
				"The JSON must have exactly these top-level keys: \"text\" (string) and \"payload\" (object).",
				"\"text\" is a clean prose summary of the event suitable for full-text storage.",
				"\"payload\" contains all structured fields extracted from the free text.",
				"Do not include any explanation, markdown, or code fences — only the raw JSON.",
				"",
				"Grammar:",
				grammarSource,
			].join("\n");

			const userMessage = `Event type: ${selectedType}\n\nFree text:\n${freeText}`;

			const res = await rpc.chatTurn({
				turnId:   `fill-${Date.now()}`,
				settings: {
					llmBaseUrl: settings.llmBaseUrl,
							natsUrl:    settings.natsUrl,
					llmApiKey:  settings.llmApiKey,
					llmModel:   settings.llmModel,
				},
				history:  [{ role: "system", content: systemPrompt }],
				user:     userMessage,
			});

			if (res.error) {
				setFillError(res.error);
				return;
			}

			// The response text should be a raw JSON object. Try to
			// parse and re-format it so the editor shows pretty JSON.
			const raw = res.text.trim().replace(/^```json\s*|```\s*$/g, "").trim();
			try {
				const parsed = JSON.parse(raw) as unknown;
				setEventJson(JSON.stringify(parsed, null, 2));
			} catch {
				// Not valid JSON — show raw so the user can fix it.
				setEventJson(raw);
			}
		} catch (err) {
			setFillError(err instanceof Error ? err.message : String(err));
		} finally {
			setFilling(false);
		}
	}

	// ─── Save ─────────────────────────────────────────────────────

	async function handleSave() {
		if (!selectedMapping || !selectedStream || !selectedType || !eventJson.trim()) return;
		setSaving(true);
		setSaveMsg(null);
		try {
			let parsed: { text?: unknown; payload?: unknown };
			try {
				parsed = JSON.parse(eventJson) as { text?: unknown; payload?: unknown };
			} catch {
				setSaveMsg({ ok: false, msg: "Invalid JSON — please fix the event before saving." });
				return;
			}
			const text    = typeof parsed.text    === "string" ? parsed.text    : "";
			const payload = parsed.payload && typeof parsed.payload === "object" && !Array.isArray(parsed.payload)
				? parsed.payload as Record<string, unknown>
				: {};

			const res = await rpc.insertEvent({
				mapping:   selectedMapping,
				stream:    selectedStream,
				eventType: selectedType,
				text,
				payload,
			});
			if (res.ok) {
				setSaveMsg({ ok: true, msg: `Event inserted (id: ${res.id ?? "?"}).` });
				// Reset the form for the next event.
				setFreeText("");
				setEventJson("");
			} else {
				setSaveMsg({ ok: false, msg: res.error ?? "insert failed" });
			}
		} finally {
			setSaving(false);
		}
	}

	// ─── Derived ──────────────────────────────────────────────────

	const mappingOptions = useMemo(() =>
		mappings.filter((m) => m.enabled).map((m) => ({ value: m.name, label: m.name })),
		[mappings],
	);

	const streamOptions = useMemo(() =>
		streams.map((s) => ({
			value: s.stream,
			label: s.description ? `${s.stream} — ${s.description}` : s.stream,
		})),
		[streams],
	);

	const typeOptions = useMemo(() =>
		schemas.map((s) => ({ value: s.event_type, label: s.event_type })),
		[schemas],
	);

	const canFill = !!(selectedType && grammarSource && freeText.trim() && settings.llmModel);
	const canSave = !!(selectedMapping && selectedStream && selectedType && eventJson.trim());

	// ─── Render ───────────────────────────────────────────────────

	return (
		<Box style={{ display: "flex", flexDirection: "column", height: "100%", minHeight: 0 }}>
			{/* Header */}
			<Group
				px="md" py="xs" justify="space-between"
				style={{ borderBottom: "1px solid var(--mantine-color-default-border)", flexShrink: 0 }}
			>
				<Title order={5}>New Event</Title>
				<Button
					size="xs"
					color="green"
					onClick={handleSave}
					loading={saving}
					disabled={!canSave || saving}
				>
					Save
				</Button>
			</Group>

			{/* Save result */}
			{saveMsg && (
				<Alert
					mx="md" mt="xs"
					color={saveMsg.ok ? "green" : "red"}
					icon={saveMsg.ok ? <IconCheck size={14} /> : <IconX size={14} />}
					onClose={() => setSaveMsg(null)}
					withCloseButton
					style={{ flexShrink: 0 }}
				>
					{saveMsg.msg}
				</Alert>
			)}

			<ScrollArea style={{ flex: 1 }} p="md">
				<Stack gap="md" p="md">

					{/* ── Step 1–3: pickers ── */}
					<Group align="flex-end" gap="sm">
						<Select
							label="Mapping"
							placeholder={mappingsLoading ? "Loading…" : "Select mapping…"}
							data={mappingOptions}
							value={selectedMapping}
							onChange={(v) => {
								setSelectedMapping(v);
								setSelectedStream(null);
								setSelectedType(null);
								setGrammarSource("");
								setEventJson("");
								setSaveMsg(null);
							}}
							disabled={mappingsLoading || mappingOptions.length === 0}
							style={{ flex: 1 }}
							comboboxProps={{ withinPortal: true }}
						/>
						<Select
							label="Stream"
							placeholder={
								!selectedMapping ? "Select mapping first" :
								streamsLoading   ? "Loading…" :
								streams.length === 0 ? "No streams" :
								"Select stream…"
							}
							data={streamOptions}
							value={selectedStream}
							onChange={(v) => { setSelectedStream(v); setSaveMsg(null); }}
							disabled={!selectedMapping || streamsLoading || streams.length === 0}
							style={{ flex: 1 }}
							searchable
							comboboxProps={{ withinPortal: true }}
						/>
						<Select
							label="Event Type"
							placeholder={
								!selectedMapping  ? "Select mapping first" :
								schemasLoading    ? "Loading…" :
								schemas.length === 0 ? "No event types defined" :
								"Select type…"
							}
							data={typeOptions}
							value={selectedType}
							onChange={(v) => { setSelectedType(v); setEventJson(""); setSaveMsg(null); }}
							disabled={!selectedMapping || schemasLoading || schemas.length === 0}
							style={{ flex: 1 }}
							comboboxProps={{ withinPortal: true }}
						/>
					</Group>

					{/* ── Grammar display ── */}
					{grammarSource && (
						<Box>
							<Text size="sm" fw={500} mb={4}>Grammar</Text>
							<Box
								style={{
									border:       "1px solid var(--mantine-color-default-border)",
									borderRadius: 4,
									overflow:     "hidden",
									height:       120,
								}}
							>
								<Editor
									height="120px"
									language="plaintext"
									value={grammarSource}
									options={{
										readOnly:             true,
										minimap:              { enabled: false },
										fontSize:             12,
										scrollBeyondLastLine: false,
										automaticLayout:      true,
										lineNumbers:          "off",
									}}
								/>
							</Box>
						</Box>
					)}

					<Divider />

					{/* ── Free text + LLM fill ── */}
					<Box>
						<Group justify="space-between" mb={4} align="flex-end">
							<Text size="sm" fw={500}>Free text description</Text>
							<Button
								size="xs"
								variant="light"
								leftSection={filling ? <Loader size={12} /> : <IconPlayerPlay size={13} />}
								onClick={handleFill}
								disabled={!canFill || filling}
								loading={filling}
							>
								Fill with LLM
							</Button>
						</Group>
						<Textarea
							placeholder="Describe the event in plain text…"
							value={freeText}
							onChange={(e) => { setFreeText(e.currentTarget.value); setFillError(null); }}
							minRows={4}
							autosize
							disabled={!selectedType}
						/>
						{fillError && (
							<Text size="xs" c="red" mt={4}>{fillError}</Text>
						)}
						{!settings.llmModel && selectedType && (
							<Text size="xs" c="dimmed" mt={4}>
								No LLM model configured — set one in Settings → Connection.
							</Text>
						)}
					</Box>

					{/* ── Result editor ── */}
					<Box>
						<Text size="sm" fw={500} mb={4}>
							Event JSON
							{eventJson && <Text span size="xs" c="dimmed" ml={6}>(review and edit before saving)</Text>}
						</Text>
						<Box
							style={{
								border:       "1px solid var(--mantine-color-default-border)",
								borderRadius: 4,
								overflow:     "hidden",
								height:       240,
							}}
						>
							<Editor
								height="240px"
								language="json"
								value={eventJson || "// Fill with LLM or type the event JSON manually.\n// Expected shape:\n// {\n//   \"text\": \"...\",\n//   \"payload\": { ... }\n// }"}
								onChange={(v?: string) => { setEventJson(v ?? ""); setSaveMsg(null); }}
								options={{
									minimap:              { enabled: false },
									fontSize:             12,
									scrollBeyondLastLine: false,
									automaticLayout:      true,
									lineNumbers:          "on",
									wordWrap:             "on",
								}}
							/>
						</Box>
					</Box>

				</Stack>
			</ScrollArea>
		</Box>
	);
}
