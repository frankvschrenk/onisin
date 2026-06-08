// OnisinEditor.tsx — shared Monaco editor wrapper for all oos panels.
//
// Centralises the common options (minimap off, fontSize 13, wordWrap on,
// scrollBeyondLastLine off, automaticLayout on, tabSize 2) so every panel
// uses identical defaults and deviations are explicit via props.
//
// Copy/Cut/Paste is routed through bun via clipboardRead/clipboardWrite
// because the Electrobun WebView does not expose navigator.clipboard.

import { useRef }          from "react";
import Editor, { type OnMount } from "@monaco-editor/react";
import * as monaco         from "monaco-editor";

import { clipboardRead, clipboardWrite } from "../clipboard";

interface OnisinEditorProps {
	/** Content to display. */
	value:        string;
	/** Called when the user edits. Omit to make read-only. */
	onChange?:    (value: string) => void;
	/** When true the editor is not editable. Default: false. */
	readOnly?:    boolean;
	/** Monaco language id. Default: "plaintext". */
	language?:    string;
	/** CSS height passed to Monaco. Default: "100%". */
	height?:      string | number;
	/** Show line numbers. Default: "on". */
	lineNumbers?: "on" | "off" | "relative";
	/** Called after the editor mounts, AFTER clipboard commands are wired.
	 *  Use for additional keybindings (e.g. Cmd+S). */
	onMount?:     OnMount;
}

/**
 * OnisinEditor is the single Monaco wrapper used across all oos panels.
 * All panels share the same base configuration; overrides are explicit.
 */
export function OnisinEditor({
	value,
	onChange,
	readOnly   = false,
	language   = "plaintext",
	height     = "100%",
	lineNumbers = "on",
	onMount: externalOnMount,
}: OnisinEditorProps) {
	const editorRef = useRef<monaco.editor.IStandaloneCodeEditor | null>(null);

	const handleMount: OnMount = (editor, monacoInstance) => {
		editorRef.current = editor;

		// Route clipboard through bun — navigator.clipboard is not
		// available in the Electrobun WebView environment.
		editor.addCommand(monaco.KeyMod.CtrlCmd | monaco.KeyCode.KeyC, async () => {
			const sel = editor.getSelection();
			if (!sel) return;
			const text = editor.getModel()?.getValueInRange(sel) ?? "";
			if (text) await clipboardWrite(text);
		});

		editor.addCommand(monaco.KeyMod.CtrlCmd | monaco.KeyCode.KeyX, async () => {
			const sel = editor.getSelection();
			if (!sel) return;
			const text = editor.getModel()?.getValueInRange(sel) ?? "";
			if (text) {
				await clipboardWrite(text);
				editor.executeEdits("clipboard", [{ range: sel, text: "" }]);
			}
		});

		editor.addCommand(monaco.KeyMod.CtrlCmd | monaco.KeyCode.KeyV, async () => {
			const text = await clipboardRead();
			if (text == null) return;
			const sel = editor.getSelection();
			if (sel) editor.executeEdits("clipboard", [{ range: sel, text }]);
		});

		// Call the panel-specific onMount after clipboard is wired.
		externalOnMount?.(editor, monacoInstance);
	};

	return (
		<Editor
			height={height}
			language={language}
			value={value}
			onChange={(v) => onChange?.(v ?? "")}
			onMount={handleMount}
			options={{
				readOnly:             readOnly || !onChange,
				minimap:              { enabled: false },
				fontSize:             13,
				wordWrap:             "on",
				scrollBeyondLastLine: false,
				automaticLayout:      true,
				tabSize:              2,
				lineNumbers,
				padding:              { top: 8, bottom: 8 },
			}}
		/>
	);
}
