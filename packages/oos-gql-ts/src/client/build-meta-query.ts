// client/build-meta-query.ts — Build a combined GraphQL meta query
// from a domain definition.
//
// Detail tabs need dropdown options (roles, departments, cities, …)
// to render select widgets correctly. The options come from oosgql's
// `meta_<name>` queries — one per declared Meta on the domain. The
// frontend could fire one query per dropdown, but a single combined
// query is one round-trip instead of N and produces an envelope the
// renderer can drop straight into loadEnvelope's `meta` slot.
//
// This is the TS-side counterpart to the chunk-renderer logic in
// `oos-dsls-ts/renderer/llm-chunk.ts` (writeDropdownFieldMapping):
// the LLM is taught that the combined query is the way to fetch a
// record with its dropdowns; the detail tab does it on its own
// because by the time the user clicks a row, the LLM has already
// done its job.
//
// Why a separate file from build-mutation.ts: the meta builder is
// read-side (Query, no permissions to gate, no data map to validate)
// while build-mutation is write-side. Keeping them apart means a
// future caller that only wants to fetch options doesn't have to
// pay the import cost of the mutation surface.

import type { DomainDef } from "oos-dsls-ts";

import { metaQueryName } from "../naming";

/**
 * buildMetaQueriesForDomain returns a GraphQL query string that
 * fetches every meta source declared on the domain. Each meta is
 * fetched as `meta_<name> { value label }` — the same shape oosgql
 * exposes and that the chunk renderer teaches the LLM.
 *
 * Returns an empty string when the domain has no metas. Callers
 * should treat that as "no meta query needed" and skip the round
 * trip entirely.
 *
 * The returned query is a complete top-level operation, ready to
 * POST verbatim to /query. It looks like:
 *
 *   { meta_roles { value label } meta_cities { value label } ... }
 */
export function buildMetaQueriesForDomain(domain: DomainDef): string {
	if (domain.metas.length === 0) return "";

	const blocks: string[] = [];
	for (const m of domain.metas) {
		blocks.push(`${metaQueryName(m.name)} { value label }`);
	}
	return `{ ${blocks.join(" ")} }`;
}

/**
 * extractMetaPayload pulls the `data` envelope out of a successful
 * GraphQL response and returns it shaped for loadEnvelope's `meta`
 * slot.
 *
 * oosgql's response for the combined meta query is:
 *
 *   { "data": { "meta_roles": [{value, label}, ...], "meta_cities": [...] } }
 *
 * loadEnvelope expects:
 *
 *   { "<short-name>": [{value, label}, ...], ... }
 *
 * The `meta_` prefix is stripped on the way through so the option
 * key matches what the field's `optionsRef` points to (e.g. the
 * field carries `optionsRef: "roles"`, the option store sees
 * `roles`, not `meta_roles`).
 *
 * Returns an empty object when the response is malformed or the
 * data section is missing — that lets the caller fold the result
 * into loadEnvelope without a defensive null-check.
 */
export function extractMetaPayload(json: string): Record<string, unknown> {
	let parsed: { data?: unknown };
	try {
		parsed = JSON.parse(json) as typeof parsed;
	} catch {
		return {};
	}
	const data = parsed.data;
	if (!data || typeof data !== "object") return {};

	const out: Record<string, unknown> = {};
	for (const [key, value] of Object.entries(data as Record<string, unknown>)) {
		// Drop the `meta_` prefix; everything else passes through.
		const shortKey = key.startsWith("meta_") ? key.slice("meta_".length) : key;
		out[shortKey] = value;
	}
	return out;
}
