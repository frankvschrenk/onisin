// scripts/seed-dsl.ts
//
// One-shot upsert of the DSL examples in grammar/examples/ into
// oos.domain and oos.view. Idempotent: re-running it just overwrites
// the source columns with whatever is on disk.
//
// Each example is parsed through the generated Langium services
// before it is written. If any file fails to parse, nothing is
// written — we don't want to seed broken DSL into the demo.
//
// Usage:  bun run scripts/seed-dsl.ts
//   or:   bun run db:seed-dsl
//
// DB credentials come from DATABASE_URL or the default DSN matching
// the demo database convention.

import { readFileSync } from "node:fs";
import { join, resolve } from "node:path";

import knex from "knex";

import { createOnisinServices } from "oos-dsls-ts";

// ─── Examples to seed ────────────────────────────────────────────────

type Kind = "domain" | "view";

const EXAMPLES: { file: string; kind: Kind; id: string }[] = [
	{ file: "note.domain",        kind: "domain", id: "note"          },
	{ file: "person.domain",      kind: "domain", id: "person"        },
	{ file: "note_detail.view",   kind: "view",   id: "note_detail"   },
	{ file: "note_list.view",     kind: "view",   id: "note_list"     },
	{ file: "person_detail.view", kind: "view",   id: "person_detail" },
	{ file: "person_list.view",   kind: "view",   id: "person_list"   },
];

const EXAMPLES_DIR = resolve(
	import.meta.dir,
	"..",
	"..",
	"packages",
	"oos-dsls-ts",
	"grammar",
	"examples",
);

// ─── Build Langium services via the shared package ───────────────────

const services = createOnisinServices();
const domainServices = services.domain;
const viewServices = services.view;

// ─── Step 1: load and parse every file ───────────────────────────────

type Entry = { kind: Kind; id: string; source: string };
const loaded: Entry[] = [];
let failures = 0;

for (const e of EXAMPLES) {
	const path = join(EXAMPLES_DIR, e.file);
	const source = readFileSync(path, "utf8");

	const svc = e.kind === "domain" ? domainServices : viewServices;
	const result = svc.parser.LangiumParser.parse(source);
	const errors = result.lexerErrors.length + result.parserErrors.length;

	if (errors !== 0) {
		failures++;
		console.error(`FAIL ${path}`);
		for (const x of result.lexerErrors) {
			console.error(`  lex   ${x.line}:${x.column}  ${x.message}`);
		}
		for (const x of result.parserErrors) {
			const tok = x.token;
			console.error(
				`  parse ${tok.startLine ?? "?"}:${tok.startColumn ?? "?"}  ${x.message}`,
			);
		}
		continue;
	}

	loaded.push({ kind: e.kind, id: e.id, source });
	console.log(`OK   ${path}`);
}

if (failures > 0) {
	console.error(`\n${failures} file(s) failed to parse — nothing written.`);
	process.exit(1);
}

// ─── Step 2: upsert into oos.domain / oos.view ───────────────────────

const dsn =
	process.env.DATABASE_URL ??
	"postgres://postgres:demo@localhost:5432/onisin?sslmode=disable";

const db = knex({ client: "pg", connection: dsn });

try {
	for (const e of loaded) {
		const table = e.kind === "domain" ? "oos.domain" : "oos.view";
		await db.raw(
			`INSERT INTO ${table} (id, source)
			 VALUES (?, ?)
			 ON CONFLICT (id) DO UPDATE
			   SET source = EXCLUDED.source, updated_at = now()`,
			[e.id, e.source],
		);
		console.log(`wrote ${table}[${e.id}]  (${e.source.length} bytes)`);
	}
} finally {
	await db.destroy();
}

console.log(`\nseeded ${loaded.length} DSL row(s).`);
