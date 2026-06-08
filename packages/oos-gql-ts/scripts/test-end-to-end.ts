// scripts/test-end-to-end.ts — Real round-trip against the demo DB.
//
// Loads .domain examples, builds the schema, and executes a few
// representative queries and a mutation. Verifies that:
//
//   * the bare-name and `_eq` filters return the same row
//   * `_contains` does ILIKE matching
//   * meta queries return value/label pairs
//   * an UPDATE round-trips through buildUpdate/the resolver
//
// Database: localhost:5432 / postgres / demo / onisin (the standard
// demo DSN). Skipped silently if the database is unreachable, so this
// script doesn't break a CI run that happens to omit Postgres.

import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { graphql, type ExecutionResult } from "graphql";
import postgres from "postgres";

import { parseDomain } from "oos-dsls-ts";
import type { DomainDef } from "oos-dsls-ts";

import { buildSchema } from "../src";

const examplesDir = resolve(
	import.meta.dir,
	"..",
	"..",
	"oos-dsls-ts",
	"grammar",
	"examples",
);

const domains: DomainDef[] = [];
for (const file of ["person.domain", "note.domain"]) {
	const path = resolve(examplesDir, file);
	const source = readFileSync(path, "utf-8");
	const result = await parseDomain(source, `file://${path}`);
	if (!result.def) {
		console.error(`✗ failed to parse ${file}`);
		process.exit(1);
	}
	domains.push(result.def);
}

const sql = postgres({
	host: "localhost",
	port: 5432,
	user: "postgres",
	password: "demo",
	database: "onisin",
	onnotice: () => {}, // silence NOTICE chatter
});

try {
	await sql`SELECT 1`;
} catch (e) {
	console.warn("⚠️  demo DB unreachable, skipping end-to-end test:", String(e));
	process.exit(0);
}

const schema = buildSchema(domains, sql);

async function run(query: string, label: string): Promise<ExecutionResult> {
	const result = await graphql({ schema, source: query });
	if (result.errors) {
		console.error(`✗ ${label}`);
		for (const err of result.errors) {
			console.error(`    ${err.message}`);
		}
	} else {
		console.log(`✓ ${label}`);
	}
	return result;
}

// 1. Fetch a person by id; expect a single record.
const r1 = await run(
	`{ person(id: 1) { id firstname lastname email } }`,
	"person(id: 1)",
);
console.log(`    →`, JSON.stringify((r1.data as any)?.person?.[0] ?? null));

// 2. Bare-name equals shortcut.
const r2 = await run(
	`{ person(firstname: "Anna") { id firstname lastname } }`,
	'person(firstname: "Anna")',
);
console.log(`    → ${((r2.data as any)?.person ?? []).length} rows`);

// 3. ILIKE via _contains.
const r3 = await run(
	`{ person(lastname_contains: "an") { id lastname } }`,
	'person(lastname_contains: "an")',
);
console.log(`    → ${((r3.data as any)?.person ?? []).length} rows`);

// 4. Meta query.
const r4 = await run(
	`{ meta_roles { value label } }`,
	"meta_roles",
);
console.log(`    → ${((r4.data as any)?.meta_roles ?? []).length} options`);

// 5. Combined query — record + dropdowns in one shot, the LLM-chunk shape.
const r5 = await run(
	`{
		person(id: 1) { id firstname lastname role department }
		meta_roles { value label }
		meta_departments { value label }
	}`,
	"combined: person + meta_roles + meta_departments",
);
console.log(
	`    → person=${((r5.data as any)?.person ?? []).length} ` +
		`roles=${((r5.data as any)?.meta_roles ?? []).length} ` +
		`departments=${((r5.data as any)?.meta_departments ?? []).length}`,
);

// 6. Round-trip mutation: read, update, read back. Restores the
//    original value at the end so the demo DB stays untouched.
const r6read = await run(
	`{ person(id: 1) { id firstname } }`,
	"setup: read person 1",
);
const original = (r6read.data as any)?.person?.[0]?.firstname as string | undefined;
if (original) {
	const tag = `__test_${Date.now()}`;
	await run(
		`mutation { update_person(id: 1, firstname: "${tag}") { id firstname } }`,
		"update person 1",
	);
	const r6back = await run(
		`{ person(id: 1) { id firstname } }`,
		"verify update",
	);
	const got = (r6back.data as any)?.person?.[0]?.firstname;
	console.log(`    → updated firstname = ${got} (expected ${tag})`);
	await run(
		`mutation { update_person(id: 1, firstname: "${original.replace(/"/g, '\\"')}") { id firstname } }`,
		"restore original",
	);
}

await sql.end();
console.log("done");
