// resolvers/sql-resolvers.ts — Resolver factories backed by DbClient.
//
// Each factory returns a GraphQL resolver bound to a specific domain
// plus the shared DbClient. The resolver is a thin trampoline: take
// args, build SQL, run it, return the row(s).
//
// Why factories instead of one big resolver: closures over `domain`
// keep the per-call code tiny and the WHERE/SET/INSERT helpers stay
// pure (no `domain` lookup at execution time).
//
// Dialect handling:
//   - toDialectParams rewrites $1/$2 placeholders to ? where needed.
//   - dialectSupportsReturning controls whether mutations read back
//     the mutated row in one round-trip (RETURNING) or via a
//     follow-up SELECT by id.

import type { GraphQLFieldResolver } from "graphql";

import type { DomainDef } from "oos-dsls-ts";

import { buildDelete, buildInsert, buildUpdate } from "../sql/mutation";
import { buildWhereClause, selectColumnList }    from "../sql/select";
import {
	dialectSupportsReturning,
	toDialectParams,
	type DbClient,
	type DbRow,
} from "../db-client";

/** A GraphQL resolver returning `unknown` so all four CRUD ops fit. */
export type AnyResolver = GraphQLFieldResolver<unknown, unknown>;

/**
 * makeQueryResolver returns the resolver for the top-level domain
 * query. Always returns an array — the GraphQL field is typed as
 * `[Domain]` and consumers pick `.domain[0]` for single-record cases.
 */
export function makeQueryResolver(domain: DomainDef, db: DbClient): AnyResolver {
	const cols = selectColumnList(domain);
	return async (_source, rawArgs) => {
		const args  = (rawArgs ?? {}) as Record<string, unknown>;
		const where = buildWhereClause(domain, args);
		const text  = toDialectParams(
			`SELECT ${cols} FROM ${domain.source}${where.sql}`,
			db.dialect,
		);
		return await db.query(text, where.values);
	};
}

/**
 * makeUpdateResolver returns the resolver for `update_<domain>`.
 * Throws on a missing id or empty SET clause — both surface to the
 * caller as a GraphQL error string.
 */
export function makeUpdateResolver(domain: DomainDef, db: DbClient): AnyResolver {
	const returning = dialectSupportsReturning(db.dialect);
	const cols      = selectColumnList(domain);
	return async (_source, rawArgs) => {
		const args = (rawArgs ?? {}) as Record<string, unknown>;
		const cmd  = buildUpdate(domain, args);
		if (!cmd) throw new Error("update requires id and at least one settable field");
		if (returning) {
			const sql  = toDialectParams(cmd.sql, db.dialect);
			const rows = await db.query(sql, cmd.values);
			return rows[0];
		}
		// Fallback: execute without RETURNING, then re-fetch by id.
		const sql = toDialectParams(
			cmd.sql.replace(/ RETURNING .+$/, ""),
			db.dialect,
		);
		await db.query(sql, cmd.values);
		const rows = await db.query(
			toDialectParams(`SELECT ${cols} FROM ${domain.source} WHERE id = $1`, db.dialect),
			[cmd.idValue],
		);
		return rows[0];
	};
}

/** makeInsertResolver returns the resolver for `insert_<domain>`. */
export function makeInsertResolver(domain: DomainDef, db: DbClient): AnyResolver {
	const returning = dialectSupportsReturning(db.dialect);
	const cols      = selectColumnList(domain);
	return async (_source, rawArgs) => {
		const args = (rawArgs ?? {}) as Record<string, unknown>;
		const cmd  = buildInsert(domain, args);
		if (!cmd) throw new Error("insert requires at least one settable field");
		if (returning) {
			const sql  = toDialectParams(cmd.sql, db.dialect);
			const rows = await db.query(sql, cmd.values);
			return rows[0];
		}
		// Fallback: execute without RETURNING, fetch inserted row by
		// last-insert id. The db.query implementation must set
		// rows[0].insertId for this path (knex does this for mysql/mariadb).
		const sql = toDialectParams(
			cmd.sql.replace(/ RETURNING .+$/, ""),
			db.dialect,
		);
		const result = await db.query(sql, cmd.values);
		const insertId = (result[0] as DbRow | undefined)?.insertId;
		if (insertId === undefined) throw new Error("insert did not return an id");
		const rows = await db.query(
			toDialectParams(`SELECT ${cols} FROM ${domain.source} WHERE id = $1`, db.dialect),
			[insertId],
		);
		return rows[0];
	};
}

/** makeDeleteResolver returns the resolver for `delete_<domain>`. */
export function makeDeleteResolver(domain: DomainDef, db: DbClient): AnyResolver {
	const returning = dialectSupportsReturning(db.dialect);
	return async (_source, rawArgs) => {
		const args = (rawArgs ?? {}) as Record<string, unknown>;
		const cmd  = buildDelete(domain, args);
		if (!cmd) throw new Error("delete requires id");
		if (returning) {
			const sql  = toDialectParams(cmd.sql, db.dialect);
			const rows = await db.query(sql, cmd.values);
			return rows[0];
		}
		const sql = toDialectParams(
			cmd.sql.replace(/ RETURNING .+$/, ""),
			db.dialect,
		);
		await db.query(sql, cmd.values);
		return { id: cmd.idValue };
	};
}
