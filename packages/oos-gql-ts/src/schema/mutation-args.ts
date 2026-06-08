// schema/mutation-args.ts — Argument maps for update / insert / delete.
//
// Co-located with the SQL builders' write-eligibility logic but
// separately exposed so the schema layer stays pure GraphQL with no
// SQL imports. Both the args generator here and the SQL builder in
// `sql/mutation.ts` treat readonly fields and the `id` field the
// same way, by design — defence in depth: a misbehaving caller
// cannot bypass either layer alone.

import { GraphQLInt, GraphQLNonNull } from "graphql";
import type { GraphQLFieldConfigArgumentMap } from "graphql";

import type { DomainDef } from "oos-dsls-ts";

import { gqlScalarFor } from "../types-mapping";

/**
 * buildMutationArgs returns the args for `update_<domain>`: id (Int!,
 * required) plus every settable column. Every column is optional so
 * partial updates work — the SQL builder only sets columns whose args
 * were actually supplied.
 */
export function buildMutationArgs(domain: DomainDef): GraphQLFieldConfigArgumentMap {
	const args: GraphQLFieldConfigArgumentMap = {
		id: { type: new GraphQLNonNull(GraphQLInt) },
	};
	for (const f of domain.fields) {
		if (f.readOnly) continue;
		if (f.name === "id") continue;
		args[f.name] = { type: gqlScalarFor(f.type) };
	}
	return args;
}

/**
 * buildInsertArgs returns the args for `insert_<domain>`: every
 * settable column, all optional. The legacy resolver had no
 * required fields on insert; the database defaults handle missing
 * NOT-NULL columns. Callers that want strictness can layer a
 * validator on top — keeping required-ness out of the schema means
 * a single .domain change doesn't ripple into the type system.
 */
export function buildInsertArgs(domain: DomainDef): GraphQLFieldConfigArgumentMap {
	const args: GraphQLFieldConfigArgumentMap = {};
	for (const f of domain.fields) {
		if (f.readOnly) continue;
		if (f.name === "id") continue;
		args[f.name] = { type: gqlScalarFor(f.type) };
	}
	return args;
}

/** buildDeleteArgs returns `{ id: Int! }`. */
export function buildDeleteArgs(): GraphQLFieldConfigArgumentMap {
	return {
		id: { type: new GraphQLNonNull(GraphQLInt) },
	};
}
