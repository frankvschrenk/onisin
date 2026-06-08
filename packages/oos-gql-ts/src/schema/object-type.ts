// schema/object-type.ts — DomainDef → GraphQLObjectType.
//
// The object type carries every field declared on the domain, with
// each field's GraphQL scalar derived from `types-mapping.ts`. There
// is no equivalent of the legacy "list_fields" sub-selection here:
// list_fields lived on the View side of the old DSL and never made
// it into the new domain DSL. The View renderer picks the columns to
// show at render time, not at schema-build time.
//
// Meta (lookup) sources get their own tiny object types with just
// `value` and `label` — uniform shape regardless of which columns
// the underlying table uses, so the LLM-chunk's "copy verbatim" rule
// holds.

import { GraphQLObjectType, GraphQLString } from "graphql";
import type { GraphQLFieldConfigMap, GraphQLObjectTypeConfig } from "graphql";

import type { DomainDef, MetaDef } from "oos-dsls-ts";

import { domainTypeName, metaTypeName } from "../naming";
import { gqlScalarFor } from "../types-mapping";

/**
 * buildDomainObjectType builds the GraphQL object type for a domain.
 *
 * The type is reused as the return type of the top-level query, the
 * record-side return type of every mutation, and (in a future step)
 * the embedded type for relation traversals.
 */
export function buildDomainObjectType(domain: DomainDef): GraphQLObjectType {
	const fields: GraphQLFieldConfigMap<unknown, unknown> = {};
	for (const f of domain.fields) {
		fields[f.name] = { type: gqlScalarFor(f.type) };
	}

	const config: GraphQLObjectTypeConfig<unknown, unknown> = {
		name: domainTypeName(domain.name),
		fields,
	};
	return new GraphQLObjectType(config);
}

/**
 * buildMetaObjectType builds the GraphQL object type for a meta
 * source. Always exposes exactly two fields, `value` and `label`,
 * both as String — the Meta resolver aliases the underlying source
 * columns into this shape.
 */
export function buildMetaObjectType(meta: MetaDef): GraphQLObjectType {
	const fields: GraphQLFieldConfigMap<unknown, unknown> = {
		value: { type: GraphQLString },
		label: { type: GraphQLString },
	};
	const config: GraphQLObjectTypeConfig<unknown, unknown> = {
		name: metaTypeName(meta.name),
		fields,
	};
	return new GraphQLObjectType(config);
}
