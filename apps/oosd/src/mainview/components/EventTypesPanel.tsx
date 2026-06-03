// EventTypesPanel.tsx — admin panel for the event_type_grammar library.
//
// Layout: two columns.
//   Left  — list of all grammar entries (mapping-independent).
//           "New" button inserts a blank entry.
//           Delete removes with confirmation.
//   Right — Monaco editor with the DSL source for the selected entry.
//           Language: onisin-event-schema (Langium-driven highlighting).
//           Save persists back to the DB.
//
// This panel manages the grammar library independently of any mapping.
// Assigning types to mappings is handled by MappingTypesPanel.

import { useCallback, useEffect, useRef, useState } from "react";
import {
	Alert,
	Box,
	Button,
	Divider,
	Group,
	Loader,
	Modal,
	ScrollArea,
	Stack,
	TagsInput,
	Text,
	TextInput,
	Title,
	UnstyledButton,
} from "@mantine/core";
import {
	IconCheck,
	IconPlus,
	IconRefresh,
	IconTrash,
	IconX,
} from "@tabler/icons-react";
import Editor, { type OnMount } from "@monaco-editor/react";

import { rpc } from "../rpc";
import type { GrammarType } from "../rpc";
import {
	registerOnisinLanguages,
	EVENT_SCHEMA_LANGUAGE_ID,
} from "../../lang/monaco/register";

// ─── Default stub ─────────────────────────────────────────────────────

function stubGrammar(name: string): string {
	return `EventType "${name}" {\n  required text: string\n}\n`;
}

// ─── Component ───────────────────────────────────────────────────────

export function EventTypesPanel({ disabled }: { disabled: boolean }) {
	// ── Left: grammar list ──
	const [types,        setTypes]        = useState<GrammarType[]>([]);
	const [typesLoading, setTypesLoading] = useState(false);
	const [selected,     setSelected]     = useState<GrammarType | null>(null);

	// ── Right: editor ──
	const [source,  setSource]  = useState("");
	const [dirty,      setDirty]      = useState(false);
	const [saving,     setSaving]     = useState(false);
	const [saveMsg,    setSaveMsg]    = useState<{ ok: boolean; msg: string } | null>(null);

	// ── Tags ──
	const [tags,       setTags]       = useState<string[]>([]);
	const [tagsDirty,  setTagsDirty]  = useState(false);
	const [tagsSaving, setTagsSaving] = useState(false);

	// ── New dialog ──
	const [newOpen,   setNewOpen]   = useState(false);
	const [newName,   setNewName]   = useState("");
	const [newError,  setNewError]  = useState<string | null>(null);
	const [inserting, setInserting] = useState(false);

	// ── Delete confirm ──
	const [deleteOpen, setDeleteOpen] = useState(false);
	const [deleting,   setDeleting]   = useState(false);

	// No Langium validation for .langium source itself — highlighting only.
	const editorRef = useRef<Parameters<OnMount>[0] | null>(null);

	// ─── Load ─────────────────────────────────────────────────────

	const loadTypes = useCallback(async () => {

		setTypesLoading(true);
		try {
			const res = await rpc.listGrammarTypes({});
			setTypes(res.types);
		} finally {
			setTypesLoading(false);
		}
	}, [disabled]);

	useEffect(() => { void loadTypes(); }, [loadTypes]);

	// ─── Select ───────────────────────────────────────────────────

	async function selectType(t: GrammarType) {
		setSelected(t);
		setSaveMsg(null);
		setDirty(false);
		setTagsDirty(false);
		setTags(Array.isArray(t.tags) ? t.tags : []);
		const res = await rpc.loadGrammarType({ name: t.name });
		setSource(res.source ?? stubGrammar(t.name));
	}

	// ─── Save ─────────────────────────────────────────────────────

	async function handleSave() {
		if (!selected) return;
		setSaving(true);
		setSaveMsg(null);
		try {
			const res = await rpc.saveGrammarType({ name: selected.name, source });
			if (res.ok) {
				setDirty(false);
				setSaveMsg({ ok: true, msg: "Saved." });
			} else {
				setSaveMsg({ ok: false, msg: res.error ?? "save failed" });
			}
		} finally {
			setSaving(false);
		}
	}

	// ─── Save tags ────────────────────────────────────────────────

	async function handleSaveTags() {
		if (!selected) return;
		setTagsSaving(true);
		try {
			const res = await rpc.saveGrammarTags({ name: selected.name, tags });
			if (res.ok) {
				setTagsDirty(false);
				// Update the list entry so tags are fresh on next select.
				setTypes((prev) =>
					prev.map((t) => t.name === selected.name ? { ...t, tags } : t),
				);
			} else {
				setSaveMsg({ ok: false, msg: res.error ?? "tags save failed" });
			}
		} finally {
			setTagsSaving(false);
		}
	}

	// ─── Insert ───────────────────────────────────────────────────

	async function handleInsert() {
		const name = newName.trim();
		if (!name) { setNewError("Name is required."); return; }
		if (types.some((t) => t.name === name)) {
			setNewError("An event type with this name already exists.");
			return;
		}
		setInserting(true);
		setNewError(null);
		try {
			const res = await rpc.insertGrammarType({ name });
			if (!res.ok) { setNewError(res.error ?? "insert failed"); return; }
			// Save stub so editor is never blank on first open.
			await rpc.saveGrammarType({ name, source: stubGrammar(name) });
			setNewOpen(false);
			setNewName("");
			await loadTypes();
			// Auto-select the new type.
			const newType: GrammarType = {
				id: 0, name, source: stubGrammar(name), tags: [], created_at: "",
			};
			setSelected(newType);
			setSource(stubGrammar(name));
			setDirty(false);
		} finally {
			setInserting(false);
		}
	}

	// ─── Delete ───────────────────────────────────────────────────

	async function handleDelete() {
		if (!selected) return;
		setDeleting(true);
		try {
			const res = await rpc.deleteGrammarType({ name: selected.name });
			if (!res.ok) {
				setSaveMsg({ ok: false, msg: res.error ?? "delete failed" });
				setDeleteOpen(false);
				return;
			}
			setSelected(null);
			setSource("");
			setTags([]);
			setDirty(false);
			setTagsDirty(false);
			setSaveMsg(null);
			setDeleteOpen(false);
			await loadTypes();
		} finally {
			setDeleting(false);
		}
	}

	// ─── Render ───────────────────────────────────────────────────

	return (
		<>
			<Box
				style={{
					display: "grid",
					gridTemplateColumns: "260px 1fr",
					height: "100%",
					minHeight: 0,
				}}
			>
				{/* Left: type list */}
				<Box
					style={{
						borderRight: "1px solid var(--mantine-color-default-border)",
						display: "flex",
						flexDirection: "column",
						minHeight: 0,
					}}
				>
					<Group px="sm" py="xs" justify="space-between">
						<Text size="sm" fw={600} c="dimmed">Event Types</Text>
						<Group gap={4}>
							<Button
								variant="subtle" size="compact-xs"
								leftSection={<IconRefresh size={12} />}
								onClick={loadTypes}
								disabled={disabled || typesLoading}
							>
								{typesLoading ? <Loader size={10} /> : "Refresh"}
							</Button>
							<Button
								variant="subtle" size="compact-xs"
								leftSection={<IconPlus size={12} />}
								onClick={() => { setNewName(""); setNewError(null); setNewOpen(true); }}
								disabled={disabled}
							>
								New
							</Button>
						</Group>
					</Group>
					<Divider />
					<ScrollArea style={{ flex: 1 }}>
						<Stack gap={0} p={4}>
							{types.length === 0 && !typesLoading && (
								<Text size="xs" c="dimmed" p="sm">No event types yet.</Text>
							)}
							{types.map((t) => (
								<SelectableRow
									key={t.id}
									label={t.name}
									active={selected?.name === t.name}
									onClick={() => void selectType(t)}
								/>
							))}
						</Stack>
					</ScrollArea>
				</Box>

				{/* Right: editor */}
				<Box style={{ display: "flex", flexDirection: "column", minHeight: 0, overflow: "hidden" }}>
					{selected === null ? (
						<Box p="md">
							<Text size="sm" c="dimmed">Select an event type to edit its Grammar.</Text>
						</Box>
					) : (
						<>
							<Group
								px="md" py="xs" justify="space-between"
								style={{ borderBottom: "1px solid var(--mantine-color-default-border)", flexShrink: 0 }}
							>
								<Group gap="xs">
									<Title order={6}>{selected.name}</Title>
									{dirty && <Text size="xs" c="dimmed">(unsaved)</Text>}
								</Group>
								<Group gap="xs">
									<Button
										size="xs" variant="subtle" color="red"
										leftSection={<IconTrash size={13} />}
										onClick={() => setDeleteOpen(true)}
										disabled={saving}
									>
										Delete
									</Button>
									<Button
										size="xs" color="green"
										onClick={handleSave}
										loading={saving}
										disabled={!dirty || saving}
									>
										Save
									</Button>
								</Group>
							</Group>

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

							{/* Tags */}
							<Box
								px="md" py="xs"
								style={{
									borderBottom: "1px solid var(--mantine-color-default-border)",
									flexShrink: 0,
								}}
							>
								<Group align="flex-end" gap="xs">
									<TagsInput
										label="Tags"
										description="Context tags — filter which streams can use this event type"
										placeholder="tag-a, tag-b, ..."
										value={tags}
										onChange={(v) => { setTags(v); setTagsDirty(true); }}
										style={{ flex: 1 }}
										splitChars={[",", " ", ";"]}
									/>
									<Button
										size="xs"
										color="blue"
										onClick={handleSaveTags}
										loading={tagsSaving}
										disabled={!tagsDirty || tagsSaving}
										mb={2}
									>
										Tags speichern
									</Button>
								</Group>
							</Box>

							<Box style={{ flex: 1, minHeight: 0, overflow: "hidden" }}>
								<Editor
									height="100%"
									language={EVENT_SCHEMA_LANGUAGE_ID}
									value={source}
									onChange={(v) => {
										setSource(v ?? "");
										setDirty(true);
										setSaveMsg(null);
									}}
									onMount={((editor) => {
										editorRef.current = editor;
										registerOnisinLanguages();
									}) satisfies OnMount}
									options={{
										minimap:              { enabled: false },
										fontSize:             13,
										wordWrap:             "on",
										scrollBeyondLastLine: false,
										automaticLayout:      true,
										lineNumbers:          "on",
									}}
								/>
							</Box>
						</>
					)}
				</Box>
			</Box>

			{/* New dialog */}
			<Modal opened={newOpen} onClose={() => setNewOpen(false)} title="New Event Type" size="sm">
				<Stack gap="sm">
					<TextInput
						label="Name"
						description="e.g. EinsatzAusgeloest — must be unique across all mappings"
						placeholder="EinsatzAusgeloest"
						value={newName}
						onChange={(e) => { setNewName(e.currentTarget.value); setNewError(null); }}
						error={newError}
						data-autofocus
					/>
					<Group justify="flex-end">
						<Button variant="subtle" onClick={() => setNewOpen(false)} disabled={inserting}>Cancel</Button>
						<Button onClick={handleInsert} loading={inserting} disabled={!newName.trim()}>Create</Button>
					</Group>
				</Stack>
			</Modal>

			{/* Delete confirm */}
			<Modal opened={deleteOpen} onClose={() => setDeleteOpen(false)} title="Delete Event Type" size="sm">
				<Stack gap="sm">
					<Text size="sm">
						Delete <strong>{selected?.name}</strong>? All mapping assignments will also be removed.
						This cannot be undone.
					</Text>
					<Group justify="flex-end">
						<Button variant="subtle" onClick={() => setDeleteOpen(false)} disabled={deleting}>Cancel</Button>
						<Button color="red" onClick={handleDelete} loading={deleting}>Delete</Button>
					</Group>
				</Stack>
			</Modal>
		</>
	);
}

// ─── SelectableRow ────────────────────────────────────────────────────

function SelectableRow({ label, active, onClick }: {
	label:   string;
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
			<Text size="sm" fw={active ? 600 : 500} lineClamp={1} c={active ? "indigo.7" : undefined}>
				{label}
			</Text>
		</UnstyledButton>
	);
}
