// scripts/test-seed.ts — Smoke test the runner against a live DB.
//
// Usage:
//   cd apps/oosd && bun run scripts/test-seed.ts
//
// Prints the row counts of every table the seed populates so a
// human can eyeball the result. Connects to the demo database
// using the password from demo.toml at the repo root.

import { runDemo, runInternal } from "../src/bun/seed/runner";

const DSN =
	"postgres://postgres:demo@localhost:5432/onisin?sslmode=disable";

console.log("→ runInternal");
await runInternal(DSN);
console.log("✓ runInternal done");

console.log("→ runDemo");
await runDemo(DSN);
console.log("✓ runDemo done");

// ─── Verify ──────────────────────────────────────────────────────────

import postgres from "postgres";

const sql = postgres(DSN, { max: 1, onnotice: () => {} });
try {
	const tables = [
		"public.country",
		"public.city",
		"public.person",
		"public.note",
		"public.police_incidents",
		"public.support_tickets",
		"oos.domain",
		"oos.view",
		"oos.event_mappings",
	];
	for (const t of tables) {
		const rows = await sql.unsafe(`SELECT count(*)::int AS n FROM ${t}`);
		console.log(`  ${t.padEnd(28)} ${rows[0]!.n}`);
	}
} finally {
	await sql.end({ timeout: 5 });
}
