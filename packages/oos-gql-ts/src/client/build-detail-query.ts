// client/build-detail-query.ts — Build a GraphQL query that fetches
// one full record by id, with every field the domain declares.
//
// Detail tabs need the *complete* row, not just whatever subset of
// columns the originating list view happened to show. Without this
// the form silently inherits empty values for any field that wasn't
// projected in the list query — dropdowns then can't preselect the
// stored option, scalars come up blank, and the user can't tell
// the form from a "new" record.
//
// The query is a single root selection on the domain by its `id`
// argument. oosgql's schema builder exposes `id` as a top-level
// equality argument distinct from the suffix-style filter args
// (`firstname_contains`, `age_gt`, ...): with `id` you get a one-
// element array back (the row, or empty if not found).
//
// Why a separate file from build-meta-query.ts: same reason that
// file is separate from build-mutation.ts — read-side detail fetch,
// no permissions to gate, no input map to validate. Three small
// builders are easier to review than one big multi-purpose helper.

import type { DomainDef } from "oos-dsls-ts";

import { domainQueryName } from "../naming";

/**
 * buildDetailQuery returns a GraphQL query string that fetches one
 * record from the domain by id, projecting every field declared on
 * the domain. The shape matches what the renderer expects:
 *
 *   { person(id: 5) { id firstname lastname role department ... } }
 *
 * The id is interpolated as a numeric literal — oosgql's `id` arg
 * is `Int`, and the caller already validated the value (only fires
 * when an id is known).
 */
export function buildDetailQuery(domain: DomainDef, id: string | number): string {
	const fields = domain.fields.map((f) => f.name).join(" ");
	return `{ ${domainQueryName(domain.name)}(id: ${id}) { ${fields} } }`;
}

/**
 * extractDetailRow pulls the single row out of oosgql's response.
 *
 * Response shape:
 *
 *   { "data": { "<domain>": [ { id: 5, firstname: "...", ... } ] } }
 *
 * Returns undefined when the response is malformed, the data section
 * is missing, or the id matched no row. The caller decides what to
 * do — typically falling back to whatever stub the list view passed
 * in so the form at least renders the known fields.
 */
export function extractDetailRow(
	json:       string,
	domainName: string,
): Record<string, unknown> | undefined {
	let parsed: { data?: unknown };
	try {
		parsed = JSON.parse(json) as typeof parsed;
	} catch {
		return undefined;
	}
	const data = parsed.data;
	if (!data || typeof data !== "object") return undefined;

	const block = (data as Record<string, unknown>)[domainQueryName(domainName)];
	if (!Array.isArray(block) || block.length === 0) return undefined;

	const row = block[0];
	if (!row || typeof row !== "object") return undefined;
	return row as Record<string, unknown>;
}
