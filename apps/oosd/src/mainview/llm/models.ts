// llm/models.ts — Renderer-side wrapper for the listModels RPC.
//
// The actual fetch happens in the bun process to keep the API key
// out of the renderer and avoid CORS issues with local endpoints.

import { rpc } from "../rpc";

/**
 * listModels asks the bun backend to fetch the OpenAI-compatible
 * /v1/models list from the configured endpoint. Returns sorted ids.
 * Throws when the backend reports an error.
 */
export async function listModels(
	baseUrl: string,
	apiKey:  string,
): Promise<string[]> {
	const result = await rpc.listModels({ baseUrl, apiKey });
	if (result.error) throw new Error(result.error);
	return result.models;
}
