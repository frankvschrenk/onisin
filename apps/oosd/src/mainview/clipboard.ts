// clipboard.ts — Clipboard access routed through the bun process.
//
// The Electrobun WebView does not expose navigator.clipboard, so all
// read/write goes via RPC to bun which uses the native system clipboard.

import { rpc } from "./rpc";

/** Read plain text from the system clipboard. Returns null if empty. */
export async function clipboardRead(): Promise<string | null> {
	const res = await rpc.clipboardRead({});
	return res.text;
}

/** Write plain text to the system clipboard. */
export async function clipboardWrite(text: string): Promise<void> {
	await rpc.clipboardWrite({ text });
}

/**
 * installMonacoClipboard wires Cmd+C/X/V in a Monaco editor instance
 * to the Bun clipboard RPC.
 *
 * The WebView has no access to navigator.clipboard in the Electrobun
 * environment. Call this inside every Monaco onMount handler.
 */
export function installMonacoClipboard(
	editor: import("monaco-editor").editor.IStandaloneCodeEditor,
	monaco: typeof import("monaco-editor"),
): void {
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
}
