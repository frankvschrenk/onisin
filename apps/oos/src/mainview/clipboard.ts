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
