// llm/models.ts — Renderer-side wrapper for the listModels RPC.
//
// The actual fetch happens in the bun process (see src/bun/llm.ts)
// to keep the API key out of the renderer bundle and to avoid the
// CORS rejection that local LLM endpoints serve to webview-origin
// requests by default.
//
// This thin wrapper exists so the SettingsDrawer keeps the same
// async-string-list contract it had before the refactor.

import { rpc } from "../rpc";

/**
 * listModels asks the bun backend to fetch the OpenAI-compatible
 * `/v1/models` list from a configured endpoint. Returns sorted ids.
 * Throws when the backend reports an error, so the drawer's
 * existing try/catch still works.
 */
export async function listModels(
	baseUrl: string,
	apiKey: string,
): Promise<string[]> {
	const result = await rpc.listModels({ baseUrl, apiKey });
	if (result.error) {
		throw new Error(result.error);
	}
	return result.models;
}
