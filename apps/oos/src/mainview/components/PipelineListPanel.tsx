// PipelineListPanel.tsx — browse, create, edit and delete pipelines in oos.
//
// Layout mirrors oosd PipelinePanel: list left, Monaco editor right.
// Pipelines are persisted via rpc.savePipeline / rpc.deletePipeline
// which tunnel through NATS to oosai (oos.cmd.pipeline.save/delete).
// The "Ausführen" button opens a pipeline_run tab for the execution result.

import { useCallback, useEffect, useRef, useState } from "react";
import {
	ActionIcon,
	Alert,
	Box,
	Button,
	Group,
	Loader,
	ScrollArea,
	Stack,
	Text,
	TextInput,
	Tooltip,
} from "@mantine/core";
import {
	IconAlertCircle,
	IconDeviceFloppy,
	IconPlayerPlay,
	IconPlus,
	IconTimeline,
	IconTrash,
} from "@tabler/icons-react";
import type { OnMount }    from "@monaco-editor/react";
import type * as monacoType from "monaco-editor";
import { OnisinEditor }            from "./OnisinEditor";
import { PipelineExamplesDrawer }  from "./PipelineExamplesDrawer";

import { rpc }              from "../rpc";
import { openPipelineRun }  from "../store/tabs";

// ── Types ───────────────────────────────────────────────────────────────────

interface PipelineRow {
	name:   string;
	source: string;
}

// ── Default template ─────────────────────────────────────────────────────────

function newTemplate(name: string): string {
	return [
		`pipeline "${name}" {`,
		``,
		`  source db   main`,
		``,
		`  mode    context`,
		`  llm     "gemma4:26b"`,
		`  embedding "bge-m3:latest"`,
		``,
		`  step llm analyse {`,
		`    from   main`,
		`    mode   aggregate`,
		`    prompt "Analysiere die Daten und erstelle eine Zusammenfassung"`,
		`  }`,
		``,
		`  out editor`,
		`}`,
	].join("\n");
}

// ── Component ────────────────────────────────────────────────────────────────

export function PipelineListPanel() {
	const [pipelines, setPipelines] = useState<PipelineRow[]>([]);
	const [selected,  setSelected]  = useState<string | null>(null);
	const [source,    setSource]    = useState("");
	const [dirty,     setDirty]     = useState(false);
	const [saving,    setSaving]    = useState(false);
	const [loading,   setLoading]   = useState(false);
	const [error,     setError]     = useState<string | null>(null);
	const [newName,   setNewName]   = useState("");
	const [creating,  setCreating]  = useState(false);

	const editorRef = useRef<monacoType.editor.IStandaloneCodeEditor | null>(null);

	// ── Load ────────────────────────────────────────────────────────────────
	const loadList = useCallback(async () => {
		setLoading(true);
		try {
			const res = await rpc.listPipelines({});
			if (res.error) { setError(res.error); return; }
			setPipelines(res.pipelines.map(p => ({ name: p.name, source: p.source })));
		} catch (err) {
			setError(err instanceof Error ? err.message : String(err));
		} finally {
			setLoading(false);
		}
	}, []);

	useEffect(() => { void loadList(); }, [loadList]);

	// ── Select ──────────────────────────────────────────────────────────────
	const handleSelect = useCallback((name: string) => {
		const row = pipelines.find(p => p.name === name);
		if (!row) return;
		setSelected(name);
		setSource(row.source);
		setDirty(false);
		setError(null);
	}, [pipelines]);

	// ── Create ──────────────────────────────────────────────────────────────
	const handleCreate = useCallback(async () => {
		const name = newName.trim();
		if (!name) return;
		if (pipelines.some(p => p.name === name)) {
			setError(`Pipeline "${name}" already exists.`);
			return;
		}
		const src = newTemplate(name);
		const res = await rpc.savePipeline({ name, source: src });
		if (!res.ok) { setError(res.error ?? "save failed"); return; }
		setPipelines(prev => [...prev, { name, source: src }].sort((a, b) => a.name.localeCompare(b.name)));
		setSelected(name);
		setSource(src);
		setDirty(false);
		setNewName("");
		setCreating(false);
		setError(null);
	}, [newName, pipelines]);

	// ── Delete ──────────────────────────────────────────────────────────────
	const handleDelete = useCallback(async (name: string) => {
		const res = await rpc.deletePipeline({ name });
		if (!res.ok) { setError(res.error ?? "delete failed"); return; }
		setPipelines(prev => prev.filter(p => p.name !== name));
		if (selected === name) { setSelected(null); setSource(""); setDirty(false); }
	}, [selected]);

	// ── Save ────────────────────────────────────────────────────────────────
	const handleSave = useCallback(async () => {
		if (!selected || !dirty) return;
		setSaving(true);
		try {
			const res = await rpc.savePipeline({ name: selected, source });
			if (!res.ok) { setError(res.error ?? "save failed"); return; }
			setPipelines(prev => prev.map(p => p.name === selected ? { ...p, source } : p));
			setDirty(false);
			setError(null);
		} catch (err) {
			setError(err instanceof Error ? err.message : String(err));
		} finally {
			setSaving(false);
		}
	}, [selected, dirty, source]);

	// ── Monaco mount — Cmd/Ctrl+S speichert ────────────────────────────────
	const onMount: OnMount = (editor, monaco) => {
		editorRef.current = editor;
		// eslint-disable-next-line no-bitwise
		editor.addCommand(
			monaco.KeyMod.CtrlCmd | monaco.KeyCode.KeyS,
			() => { void handleSave(); },
		);
	};

	// ── Render ───────────────────────────────────────────────────────────────
	return (
		<Box style={{ display: "flex", height: "100%", overflow: "hidden" }}>

			{/* Left: list */}
			<Box style={{
				width: 220, flexShrink: 0, display: "flex", flexDirection: "column",
				borderRight: "1px solid var(--mantine-color-default-border)",
			}}>
				<Box px="sm" py="xs" style={{ borderBottom: "1px solid var(--mantine-color-default-border)", flexShrink: 0 }}>
					<Group justify="space-between" align="center">
						<Text size="sm" fw={500}>Pipelines</Text>
						<Group gap={4}>
							{loading && <Loader size={12} />}
							<PipelineExamplesDrawer />
							<Tooltip label="New pipeline" withArrow>
								<ActionIcon size="sm" variant="subtle" onClick={() => setCreating(true)}>
									<IconPlus size={14} />
								</ActionIcon>
							</Tooltip>
						</Group>
					</Group>
				</Box>

				{creating && (
					<Box px="sm" py="xs" style={{ flexShrink: 0 }}>
						<TextInput
							size="xs"
							placeholder="Pipeline name"
							value={newName}
							autoFocus
							onChange={(e) => setNewName(e.currentTarget.value)}
							onKeyDown={(e) => {
								if (e.key === "Enter")  void handleCreate();
								if (e.key === "Escape") { setCreating(false); setNewName(""); }
							}}
						/>
					</Box>
				)}

				<ScrollArea style={{ flex: 1 }}>
					<Stack gap={0} p="xs">
						{pipelines.length === 0 && !loading && (
							<Text size="xs" c="dimmed" p="xs">No pipelines yet.</Text>
						)}
						{pipelines.map(p => (
							<Group
								key={p.name}
								justify="space-between"
								px="xs" py={4}
								style={{
									borderRadius: 4,
									cursor: "pointer",
									background: selected === p.name
										? "var(--mantine-color-blue-light)"
										: "transparent",
								}}
								onClick={() => handleSelect(p.name)}
							>
								<Group gap={6} style={{ flex: 1, minWidth: 0 }}>
									<IconTimeline size={14} style={{ flexShrink: 0 }} />
									<Text size="xs" truncate style={{ maxWidth: 120 }}>{p.name}</Text>
								</Group>
								<ActionIcon
									size="xs" variant="subtle" color="red"
									onClick={(e) => { e.stopPropagation(); void handleDelete(p.name); }}
								>
									<IconTrash size={12} />
								</ActionIcon>
							</Group>
						))}
					</Stack>
				</ScrollArea>
			</Box>

			{/* Right: Monaco editor */}
			<Box style={{ flex: 1, display: "flex", flexDirection: "column", overflow: "hidden" }}>

				<Box px="md" py="xs" style={{ flexShrink: 0, borderBottom: "1px solid var(--mantine-color-default-border)" }}>
					<Group justify="space-between" align="center">
						<Text size="sm" fw={500}>
							{selected
								? <>{selected}{dirty && <Text span c="dimmed" size="xs"> · unsaved</Text>}</>
								: "Pipeline Editor"
							}
						</Text>
						<Group gap="xs">
							<Button
								size="xs" variant="light"
								leftSection={saving ? <Loader size={12} /> : <IconDeviceFloppy size={14} />}
								disabled={!dirty || saving || !selected}
								onClick={() => void handleSave()}
							>
								Save
							</Button>
							<Button
								size="xs"
								leftSection={<IconPlayerPlay size={14} />}
								disabled={!selected}
								onClick={() => selected && openPipelineRun(selected)}
							>
								Ausführen
							</Button>
						</Group>
					</Group>
				</Box>

				{error && (
					<Box px="md" pt="xs" style={{ flexShrink: 0 }}>
						<Alert icon={<IconAlertCircle size={14} />} color="red" py={6}>{error}</Alert>
					</Box>
				)}

				{!selected && (
					<Box style={{
						flex: 1, display: "flex", alignItems: "center",
						justifyContent: "center", flexDirection: "column",
						gap: 8,
					}}>
						<IconTimeline size={32} opacity={0.3} />
						<Text size="sm" c="dimmed">Select or create a pipeline</Text>
					</Box>
				)}

				{selected && (
					<Box style={{ flex: 1, overflow: "hidden" }}>
						<OnisinEditor
							value={source}
							onChange={(v) => { setSource(v); setDirty(true); }}
							onMount={onMount}
							language="onisin-pipeline"
						/>
					</Box>
				)}
			</Box>
		</Box>
	);
}
