// scripts/test-mock-envelope.ts — Smoke test for the auto-mock generator.
//
// Loads the real person.domain and person_detail.view sources from
// the demo DB, parses both, runs mockEnvelope, and pretty-prints
// the result. Exits 0 on a structurally sensible envelope.
//
// Not a unit test — there's nothing to assert against without
// hand-rolling expected output. Goal is "does it look right when
// I read it" before committing.

import postgres from "postgres";

import { parseDomain, parseView } from "oos-dsls-ts";
import { mockEnvelope } from "oos-ui-ts";

const sql = postgres("postgres://postgres:demo@localhost:5432/onisin", {
	onnotice: () => {},
});

try {
	const [domainRow] = (await sql`SELECT source FROM oos.domain WHERE id = 'person'`) as Array<{ source: string }>;
	const [viewRow] = (await sql`SELECT source FROM oos.view WHERE id = 'person_detail'`) as Array<{ source: string }>;

	if (!domainRow || !viewRow) {
		console.error("missing demo data: person.domain or person_detail.view");
		process.exit(1);
	}

	const domainResult = await parseDomain(
		domainRow.source,
		"inmemory://test/person.domain",
	);
	const viewResult = await parseView(
		viewRow.source,
		"inmemory://test/person_detail.view",
	);

	if (!domainResult.def || !viewResult.def) {
		console.error("parse failed");
		console.error(
			"  domain:",
			domainResult.diagnostics.filter((d) => d.severity === 1),
		);
		console.error(
			"  view:",
			viewResult.diagnostics.filter((d) => d.severity === 1),
		);
		process.exit(1);
	}

	const env = mockEnvelope(viewResult.def, domainResult.def);
	console.log("=== person_detail envelope ===");
	console.log(JSON.stringify(env, null, 2));

	// Also try a list view to verify the rows path.
	const [viewListRow] = (await sql`SELECT source FROM oos.view WHERE id = 'person_list'`) as Array<{ source: string }>;
	if (viewListRow) {
		const listResult = await parseView(
			viewListRow.source,
			"inmemory://test/person_list.view",
		);
		if (listResult.def) {
			const listEnv = mockEnvelope(listResult.def, domainResult.def);
			console.log("\n=== person_list envelope (rows count + first row) ===");
			const content = listEnv.content as Record<string, { rows?: unknown[] }>;
			const rows = content.person?.rows;
			console.log(`rows: ${Array.isArray(rows) ? rows.length : "missing"}`);
			if (Array.isArray(rows) && rows[0]) {
				console.log("first row:", JSON.stringify(rows[0], null, 2));
			}
		}
	}
} finally {
	await sql.end();
}
