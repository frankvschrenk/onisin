// sql/mutation.ts — UPDATE / INSERT / DELETE statement assembly.
//
// All three mutation kinds share the same readonly-filtering and
// parameterisation conventions. Each function returns the assembled
// SQL plus the values array in placeholder order, ready for
// the DbClient to execute.
//
// Parameter placeholders are always written in Postgres style ($1,
// $2, …) here. The DbClient layer (toDialectParams) rewrites them
// to ? for dialects that require it before execution.
//
// RETURNING support: dialects that support it get a RETURNING clause
// so the mutated row comes back in a single round-trip. For others
// the resolver falls back to a follow-up SELECT by id — handled in
// sql-resolvers.ts, not here. The SqlCommand carries a `returning`
// flag so the resolver knows which path to take.

import type { DomainDef, DomainFieldDef } from "oos-dsls-ts";

import { selectColumnList } from "./select";

/** A SQL statement with its positional parameter values. */
export interface SqlCommand {
	sql: string;
	values: unknown[];
	/** The id value for follow-up SELECT when RETURNING is not supported. */
	idValue?: unknown;
}

/**
 * buildUpdate produces an UPDATE … SET … WHERE id = … RETURNING …
 * statement for a record mutation.
 *
 * Readonly fields and the id field are stripped from the SET clause
 * even if the caller supplied them — server-side enforcement, the
 * GraphQL schema also blocks them at argument level. Defence in
 * depth: a misbehaving client cannot bypass either layer alone.
 *
 * Returns undefined when no settable field was supplied; the caller
 * surfaces that as a GraphQL error.
 */
export function buildUpdate(
	domain: DomainDef,
	args: Record<string, unknown>,
): SqlCommand | undefined {
	const idVal = args.id;
	if (idVal === undefined) return undefined;

	const settable = settableFieldNames(domain);
	const setClauses: string[] = [];
	const values: unknown[] = [];
	let i = 1;

	for (const [key, val] of Object.entries(args)) {
		if (key === "id") continue;
		if (!settable.has(key)) continue;
		if (val === undefined) continue;
		setClauses.push(`${key} = $${i}`);
		values.push(val);
		i++;
	}

	if (setClauses.length === 0) return undefined;

	values.push(idVal);
	const sql =
		`UPDATE ${domain.source} SET ${setClauses.join(", ")} ` +
		`WHERE id = $${i} RETURNING ${selectColumnList(domain)}`;
	return { sql, values, idValue: idVal };
}

/**
 * buildInsert produces an INSERT INTO … (cols) VALUES (…) RETURNING …
 * statement.
 *
 * The id field is never inserted directly — the database assigns it
 * via a sequence or identity column. Readonly fields are also dropped,
 * matching the DSL semantics: created_at, updated_at and friends fill
 * themselves on the database side.
 *
 * Returns undefined when the args contain no settable field, which
 * the caller should treat as a GraphQL error.
 */
export function buildInsert(
	domain: DomainDef,
	args: Record<string, unknown>,
): SqlCommand | undefined {
	const settable = settableFieldNames(domain);
	const columns: string[] = [];
	const placeholders: string[] = [];
	const values: unknown[] = [];
	let i = 1;

	for (const [key, val] of Object.entries(args)) {
		if (!settable.has(key)) continue;
		if (val === undefined) continue;
		columns.push(key);
		placeholders.push(`$${i}`);
		values.push(val);
		i++;
	}

	if (columns.length === 0) return undefined;

	const sql =
		`INSERT INTO ${domain.source} (${columns.join(", ")}) ` +
		`VALUES (${placeholders.join(", ")}) ` +
		`RETURNING ${selectColumnList(domain)}`;
	return { sql, values };
}

/** buildDelete produces a DELETE … WHERE id = … RETURNING id statement. */
export function buildDelete(
	domain: DomainDef,
	args: Record<string, unknown>,
): SqlCommand | undefined {
	const idVal = args.id;
	if (idVal === undefined) return undefined;

	const sql = `DELETE FROM ${domain.source} WHERE id = $1 RETURNING id`;
	return { sql, values: [idVal], idValue: idVal };
}

/**
 * settableFieldNames returns the set of field names that are
 * write-eligible: not readonly and not the auto-managed `id` column.
 */
function settableFieldNames(domain: DomainDef): Set<string> {
	const out = new Set<string>();
	for (const f of domain.fields) {
		if (f.readOnly) continue;
		if (f.name === "id") continue;
		out.add(f.name);
	}
	return out;
}

// Re-export the field type for callers that want to inspect what the
// settable columns are without rebuilding the set.
export type { DomainFieldDef };
