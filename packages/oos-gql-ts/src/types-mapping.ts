// types-mapping.ts — DSL field type → GraphQL scalar mapping.
//
// Mirrors the legacy `typeToGraphQL()` in oos-common_old/gql/schema.go,
// but with two improvements:
//
//   * `bool` maps to GraphQLBoolean instead of falling back to String.
//   * `date` and `datetime` map to a dedicated GraphQLString variant
//     so callers see the intent in the schema, even though the wire
//     format remains String — Postgres returns ISO timestamps and
//     the GraphQL layer passes them through.
//
// Pure module: no imports from the DSL package's runtime, only the
// FieldTypeDef literal type so consumers can call this from contexts
// where the full DSL package is overkill.

import { GraphQLBoolean, GraphQLFloat, GraphQLInt, GraphQLString } from "graphql";
import type { GraphQLScalarType } from "graphql";

import type { FieldTypeDef } from "oos-dsls-ts";

/**
 * gqlScalarFor returns the GraphQL scalar for a DSL field type.
 *
 * Unknown types fall through to GraphQLString — matching the legacy
 * "default to string" behaviour. This keeps the schema build robust
 * to future grammar additions: a new field type renders as a String
 * column until a real mapping lands here.
 */
export function gqlScalarFor(t: FieldTypeDef): GraphQLScalarType {
	switch (t) {
		case "int":
			return GraphQLInt;
		case "float":
			return GraphQLFloat;
		case "bool":
			return GraphQLBoolean;
		case "string":
		case "text":
		case "date":
		case "datetime":
			return GraphQLString;
		default:
			// Exhaustive in normal grammar; defensive for forward compat.
			return GraphQLString;
	}
}
