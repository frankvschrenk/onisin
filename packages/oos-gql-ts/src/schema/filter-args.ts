// schema/filter-args.ts — Per-domain GraphQL filter argument config.
//
// The argument names emitted here MUST match the suffixes that
// `oos-dsls-ts/renderer/operators.ts` returns from `operatorsForType`,
// because that's exactly what gets baked into the LLM-chunk filter
// examples. Filter args generated here vs filter args taught to the
// LLM are two views of the same source list — `operatorsForType`.
// Drift means the LLM emits queries the schema rejects.
//
// In addition to the per-operator args, the field's bare name is
// also exposed as an "equals" shortcut: `firstname: "Anna"` resolves
// the same as `firstname_eq: "Anna"`. The legacy resolver supported
// this and code in the wild relies on it.
//
// `id` is ALWAYS exposed as an Int filter, regardless of whether the
// id field is marked filterable in the DSL — a primary-key lookup is
// the most common single-record query and not having it would make
// the schema effectively unusable.

import { GraphQLInt } from "graphql";
import type { GraphQLFieldConfigArgumentMap } from "graphql";

import type { DomainDef, DomainFieldDef } from "oos-dsls-ts";
import { operatorsForType } from "oos-dsls-ts";

import { gqlScalarFor } from "../types-mapping";

/**
 * buildQueryArgs assembles the argument map for a top-level query
 * field. Always includes `id`, plus one bare-name "equals shortcut"
 * and one per-operator suffixed argument for every filterable field.
 */
export function buildQueryArgs(domain: DomainDef): GraphQLFieldConfigArgumentMap {
	const args: GraphQLFieldConfigArgumentMap = {
		id: { type: GraphQLInt },
	};

	for (const field of domain.fields) {
		if (!field.filterable) continue;
		appendFieldArgs(args, field);
	}
	return args;
}

/** appendFieldArgs adds one bare-name plus all operator-suffixed args. */
function appendFieldArgs(
	args: GraphQLFieldConfigArgumentMap,
	field: DomainFieldDef,
): void {
	const scalar = gqlScalarFor(field.type);

	// Bare-name equals shortcut. Keep separate from the operator loop
	// so a future grammar change ("equals must be opt-in") only touches
	// this line.
	args[field.name] = { type: scalar };

	for (const op of operatorsForType(field.type)) {
		args[field.name + op.suffix] = { type: scalar };
	}
}
