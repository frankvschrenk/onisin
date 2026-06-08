// resolvers/meta-resolver.ts — Resolver factory for `meta_<name>` queries.
//
// Meta queries fetch dropdown options. Schema convention: every meta
// query returns `[ { value, label } ]`. The underlying source columns
// (`m.valueField`, `m.labelField`) are aliased into that uniform
// shape on the SQL side so the GraphQL contract stays stable across
// metas with wildly different schemas.
//
// Order: when the meta declares `order_by`, that column is used.
// Otherwise the result is unordered — the caller can re-sort
// client-side if needed.

import type { GraphQLFieldResolver } from "graphql";

import type { MetaDef } from "oos-dsls-ts";

import type { AnyResolver } from "./sql-resolvers";
import type { DbClient }    from "../db-client";

/**
 * makeMetaResolver returns the resolver for one Meta. Selects the
 * value and label columns aliased as `value` and `label`, ordered by
 * the meta's `order_by` if present.
 */
export function makeMetaResolver(meta: MetaDef, db: DbClient): AnyResolver {
	const orderBy = meta.orderBy ? ` ORDER BY ${meta.orderBy}` : "";
	const text =
		`SELECT ${meta.valueField} AS value, ${meta.labelField} AS label ` +
		`FROM ${meta.table}${orderBy}`;
	const resolver: GraphQLFieldResolver<unknown, unknown> = async () => {
		return await db.query(text);
	};
	return resolver;
}
