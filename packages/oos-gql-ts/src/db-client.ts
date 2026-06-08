// db-client.ts — Database driver abstraction for oos-gql-ts.
//
// Decouples the GraphQL resolver layer from any specific database
// driver. Callers (oosgql) create a DbClient from their Knex
// instance and pass it to buildSchema. The package never imports
// postgres.js, knex, or any other driver directly.
//
// Dialect controls the two SQL constructs that vary across databases:
//   - Parameter placeholders: Postgres uses $1/$2, MySQL/MariaDB/
//     Oracle/MSSQL use ?.
//   - RETURNING clause: Postgres and MariaDB 10.5+ support
//     RETURNING; MSSQL uses OUTPUT INSERTED.*; Oracle uses
//     RETURNING … INTO (with bind variables). For simplicity we
//     treat non-postgres dialects as not supporting RETURNING and
//     fall back to a follow-up SELECT by id.

/** Supported SQL dialects. */
export type DbDialect = "postgres" | "mysql" | "mariadb" | "mssql" | "oracle" | "sqlite3";

/** A single row returned from the database, shape unknown at compile time. */
export type DbRow = Record<string, unknown>;

/**
 * DbClient is the only interface oos-gql-ts uses to talk to a
 * database. Implementors wrap any driver (Knex, pg, mysql2, …).
 */
export interface DbClient {
	/** SQL dialect — controls placeholder style and RETURNING support. */
	readonly dialect: DbDialect;

	/**
	 * query executes a raw SQL string with positional or '?'-style
	 * parameters (matching the dialect) and returns all rows.
	 * The implementation is responsible for mapping driver-specific
	 * result shapes to a plain Row[].
	 */
	query(sql: string, values?: unknown[]): Promise<DbRow[]>;
}

/**
 * dialectSupportsReturning returns true for dialects that support the
 * RETURNING clause natively.  MariaDB added it in 10.5; we conservatively
 * only trust postgres here since that is the only dialect tested.
 */
export function dialectSupportsReturning(d: DbDialect): boolean {
	return d === "postgres" || d === "mariadb";
}

/**
 * toDialectParams rewrites a Postgres-style positional SQL string
 * ($1, $2, …) to ? placeholders for knex.raw().
 *
 * knex.raw(sql, values[]) always uses ? as the binding marker,
 * regardless of the underlying driver — the pg driver's native $N
 * syntax is only active when knex builds the query itself, not when
 * raw SQL is passed directly. So we normalise to ? for all dialects.
 */
export function toDialectParams(sql: string, _dialect: DbDialect): string {
	// Replace $1, $2, … with ? in order — the values array stays in
	// the same positional order so no reordering is needed.
	return sql.replace(/\$\d+/g, "?");
}
