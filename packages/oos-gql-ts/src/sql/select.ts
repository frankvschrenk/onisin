// sql/select.ts — SELECT statement assembly for query resolvers.
//
// Translates the GraphQL filter arguments into a parameterised SQL
// WHERE clause. The mapping from operator suffix → SQL fragment is
// the SQL-side counterpart to the operator catalog in
// `oos-dsls-ts/renderer/operators.ts`. They share the same suffix
// vocabulary; this module knows what each suffix MEANS in SQL.
//
// Placeholders are written in Postgres style ($1, $2, …). The
// DbClient layer (toDialectParams in db-client.ts) rewrites them to
// ? for dialects that require it before the query reaches the driver.

import type { DomainDef, DomainFieldDef } from "oos-dsls-ts";

/** One assembled WHERE fragment plus its values, in placeholder order. */
export interface WhereClause {
	/** Empty string when there are no conditions, else " WHERE a=$1 AND b<$2". */
	sql: string;
	values: unknown[];
}

/**
 * buildWhereClause turns GraphQL filter args into a SQL WHERE.
 *
 * Argument shapes (matching schema/filter-args.ts):
 *   - `id`              → `id = $n`
 *   - `<field>`         → `<field> = $n`              (equals shortcut)
 *   - `<field>_eq`      → `<field> = $n`
 *   - `<field>_ne`      → `<field> <> $n`
 *   - `<field>_gt`      → `<field> > $n`
 *   - `<field>_gte`     → `<field> >= $n`
 *   - `<field>_lt`      → `<field> < $n`
 *   - `<field>_lte`     → `<field> <= $n`
 *   - `<field>_contains`→ `<field> ILIKE $n`  (value wrapped %…%)
 *
 * Unknown args are ignored — they cannot reach this point through the
 * GraphQL layer, but the defensive skip keeps the helper robust to
 * future arg additions on the schema side.
 */
export function buildWhereClause(
	domain: DomainDef,
	args: Record<string, unknown>,
): WhereClause {
	const fieldByName = new Map<string, DomainFieldDef>();
	for (const f of domain.fields) fieldByName.set(f.name, f);

	const conditions: string[] = [];
	const values: unknown[] = [];
	let i = 1;

	// Special-case: id is always exposed even when the DSL doesn't
	// mark id as filterable.
	if (args.id !== undefined) {
		conditions.push(`id = $${i}`);
		values.push(args.id);
		i++;
	}

	for (const [argName, argVal] of Object.entries(args)) {
		if (argVal === undefined || argVal === null) continue;
		if (argName === "id") continue; // already handled

		const decoded = decodeFilterArg(argName, fieldByName);
		if (!decoded) continue;

		const { field, op } = decoded;
		switch (op) {
			case "eq":
				conditions.push(`${field.name} = $${i}`);
				values.push(argVal);
				i++;
				break;
			case "ne":
				conditions.push(`${field.name} <> $${i}`);
				values.push(argVal);
				i++;
				break;
			case "gt":
				conditions.push(`${field.name} > $${i}`);
				values.push(argVal);
				i++;
				break;
			case "gte":
				conditions.push(`${field.name} >= $${i}`);
				values.push(argVal);
				i++;
				break;
			case "lt":
				conditions.push(`${field.name} < $${i}`);
				values.push(argVal);
				i++;
				break;
			case "lte":
				conditions.push(`${field.name} <= $${i}`);
				values.push(argVal);
				i++;
				break;
			case "contains":
				conditions.push(`${field.name} ILIKE $${i}`);
				values.push(`%${String(argVal)}%`);
				i++;
				break;
		}
	}

	if (conditions.length === 0) {
		return { sql: "", values: [] };
	}
	return { sql: ` WHERE ${conditions.join(" AND ")}`, values };
}

/**
 * decodeFilterArg parses an argument name and resolves it to a field
 * plus operator. Handles both the bare-name equals shortcut and the
 * suffixed forms.
 *
 * Returns undefined when the arg name doesn't match any field, so the
 * caller can skip it cleanly.
 */
function decodeFilterArg(
	argName: string,
	fieldByName: Map<string, DomainFieldDef>,
): { field: DomainFieldDef; op: SqlOp } | undefined {
	// Bare-name equals shortcut.
	const direct = fieldByName.get(argName);
	if (direct) return { field: direct, op: "eq" };

	// Suffix match. Iterate over known suffixes in length-descending
	// order so `_gte`/`_lte` win over `_gt`/`_lt` — otherwise a field
	// named `xxx` with arg `xxx_gte` would parse as field=`xxx_g` op=`te`,
	// which is nonsense, but defensive matching avoids the trap entirely.
	for (const { suffix, op } of SUFFIXES) {
		if (!argName.endsWith(suffix)) continue;
		const fieldName = argName.slice(0, -suffix.length);
		const field = fieldByName.get(fieldName);
		if (field) return { field, op };
	}
	return undefined;
}

/** Suffix-to-op table, ordered longest first for unambiguous matching. */
const SUFFIXES: Array<{ suffix: string; op: SqlOp }> = [
	{ suffix: "_contains", op: "contains" },
	{ suffix: "_gte", op: "gte" },
	{ suffix: "_lte", op: "lte" },
	{ suffix: "_eq", op: "eq" },
	{ suffix: "_ne", op: "ne" },
	{ suffix: "_gt", op: "gt" },
	{ suffix: "_lt", op: "lt" },
];

/** Internal SQL operator ids; not exposed in the package public API. */
type SqlOp = "eq" | "ne" | "gt" | "gte" | "lt" | "lte" | "contains";

/**
 * selectColumnList returns the comma-joined column list to put after
 * SELECT. Currently every declared field; future relation projection
 * will plug in here.
 */
export function selectColumnList(domain: DomainDef): string {
	return domain.fields.map((f) => f.name).join(", ");
}
