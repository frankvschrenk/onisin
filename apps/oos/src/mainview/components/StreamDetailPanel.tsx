// StreamDetailPanel.tsx — event lifecycle UI for one stream.
//
// Layout:
//   Left  — chronological list of existing events. Left-click a row
//            loads it into the editor on the right. Right-click opens
//            an action menu (Schliessen, Löschen) at the cursor
//            position. Right-click on the empty list area starts a new
//            event. Closed events get a lock icon and are dimmed.
//            The action menu disables Schliessen and Löschen on closed
//            rows since the DB trigger would reject them anyway.
//   Right — panel content depends on `mode`:
//     idle  : help text — "pick an event or right-click for new"
//     new   : event-type picker + editor + Speichern (insertEvent)
//     edit  : readonly event-type label + editor + Aktualisieren
//             (updateEvent). When the event is closed, the editor is
//             read-only and all action buttons are hidden — the row is
//             history.
//
// Closed-event semantics (Phase 1 backend):
//   - The DB trigger blocks UPDATE and DELETE when closed=true. We
//     mirror the rule in the UI so users get a clear signal before the
//     server-side rejection.
//   - "Schliessen" is a one-way flip and is confirmed with a modal so
//     the user knows future edits will be blocked.
//   - "Löschen" is also behind a confirm modal — source row and
//     embedding both go away.

import { useCallback, useEffect, useRef, useState } from "react";
import {
	ActionIcon,
	Alert,
	Autocomplete,
	Badge,
	Box,
	Button,
	Divider,
	Group,
	Loader,
	Menu,
	ScrollArea,
	Stack,
	Text,
	Title,
	Tooltip,
	UnstyledButton,
} from "@mantine/core";
import { modals } from "@mantine/modals";
import {
	IconCheck,
	IconLock,
	IconPlus,
	IconRefresh,
	IconSortAscending,
	IconSortDescending,
	IconTrash,
	IconX,
} from "@tabler/icons-react";
import { type OnMount }  from "@monaco-editor/react";
import { OnisinEditor }  from "./OnisinEditor";
import type * as MonacoType from "monaco-editor";

import { rpc } from "../rpc";
import { useAppSettings } from "../store/settings";

// ─── Monaco language + completion ────────────────────────────────

const EDITOR_LANGUAGE = "oos-event-content";

interface FieldDef {
	name:     string;
	type:     string;
	required: boolean;
}

function extractFields(source: string): FieldDef[] {
	const fields: FieldDef[] = [];
	const re = /\b(required|optional)\s+(\w+)\s*:\s*(string|number|boolean)/g;
	let m: RegExpExecArray | null;
	while ((m = re.exec(source)) !== null) {
		fields.push({ required: m[1] === "required", name: m[2]!, type: m[3]! });
	}
	return fields;
}

let languageRegistered = false;
let completionDisposable: MonacoType.IDisposable | null = null;

function ensureLanguageRegistered(monaco: typeof MonacoType): void {
	if (languageRegistered) return;
	languageRegistered = true;
	monaco.languages.register({ id: EDITOR_LANGUAGE });
	monaco.languages.setMonarchTokensProvider(EDITOR_LANGUAGE, {
		tokenizer: {
			initial: [
				{ regex: /"(?:[^"\\]|\\.)*"/, action: { token: "string" } },
				{ regex: /\d+(\.\d+)?/,       action: { token: "number" } },
				{ regex: /\b(true|false)\b/,   action: { token: "keyword" } },
				{ regex: /[{}:]/,              action: { token: "delimiter" } },
				{ regex: /[_a-zA-Z]\w*/,       action: { token: "identifier" } },
				{ regex: /\s+/,                action: { token: "white" } },
			],
		},
	} as MonacoType.languages.IMonarchLanguage);
}

function updateCompletions(monaco: typeof MonacoType, fields: FieldDef[]): void {
	completionDisposable?.dispose();
	completionDisposable = monaco.languages.registerCompletionItemProvider(
		EDITOR_LANGUAGE,
		{
			triggerCharacters: ["\n", " "],
			provideCompletionItems(model, position) {
				const word  = model.getWordUntilPosition(position);
				const range: MonacoType.IRange = {
					startLineNumber: position.lineNumber,
					endLineNumber:   position.lineNumber,
					startColumn:     word.startColumn,
					endColumn:       word.endColumn,
				};
				return {
					suggestions: fields.map((f) => ({
						label:           f.name,
						kind:            monaco.languages.CompletionItemKind.Field,
						detail:          `${f.required ? "required" : "optional"} ${f.type}`,
						insertText:      `${f.name}: ${f.type === "string" ? '""' : f.type === "number" ? "0" : "false"}`,
						insertTextRules: monaco.languages.CompletionItemInsertTextRule.None,
						range,
					})),
				};
			},
		},
	);
}

// ─── Stub + content helpers ───────────────────────────────────

function buildStub(eventTypeName: string, grammarSource: string): string {
	const lines: string[] = [`${eventTypeName} {`];
	const re = /\b(?:required|optional)\s+(\w+)\s*:\s*(string|number|boolean)/g;
	let m: RegExpExecArray | null;
	while ((m = re.exec(grammarSource)) !== null) {
		const placeholder = m[2] === "string" ? '""' : m[2] === "number" ? "0" : "false";
		lines.push(`  ${m[1]}: ${placeholder}`);
	}
	lines.push("}");
	return lines.join("\n");
}

function buildFromEvent(eventType: string, text: string, payload: Record<string, unknown>): string {
	const lines: string[] = [`${eventType} {`];
	lines.push(`  text: ${JSON.stringify(text)}`);
	for (const [k, v] of Object.entries(payload)) {
		lines.push(`  ${k}: ${JSON.stringify(v)}`);
	}
	lines.push("}");
	return lines.join("\n");
}

function parseEventContent(content: string): { text: string; payload: Record<string, unknown> } {
	const result: Record<string, unknown> = {};
	const re = /^\s*(\w+)\s*:\s*("(?:[^"\\]|\\.)*"|\d+(?:\.\d+)?|true|false)\s*$/gm;
	let m: RegExpExecArray | null;
	while ((m = re.exec(content)) !== null) {
		try { result[m[1]!] = JSON.parse(m[2]!); } catch { result[m[1]!] = m[2]; }
	}
	const text    = typeof result["text"] === "string" ? result["text"] : "";
	const payload = { ...result };
	delete payload["text"];
	return { text, payload };
}

// ─── Types ────────────────────────────────────────────────────

interface EventTypeOption {
	name:   string;
	source: string;
}

interface EventRow {
	id:         number;
	event_type: string;
	text:       string;
	payload:    Record<string, unknown>;
	closed:     boolean;
	closed_at:  string | null;
	created_at: string;
}

type PanelMode =
	| { kind: "idle" }
	| { kind: "new" }
	| { kind: "edit"; eventId: number; closed: boolean };

// ─── Component ──────────────────────────────────────────────

export function StreamDetailPanel({ mapping, sourceTable, streamId }: {
	mapping:     string;
	sourceTable: string;
	streamId:    string;
}) {
	const { loaded: settingsLoaded } = useAppSettings();

	// ── Left: event history ──
	const [events,        setEvents]        = useState<EventRow[]>([]);
	const [eventsLoading, setEventsLoading] = useState(false);
	const [sortOrder,     setSortOrder]     = useState<"desc" | "asc">("desc");

	// ── Right: panel mode ──
	const [mode, setMode] = useState<PanelMode>({ kind: "idle" });

	// ── Right: event type picker (only used in new mode) ──
	const [eventTypes,   setEventTypes]   = useState<EventTypeOption[]>([]);
	const [typesLoading, setTypesLoading] = useState(false);
	const [typeInput,    setTypeInput]    = useState("");
	const [selectedType, setSelectedType] = useState<EventTypeOption | null>(null);

	// ── Right: editor ──
	const [editorContent, setEditorContent] = useState("");
	const monacoRef = useRef<typeof MonacoType | null>(null);

	// ── Save ──
	const [saving,  setSaving]  = useState(false);
	const [saveMsg, setSaveMsg] = useState<{ ok: boolean; msg: string } | null>(null);

	// ─── Load event history ───────────────────────────────────────

	const loadEvents = useCallback(async () => {
		if (!settingsLoaded) return;
		setEventsLoading(true);
		try {
			const res = await rpc.getStreamEvents({
				mapping,
				streamId,
				limit:    100,
			});
			if (res.error) return;
			const body = JSON.parse(res.json) as { events?: EventRow[] };
			setEvents(body.events ?? []);
		} finally {
			setEventsLoading(false);
		}
	}, [settingsLoaded, mapping, streamId]);

	useEffect(() => { void loadEvents(); }, [loadEvents]);

	// Reset to idle whenever the stream changes — the previous edit
	// session points at a different stream's data.
	useEffect(() => {
		setMode({ kind: "idle" });
		setSelectedType(null);
		setTypeInput("");
		setEditorContent("");
		setSaveMsg(null);
	}, [streamId]);

	// ── Stream tag (drives event type filter) ──
	const [streamTag, setStreamTag] = useState<string | null>(null);

	useEffect(() => {
		if (!settingsLoaded) return;
		rpc.getStreamTag({ stream: streamId })
			.then((res) => setStreamTag(res.tag ?? null))
			.catch(() => setStreamTag(null));
	}, [settingsLoaded, streamId]);

	// ─── Load event types (filtered by stream tag) ───────────────────────────

	const loadTypes = useCallback(async () => {
		if (!settingsLoaded) return;
		setTypesLoading(true);
		try {
			const res = await rpc.getEventSchemas({
				mapping,
				stream:   streamId,
			});
			if (res.error) return;
			const body = JSON.parse(res.json) as { schemas?: Array<{ name: string; source: string }> };
			setEventTypes(body.schemas ?? []);
		} finally {
			setTypesLoading(false);
		}
	}, [settingsLoaded, mapping, streamId]);

	useEffect(() => { void loadTypes(); }, [loadTypes]);

	// ─── Mode transitions ─────────────────────────────────────────

	function startNew() {
		setMode({ kind: "new" });
		setSelectedType(null);
		setTypeInput("");
		setEditorContent("");
		setSaveMsg(null);
	}

	function startEdit(ev: EventRow) {
		setMode({ kind: "edit", eventId: ev.id, closed: ev.closed });
		const found = eventTypes.find((t) => t.name === ev.event_type) ?? { name: ev.event_type, source: "" };
		setSelectedType(found);
		setTypeInput(ev.event_type);
		setEditorContent(buildFromEvent(ev.event_type, ev.text, ev.payload));
		setSaveMsg(null);
		if (monacoRef.current && found.source) {
			updateCompletions(monacoRef.current, extractFields(found.source));
		}
	}

	function selectType(name: string) {
		const found = eventTypes.find((t) => t.name === name);
		if (!found) return;
		setSelectedType(found);
		setTypeInput(name);
		setEditorContent(buildStub(name, found.source));
		setSaveMsg(null);
		if (monacoRef.current) {
			updateCompletions(monacoRef.current, extractFields(found.source));
		}
	}

	// ─── Save (insert or update depending on mode) ────────────────────────

	async function handleSave() {
		if (!selectedType || !editorContent.trim()) return;
		if (mode.kind === "edit" && mode.closed)  return;
		setSaving(true);
		setSaveMsg(null);
		try {
			const parsed = parseEventContent(editorContent);
			if (mode.kind === "new") {
				const res = await rpc.insertEvent({
					mapping,
					stream:    streamId,
					eventType: selectedType.name,
					text:      parsed.text,
					payload:   parsed.payload,
				});
				if (res.ok) {
					setSaveMsg({ ok: true, msg: `Event ${selectedType.name} angelegt (id: ${res.id ?? "?"}).` });
					void loadEvents();
				} else {
					setSaveMsg({ ok: false, msg: res.error ?? "insert failed" });
				}
			} else if (mode.kind === "edit") {
				const res = await rpc.updateEvent({
					mapping,
					id:      mode.eventId,
					text:    parsed.text,
					payload: parsed.payload,
				});
				if (res.ok) {
					setSaveMsg({ ok: true, msg: `Event #${mode.eventId} aktualisiert.` });
					void loadEvents();
				} else {
					setSaveMsg({ ok: false, msg: res.error ?? "update failed" });
				}
			}
		} finally {
			setSaving(false);
		}
	}

	// ─── Close + Delete (with confirm modals) ─────────────────────────────

	function confirmClose(ev: EventRow) {
		modals.openConfirmModal({
			title: `Event #${ev.id} schliessen?`,
			children: (
				<Text size="sm">
					Geschlossene Events können nicht mehr geändert oder gelöscht werden.
					Dieser Schritt ist endgültig.
				</Text>
			),
			labels: { confirm: "Schliessen", cancel: "Abbrechen" },
			confirmProps: { color: "orange" },
			onConfirm: async () => {
				const res = await rpc.closeEvent({ mapping, id: ev.id });
				if (res.ok) {
					setSaveMsg({ ok: true, msg: `Event #${ev.id} geschlossen.` });
					void loadEvents();
					// If we were editing this event, flip mode.closed so the
					// editor goes read-only without losing context.
					if (mode.kind === "edit" && mode.eventId === ev.id) {
						setMode({ kind: "edit", eventId: ev.id, closed: true });
					}
				} else {
					setSaveMsg({ ok: false, msg: res.error ?? "close failed" });
				}
			},
		});
	}

	function confirmDelete(ev: EventRow) {
		modals.openConfirmModal({
			title: `Event #${ev.id} löschen?`,
			children: (
				<Text size="sm">
					Das Event und sein Embedding werden unwiderruflich entfernt.
				</Text>
			),
			labels: { confirm: "Löschen", cancel: "Abbrechen" },
			confirmProps: { color: "red" },
			onConfirm: async () => {
				const res = await rpc.deleteEvent({ mapping, id: ev.id });
				if (res.ok) {
					setSaveMsg({ ok: true, msg: `Event #${ev.id} gelöscht.` });
					void loadEvents();
					if (mode.kind === "edit" && mode.eventId === ev.id) {
						setMode({ kind: "idle" });
						setSelectedType(null);
						setTypeInput("");
						setEditorContent("");
					}
				} else {
					setSaveMsg({ ok: false, msg: res.error ?? "delete failed" });
				}
			},
		});
	}

	// ─── Render ───────────────────────────────────────────────────

	const typeOptions = eventTypes.map((t) => t.name);
	const editing      = mode.kind === "edit";
	const editingClosed = editing && mode.closed;
	const canSave      = !editingClosed
		&& !!selectedType
		&& editorContent.trim().length > 0
		&& (mode.kind === "new" || mode.kind === "edit");

	const saveLabel = mode.kind === "edit" ? "Aktualisieren" : "Speichern";
	const headerSub =
		mode.kind === "new"  ? "Neuer Event" :
		mode.kind === "edit" ? `Bearbeiten: #${mode.eventId}${mode.closed ? " (geschlossen)" : ""}` :
		                       `${mapping} · ${sourceTable}`;

	return (
		<Box style={{ display: "flex", flexDirection: "column", height: "100%", minHeight: 0 }}>

			{/* Header */}
			<Group
				px="md" py="xs" justify="space-between"
				style={{ borderBottom: "1px solid var(--mantine-color-default-border)", flexShrink: 0 }}
			>
				<Box>
					<Group gap="xs" align="center">
						<Title order={5}>{streamId}</Title>
						{streamTag && (
							<Badge size="sm" variant="light" color="teal">{streamTag}</Badge>
						)}
						{editingClosed && (
							<Badge size="sm" variant="light" color="gray" leftSection={<IconLock size={10} />}>
								geschlossen
							</Badge>
						)}
					</Group>
					<Text size="xs" c="dimmed">{headerSub}</Text>
				</Box>
				<Group gap="xs">
					{editing && !editingClosed && (
						<Button
							size="xs" variant="default"
							onClick={() => {
								setMode({ kind: "idle" });
								setSelectedType(null);
								setTypeInput("");
								setEditorContent("");
								setSaveMsg(null);
							}}
						>
							Abbrechen
						</Button>
					)}
					{mode.kind === "new" && (
						<Button
							size="xs" variant="default"
							onClick={() => {
								setMode({ kind: "idle" });
								setSelectedType(null);
								setTypeInput("");
								setEditorContent("");
								setSaveMsg(null);
							}}
						>
							Abbrechen
						</Button>
					)}
					{(mode.kind === "new" || (mode.kind === "edit" && !mode.closed)) && (
						<Button
							size="xs" color="green"
							onClick={handleSave}
							loading={saving}
							disabled={!canSave || saving}
						>
							{saveLabel}
						</Button>
					)}
				</Group>
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

			{/* Body: event list + content panel */}
			<Box
				style={{
					flex: 1,
					display: "grid",
					gridTemplateColumns: "260px 1fr",
					minHeight: 0,
				}}
			>
				{/* Left: event history */}
				<Box
					style={{
						borderRight: "1px solid var(--mantine-color-default-border)",
						display: "flex",
						flexDirection: "column",
						minHeight: 0,
					}}
					onContextMenu={(e) => {
						// Right-click on empty list area — only when the click
						// did not bubble up from an event item (those handle
						// their own context menu via Mantine).
						if ((e.target as HTMLElement).closest("[data-event-item]")) return;
						e.preventDefault();
						startNew();
					}}
				>
					<Group px="sm" py="xs" justify="space-between">
						<Menu shadow="md" width={180} position="bottom-start">
							<Menu.Target>
								<UnstyledButton>
									<Group gap={4}>
										<Text size="sm" fw={600} c="dimmed">Events</Text>
										<Text size="xs" c="dimmed">
											{sortOrder === "desc" ? "↓ Neueste" : "↑ Älteste"}
										</Text>
									</Group>
								</UnstyledButton>
							</Menu.Target>
							<Menu.Dropdown>
								<Menu.Label>Sortierung</Menu.Label>
								<Menu.Item
									leftSection={<IconSortDescending size={14} />}
									onClick={() => setSortOrder("desc")}
									fw={sortOrder === "desc" ? 700 : 400}
								>
									Neueste zuerst
								</Menu.Item>
								<Menu.Item
									leftSection={<IconSortAscending size={14} />}
									onClick={() => setSortOrder("asc")}
									fw={sortOrder === "asc" ? 700 : 400}
								>
									Älteste zuerst
								</Menu.Item>
							</Menu.Dropdown>
						</Menu>
						<Group gap="xs">
							<Tooltip label="Neuer Event" withArrow>
								<ActionIcon
									size="sm" variant="subtle" color="green"
									onClick={startNew}
								>
									<IconPlus size={14} />
								</ActionIcon>
							</Tooltip>
							<Button
								variant="subtle" size="compact-xs"
								leftSection={<IconRefresh size={12} />}
								onClick={loadEvents}
								disabled={eventsLoading}
							>
								{eventsLoading ? <Loader size={10} /> : "Refresh"}
							</Button>
						</Group>
					</Group>
					<Divider />
					<ScrollArea style={{ flex: 1 }}>
						<Stack gap={1} p={4}>
							{!eventsLoading && events.length === 0 && (
								<Text size="xs" c="dimmed" p="sm">
									Noch keine Events. Rechtsklick oder „+“ für neuen Event.
								</Text>
							)}
							{(sortOrder === "desc" ? events : [...events].reverse()).map((ev) => (
								<EventListItem
									key={ev.id}
									event={ev}
									active={mode.kind === "edit" && mode.eventId === ev.id}
									onEdit={() => startEdit(ev)}
									onClose={() => confirmClose(ev)}
									onDelete={() => confirmDelete(ev)}
								/>
							))}
						</Stack>
					</ScrollArea>
				</Box>

				{/* Right: content panel — idle / new / edit */}
				<Box style={{ display: "flex", flexDirection: "column", minHeight: 0 }}>

					{mode.kind === "idle" && (
						<Box p="md">
							<Stack gap="xs">
								<Text size="sm" fw={600}>Keine Auswahl</Text>
								<Text size="sm" c="dimmed">
									{events.length > 0
										? "Rechtsklick auf einen Event in der Liste öffnet das Kontextmenü (Bearbeiten, Schliessen, Löschen). Rechtsklick auf den leeren Bereich oder das „+“-Symbol startet einen neuen Event."
										: "Für diesen Stream gibt es noch keine Events. Klicke auf „+“ oder mache einen Rechtsklick auf die Event-Liste, um einen anzulegen."}
								</Text>
							</Stack>
						</Box>
					)}

					{(mode.kind === "new" || mode.kind === "edit") && (
						<>
							{/* Event type picker */}
							<Box px="md" pt="md" pb="xs" style={{ flexShrink: 0 }}>
								<Autocomplete
									label="Event Type"
									placeholder={
										typesLoading ? "Lade..." :
										typeOptions.length === 0 ? "Keine Event Types zugewiesen" :
										"Event Type auswählen..."
									}
									data={typeOptions}
									value={typeInput}
									onChange={(v) => {
										if (mode.kind === "edit") return;
										setTypeInput(v);
										if (typeOptions.includes(v)) selectType(v);
									}}
									onOptionSubmit={(v) => { if (mode.kind === "new") selectType(v); }}
									disabled={mode.kind === "edit" || typesLoading || typeOptions.length === 0}
									rightSection={typesLoading ? <Loader size={12} /> : undefined}
									comboboxProps={{ withinPortal: true }}
								/>
							</Box>

							<Divider />

							{selectedType && (
								<Text size="xs" c="dimmed" px="md" pt="xs" style={{ flexShrink: 0 }}>
									{editingClosed
										? <>Dieser Event ist <strong>geschlossen</strong> und kann nicht mehr geändert werden.</>
										: <>Grammar: <strong>{selectedType.name}</strong> — Felder nach Vorgabe ausfüllen, dann {saveLabel}.</>
									}
								</Text>
							)}

							{/* Monaco editor */}
							<Box style={{ flex: 1, minHeight: 0, overflow: "hidden", marginTop: 4 }}>
								{selectedType === null ? (
									<Box p="md">
										<Text size="sm" c="dimmed">Event Type auswählen um anzufangen.</Text>
									</Box>
								) : (
									<OnisinEditor
										value={editorContent}
										language={EDITOR_LANGUAGE}
										readOnly={editingClosed}
										onChange={(v) => { if (!editingClosed) { setEditorContent(v); setSaveMsg(null); } }}
										onMount={((_editor, monaco) => {
											monacoRef.current = monaco;
											ensureLanguageRegistered(monaco);
											if (selectedType?.source) {
												updateCompletions(monaco, extractFields(selectedType.source));
											}
										}) satisfies OnMount}
									/>
								)}
							</Box>
						</>
					)}
				</Box>
			</Box>
		</Box>
	);
}

// ─── EventListItem ──────────────────────────────────────────────
//
// Renders one event row in the history list and owns its context menu.
// The menu is a controlled Mantine Menu that anchors to a 1x1 invisible
// reference element placed at the cursor position on right-click. That
// way the menu opens exactly under the pointer and only on contextmenu
// — not on hover, not on left-click.
//
// Left click on the row calls onEdit (loads the event into the editor
// on the right). The row's onClick fires before the menu opens, so a
// stray left-click never produces the menu.

function EventListItem({ event, active, onEdit, onClose, onDelete }: {
	event:    EventRow;
	active:   boolean;
	onEdit:   () => void;
	onClose:  () => void;
	onDelete: () => void;
}) {
	const [menuPos, setMenuPos] = useState<{ x: number; y: number } | null>(null);
	const time = new Date(event.created_at).toLocaleTimeString("de-DE", {
		hour: "2-digit", minute: "2-digit",
	});

	return (
		<>
			<UnstyledButton
				data-event-item
				onClick={onEdit}
				onContextMenu={(e) => {
					e.preventDefault();
					e.stopPropagation();
					setMenuPos({ x: e.clientX, y: e.clientY });
				}}
				style={{
					display: "block",
					width: "100%",
					padding: "5px 8px",
					borderRadius: 3,
					borderLeft: active
						? "3px solid var(--mantine-color-brand-6)"
						: "3px solid transparent",
					background: active ? "var(--mantine-color-brand-0)" : "transparent",
					opacity:    event.closed ? 0.6 : 1,
					transition: "background 80ms ease",
				}}
			>
				<Group gap={4} justify="space-between" wrap="nowrap">
					<Group gap={4} wrap="nowrap" style={{ minWidth: 0 }}>
						{event.closed && (
							<Tooltip
								label={event.closed_at
									? `Geschlossen am ${new Date(event.closed_at).toLocaleString("de-DE")}`
									: "Geschlossen"}
								withArrow
							>
								<IconLock size={10} style={{ flexShrink: 0, opacity: 0.7 }} />
							</Tooltip>
						)}
						<Badge size="xs" variant="light" color="blue" style={{ flexShrink: 0, fontSize: 9 }}>
							{event.event_type}
						</Badge>
					</Group>
					<Text size="xs" c="dimmed" style={{ flexShrink: 0, fontSize: 10 }}>{time}</Text>
				</Group>
				<Text size="xs" lineClamp={1} c={active ? "indigo.7" : "dimmed"} style={{ fontSize: 11 }}>
					{event.text || JSON.stringify(event.payload)}
				</Text>
			</UnstyledButton>

			{menuPos && (
				<Menu
					opened
					onClose={() => setMenuPos(null)}
					withinPortal
					shadow="md"
					width={180}
					position="bottom-start"
					offset={0}
				>
					<Menu.Target>
						<div
							style={{
								position: "fixed",
								left:     menuPos.x,
								top:      menuPos.y,
								width:    1,
								height:   1,
								pointerEvents: "none",
							}}
						/>
					</Menu.Target>
					<Menu.Dropdown>
						<Menu.Label>Event #{event.id}</Menu.Label>
						<Menu.Item
							leftSection={<IconLock size={14} />}
							color="orange"
							disabled={event.closed}
							onClick={() => { setMenuPos(null); onClose(); }}
						>
							Schliessen
						</Menu.Item>
						<Menu.Divider />
						<Menu.Item
							leftSection={<IconTrash size={14} />}
							color="red"
							disabled={event.closed}
							onClick={() => { setMenuPos(null); onDelete(); }}
						>
							Löschen
						</Menu.Item>
					</Menu.Dropdown>
				</Menu>
			)}
		</>
	);
}
