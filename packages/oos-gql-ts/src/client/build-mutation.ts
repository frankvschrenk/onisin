// client/build-mutation.ts — Build a GraphQL mutation string from a
// domain plus a data map.
//
// The LLM tool loop produces a JSON object describing what the user
// wants to save: { firstname: "Anna", age: 42, ... } or
// { id: 17, firstname: "Anna", ... }. The frontend turns that object
// into a GraphQL mutation string and posts it to oosgql.
//
// This module is the deterministic translator. It is the TS port of
// the legacy `gql.BuildMutationFromMap` in oos-common_old, but built
// against the new DomainDef so the field-type lookup is local and
// explicit — no global AST state.
//
// Verb selection: when `data.id` is missing or empty (null, "", "0",
// 0), we render `insert_<domain>(...)`. Otherwise `update_<domain>(
// id: ..., ...)`. This matches the legacy convention and what the
// LLM-chunk renderer teaches the model.
//
// Readonly fields are stripped from the argument list — the GraphQL
// schema also rejects them at args level, but stripping client-side
// avoids round-trips on a misbehaving model. Readonly fields stay in
// the RETURNING set so the caller can refresh its local view.
//
// Unknown fields throw. The whole point of this layer is to surface
// LLM hallucinations as explicit errors that the tool loop can feed
// back into the model — silently dropping unknown keys would mask
// the bug and produce mutations that look successful but persist the
// wrong shape.

import type { DomainDef, DomainFieldDef, FieldTypeDef } from "oos-dsls-ts";

import { mutationFieldName } from "../naming";

/**
 * BuildMutationResult is the result of building a mutation: the raw
 * GraphQL string ready to POST, plus the verb that was chosen so
 * callers can inspect what happened without re-parsing.
 */
export interface BuildMutationResult {
	query: string;
	verb: "insert" | "update";
	returnFields: string[];
}

/**
 * buildMutationFromMap renders an `insert_<domain>` or
 * `update_<domain>` GraphQL mutation from a domain definition and a
 * data map.
 *
 * Behaviour:
 *
 *   - `data.id` empty → INSERT, `id` is added to RETURNING.
 *   - `data.id` non-empty → UPDATE, `id` is the only WHERE arg.
 *   - readonly fields (incl. `id` on update) are removed from args
 *     but stay in RETURNING so the caller can refresh from the
 *     server's authoritative response.
 *   - unknown fields → throw. LLM hallucinations must be visible.
 *   - empty arg list (after readonly-strip) → throw. A no-op
 *     mutation is always a programming error.
 */
export function buildMutationFromMap(
	domain: DomainDef,
	data: Record<string, unknown>,
): BuildMutationResult {
	if (Object.keys(data).length === 0) {
		throw new Error(`buildMutationFromMap: empty data for domain "${domain.name}"`);
	}

	const fieldsByName = indexFields(domain);

	// Validate first — fail fast on hallucinations before we render
	// anything. `id` is a synthetic key handled separately, so it's
	// not required to appear in the field list.
	for (const key of Object.keys(data)) {
		if (key === "id") continue;
		if (!fieldsByName.has(key)) {
			throw new Error(
				`buildMutationFromMap: unknown field "${key}" on domain "${domain.name}"`,
			);
		}
	}

	const isUpdate = !isEmptyId(data.id);
	const verb: "insert" | "update" = isUpdate ? "update" : "insert";
	const mutName = mutationFieldName(verb, domain.name);

	const args: string[] = [];
	const returnFields: string[] = [];

	if (isUpdate) {
		args.push(`id: ${formatIntLiteral(data.id)}`);
	} else {
		// INSERT always returns the freshly-assigned id.
		returnFields.push("id");
	}

	for (const [key, value] of Object.entries(data)) {
		if (key === "id") continue;

		// Every supplied key (readonly or not) goes into RETURNING so
		// the caller's local copy can refresh from the server's
		// authoritative response — including server-managed columns
		// like updated_at if they were in the originally-rendered
		// shape.
		returnFields.push(key);

		const field = fieldsByName.get(key)!;
		if (field.readOnly) continue;

		args.push(formatArg(key, value, field.type));
	}

	if (args.length === 0) {
		throw new Error(
			`buildMutationFromMap: no settable fields for ${verb}_${domain.name}`,
		);
	}

	const query = renderMutationString(mutName, args, returnFields);
	return { query, verb, returnFields };
}

/**
 * buildDeleteMutation renders a `delete_<domain>(id: N)` GraphQL
 * mutation. Used by the detail tab's Delete button after the user
 * confirms in a dialog.
 *
 * The id value gets the same lenient coercion as buildMutationFromMap
 * — an LLM-emitted string "5" is as acceptable as a number 5. Empty
 * ids throw, since deleting an empty id would either be a no-op or
 * a destructive accident depending on the backend, and neither is
 * what the caller wants.
 */
export function buildDeleteMutation(
	domain: DomainDef,
	id:     unknown,
): string {
	if (isEmptyId(id)) {
		throw new Error(`buildDeleteMutation: empty id for domain "${domain.name}"`);
	}
	const mutName = mutationFieldName("delete", domain.name);
	// oosgql's schema declares `delete_<domain>` returning the row's
	// object type, so the mutation requires a subfield selection.
	// We project `id` to keep the response small and unambiguous —
	// the caller doesn't read the response on success anyway, but a
	// non-empty selection is required for the query to parse at all.
	return `mutation {\n  ${mutName}(id: ${formatIntLiteral(id)}) {\n    id\n  }\n}`;
}

// ─── Internals ───────────────────────────────────────────────────────

/** indexFields builds a name → field lookup once per call. */
function indexFields(domain: DomainDef): Map<string, DomainFieldDef> {
	const out = new Map<string, DomainFieldDef>();
	for (const f of domain.fields) {
		out.set(f.name, f);
	}
	return out;
}

/**
 * isEmptyId returns true when an id value should be treated as
 * "absent" — meaning the caller wants an INSERT. Mirrors the legacy
 * Go behaviour: missing, null, "", "0", 0 all count as empty.
 */
function isEmptyId(v: unknown): boolean {
	if (v === undefined || v === null) return true;
	if (typeof v === "number") return v === 0;
	if (typeof v === "string") {
		const trimmed = v.trim();
		return trimmed === "" || trimmed === "0";
	}
	if (typeof v === "bigint") return v === 0n;
	return false;
}

/**
 * formatIntLiteral coerces an id-like value into its integer GraphQL
 * literal. Strings get parsed, numbers are floored, anything else
 * triggers an error — id is contractually an int in our schema.
 */
function formatIntLiteral(v: unknown): number {
	if (typeof v === "number") return Math.trunc(v);
	if (typeof v === "bigint") return Number(v);
	if (typeof v === "string") {
		const n = parseInt(v.trim(), 10);
		if (Number.isFinite(n)) return n;
	}
	throw new Error(`buildMutationFromMap: id is not an integer (got ${typeof v})`);
}

/**
 * formatArg renders a single `name: value` pair as a GraphQL
 * argument. The DSL field type determines the literal form:
 *
 *   - int          → integer literal
 *   - float        → float literal
 *   - bool         → `true` / `false`
 *   - string/text  → quoted, escaped string
 *   - date         → quoted ISO date string (we pass through)
 *   - datetime     → quoted ISO timestamp (we pass through)
 *
 * Mismatched JS types are coerced where the coercion is unambiguous;
 * otherwise we throw, because guessing produces silent data
 * corruption.
 */
function formatArg(name: string, value: unknown, type: FieldTypeDef): string {
	switch (type) {
		case "int":
			return `${name}: ${formatIntLiteral(value)}`;
		case "float":
			return `${name}: ${formatFloatLiteral(value)}`;
		case "bool":
			return `${name}: ${formatBoolLiteral(value)}`;
		case "string":
		case "text":
		case "date":
		case "datetime":
			return `${name}: "${escapeGraphQLString(stringify(value))}"`;
		default: {
			// Unreachable in normal grammar; defensive for forward
			// compat. Treat unknown types as string — same fallback
			// as gqlScalarFor in types-mapping.ts.
			const exhaustive: never = type;
			void exhaustive;
			return `${name}: "${escapeGraphQLString(stringify(value))}"`;
		}
	}
}

/** formatFloatLiteral coerces a value into a finite JS number. */
function formatFloatLiteral(v: unknown): number {
	if (typeof v === "number" && Number.isFinite(v)) return v;
	if (typeof v === "string") {
		// Accept both "1,5" and "1.5" — the LLM has seen both.
		const cleaned = v.trim().replace(",", ".");
		const n = parseFloat(cleaned);
		if (Number.isFinite(n)) return n;
	}
	throw new Error(`buildMutationFromMap: cannot coerce to float (got ${typeof v})`);
}

/**
 * formatBoolLiteral coerces a value into "true" or "false". Accepts
 * native booleans plus the common string forms the LLM might emit.
 */
function formatBoolLiteral(v: unknown): "true" | "false" {
	if (typeof v === "boolean") return v ? "true" : "false";
	if (typeof v === "number") return v === 0 ? "false" : "true";
	if (typeof v === "string") {
		const s = v.trim().toLowerCase();
		if (s === "true" || s === "1" || s === "yes") return "true";
		if (s === "false" || s === "0" || s === "no" || s === "") return "false";
	}
	throw new Error(`buildMutationFromMap: cannot coerce to bool (got ${typeof v})`);
}

/** stringify renders any value into its plain string form. */
function stringify(v: unknown): string {
	if (v === null || v === undefined) return "";
	if (typeof v === "string") return v;
	return String(v);
}

/**
 * escapeGraphQLString escapes the characters that would break a
 * double-quoted GraphQL string literal: backslash, double quote,
 * newline, carriage return, tab.
 */
function escapeGraphQLString(s: string): string {
	let out = "";
	for (const ch of s) {
		switch (ch) {
			case "\\":
				out += "\\\\";
				break;
			case '"':
				out += '\\"';
				break;
			case "\n":
				out += "\\n";
				break;
			case "\r":
				out += "\\r";
				break;
			case "\t":
				out += "\\t";
				break;
			default:
				out += ch;
		}
	}
	return out;
}

/**
 * renderMutationString stitches the final mutation string together.
 * Format matches the legacy Go renderer byte-for-byte so tests that
 * compare rendered strings stay portable.
 */
function renderMutationString(
	mutName: string,
	args: string[],
	returnFields: string[],
): string {
	return (
		`mutation {\n  ${mutName}(${args.join(", ")}) {\n    ` +
		`${returnFields.join("\n    ")}\n  }\n}`
	);
}
