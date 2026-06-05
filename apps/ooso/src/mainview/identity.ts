// identity.ts — fetch this install's process identity from the native shell.
//
// A webview cannot learn its own node id / host / pid, so the Tauri command
// get_identity provides them (mints + persists a stable node id on first run).
// Cached for the session: the values never change while the process lives.

import { invoke } from "@tauri-apps/api/core";

export interface Identity {
	nodeId: string;
	host: string;
	pid: number;
	version: string;
}

let cached: Identity | null = null;

/** getIdentity returns the cached identity, fetching it from the shell once. */
export async function getIdentity(): Promise<Identity> {
	if (cached) return cached;
	cached = await invoke<Identity>("get_identity");
	return cached;
}
