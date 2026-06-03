// SourceEditor.tsx — right pane: a Monaco editor over the source
// column of the currently selected row, plus a Save button.
//
// The editor language tracks the active Kind: "onisin-domain" for
// domain rows, "onisin-view" for view rows. Both languages are
// registered with Monaco on first mount (idempotent), and every
// edit ships through a Web Worker that runs the Langium parser +
// validator and writes diagnostics back as Monaco markers.

import { useEffect, useRef, useState } from "react";
import { Box, Button, Group, Text, Tooltip } from "@mantine/core";
import { IconTable } from "@tabler/icons-react";
import Editor, { type OnMount } from "@monaco-editor/react";
import * as monaco from "monaco-editor";

import type { Kind } from "../types";
import {
	monacoLanguageFor,
	registerOnisinLanguages,
} from "../../lang/monaco/register";
import {
	clearMarkers,
	validateModel,
} from "../../lang/worker/diagnostics-client";
import { installMonacoClipboard } from "../clipboard";
import { rpc } from "../rpc";
import { useOosdSettings } from "../store/settings";

export function SourceEditor({
	kind,
	selected,
	source,
	dirty,
	saving,
	onSourceChange,
	onSave,
}: {
	kind: Kind;
	selected: string | null;
	source: string;
	dirty: boolean;
	saving: boolean;
	onSourceChange: (v: string) => void;
	onSave: () => void;
}) {
	const { settings } = useOosdSettings();
	const tableLabel = kind === "domain" ? "oos.domain" : "oos.view";
	const heading = selected
		? `${tableLabel}[${selected}]${dirty ? " •" : ""}`
		: "Vorschau";

	const [creating, setCreating] = useState(false);
	const [createMsg, setCreateMsg] = useState<{ ok: boolean; text: string } | null>(null);

	async function handleCreateTable() {
		if (!selected || !source.trim()) return;
		setCreating(true);
		setCreateMsg(null);
		const res = await rpc.createTableFromDomain({
			source,
			dbUrl: settings.dbUrl,
		});
		setCreating(false);
		setCreateMsg({
			ok:   res.ok,
			text: res.ok
				? `Table created (or already exists)`
				: res.error ?? "Unknown error",
		});
		setTimeout(() => setCreateMsg(null), 4000);
	}

	const language = monacoLanguageFor(kind);

	// Hold onto the editor instance so the effects below can reach
	// the active text model without re-rendering on every keystroke.
	const editorRef = useRef<monaco.editor.IStandaloneCodeEditor | null>(null);

	const onMount: OnMount = (editor) => {
		registerOnisinLanguages();
		editorRef.current = editor;

		// Route clipboard through bun — the WebView has no access to
		// navigator.clipboard in the Electrobun environment.
		installMonacoClipboard(editor, monaco);

		// Accept DSL drops from the chat window.
		editor.getDomNode()?.addEventListener("dragover", (e) => {
			if (e.dataTransfer?.types.includes("application/oos-dsl")) {
				e.preventDefault();
				e.dataTransfer.dropEffect = "copy";
			}
		});
		editor.getDomNode()?.addEventListener("drop", (e) => {
			const raw = e.dataTransfer?.getData("application/oos-dsl");
			if (!raw) return;
			e.preventDefault();
			try {
				const { source } = JSON.parse(raw) as { kind: string; id: string; source: string };
				editor.setValue(source);
			} catch { /* ignore */ }
		});

		// Initial validation pass so existing content is checked even
		// before the user types anything.
		const model = editor.getModel();
		if (model) validateModel(model, kind);
	};

	// Whenever the selection or kind switches, wipe stale markers
	// from the previous buffer and re-validate against the (possibly
	// new) language.
	useEffect(() => {
		const model = editorRef.current?.getModel();
		if (!model) return;
		clearMarkers(model);
		validateModel(model, kind);
	}, [selected, kind]);

	function handleChange(v: string | undefined) {
		const text = v ?? "";
		onSourceChange(text);
		const model = editorRef.current?.getModel();
		if (model) validateModel(model, kind);
	}

	return (
		<Box style={{ display: "flex", flexDirection: "column", height: "100%" }}>
			<Group
				justify="space-between"
				align="center"
				px="md"
				py="xs"
				style={{ borderBottom: "1px solid var(--mantine-color-default-border)" }}
			>
				<Group gap="xs">
					<Text size="sm" fw={500}>{heading}</Text>
					{createMsg && (
						<Text size="xs" c={createMsg.ok ? "green.6" : "red"}>
							{createMsg.text}
						</Text>
					)}
				</Group>
				<Group gap="xs">
					{kind === "domain" && (
						<Tooltip label="Create table in DB from this domain DSL" withArrow>
							<Button
								size="xs"
								variant="default"
								leftSection={<IconTable size={13} />}
								onClick={() => void handleCreateTable()}
								disabled={!selected || !source.trim() || !settings.dbUrl}
								loading={creating}
							>
								Create Table
							</Button>
						</Tooltip>
					)}
					<Button
						size="xs"
						onClick={onSave}
						disabled={!selected || !dirty}
						loading={saving}
					>
						Speichern
					</Button>
				</Group>
			</Group>
			<Box style={{ flex: 1, overflow: "hidden" }}>
				<Editor
					height="100%"
					language={language}
					value={source}
					onChange={handleChange}
					onMount={onMount}
					options={{
						minimap: { enabled: false },
						fontSize: 12,
						wordWrap: "on",
						scrollBeyondLastLine: false,
						automaticLayout: true,
						readOnly: !selected,
					}}
				/>
			</Box>
		</Box>
	);
}
