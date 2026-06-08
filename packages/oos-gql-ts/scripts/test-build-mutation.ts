// scripts/test-build-mutation.ts — Standalone test for the
// client-side GraphQL mutation builder.
//
// Loads person.domain via the parser, then exercises every relevant
// branch of buildMutationFromMap:
//
//   * INSERT path (no id)            — verb selection + RETURNING id
//   * UPDATE path (id present)        — verb selection + WHERE id
//   * readonly fields stripped        — id/uuid/created_at not in args
//   * type formatting                 — int / float / bool / string
//   * string escaping                 — quote and backslash in input
//   * unknown field rejection         — hallucination surfaces as error
//   * empty args after readonly-strip — caller sees an explicit error
//
// Pure unit test — no Postgres, no oosgql server, no network. Run
// directly with `bun run scripts/test-build-mutation.ts` from the
// package root.

import { readFileSync } from "node:fs";
import { resolve } from "node:path";

import { parseDomain } from "oos-dsls-ts";
import type { DomainDef } from "oos-dsls-ts";

import { buildMutationFromMap } from "../src";

const personPath = resolve(
	import.meta.dir,
	"..",
	"..",
	"oos-dsls-ts",
	"grammar",
	"examples",
	"person.domain",
);

const source = readFileSync(personPath, "utf-8");
const parseResult = await parseDomain(source, `file://${personPath}`);
if (!parseResult.def) {
	console.error("✗ failed to parse person.domain");
	for (const d of parseResult.diagnostics) {
		console.error(`  ${d.severity}: ${d.message}`);
	}
	process.exit(1);
}
const person: DomainDef = parseResult.def;

let failed = 0;
const ok = (label: string) => console.log(`✓ ${label}`);
const fail = (label: string, detail: string) => {
	console.error(`✗ ${label}\n    ${detail}`);
	failed++;
};

// ─── 1. INSERT — no id, mixed types ──────────────────────────────────

{
	const result = buildMutationFromMap(person, {
		firstname: "Anna",
		lastname: "Meier",
		age: 42,
		net_worth: 1234.5,
		active: true,
	});

	const expected =
		`mutation {\n` +
		`  insert_person(firstname: "Anna", lastname: "Meier", age: 42, ` +
		`net_worth: 1234.5, active: true) {\n` +
		`    id\n` +
		`    firstname\n` +
		`    lastname\n` +
		`    age\n` +
		`    net_worth\n` +
		`    active\n` +
		`  }\n` +
		`}`;

	if (result.verb !== "insert") {
		fail("INSERT verb", `expected "insert", got "${result.verb}"`);
	} else if (result.query !== expected) {
		fail(
			"INSERT rendering",
			`got:\n${result.query}\n  ---\nwant:\n${expected}`,
		);
	} else {
		ok("INSERT — no id, mixed types");
	}
}

// ─── 2. UPDATE — id present, readonly fields ignored ─────────────────

{
	const result = buildMutationFromMap(person, {
		id: 17,
		firstname: "Bea",
		// readonly fields the LLM should not be sending but might:
		uuid: "00000000-0000-0000-0000-000000000000",
		created_at: "2024-01-01T00:00:00Z",
	});

	if (result.verb !== "update") {
		fail("UPDATE verb", `expected "update", got "${result.verb}"`);
	} else if (
		result.query.includes("uuid:") ||
		result.query.includes("created_at:")
	) {
		fail(
			"UPDATE strips readonly args",
			`readonly fields leaked into args:\n${result.query}`,
		);
	} else if (
		!result.query.includes("update_person(id: 17, firstname: \"Bea\")")
	) {
		fail(
			"UPDATE arg shape",
			`expected "id: 17, firstname: \\"Bea\\"" in args:\n${result.query}`,
		);
	} else if (
		!result.returnFields.includes("uuid") ||
		!result.returnFields.includes("created_at")
	) {
		fail(
			"UPDATE keeps readonly in RETURNING",
			`returnFields: ${result.returnFields.join(", ")}`,
		);
	} else {
		ok("UPDATE — readonly stripped from args, kept in RETURNING");
	}
}

// ─── 3. id as string "0" → INSERT ────────────────────────────────────

{
	const result = buildMutationFromMap(person, { id: "0", firstname: "Cleo" });
	if (result.verb !== "insert") {
		fail(
			'id "0" → INSERT',
			`expected "insert", got "${result.verb}"`,
		);
	} else {
		ok('id "0" treated as empty → INSERT');
	}
}

// ─── 4. id as empty string → INSERT ──────────────────────────────────

{
	const result = buildMutationFromMap(person, { id: "", firstname: "Dora" });
	if (result.verb !== "insert") {
		fail(
			'id "" → INSERT',
			`expected "insert", got "${result.verb}"`,
		);
	} else {
		ok('id "" treated as empty → INSERT');
	}
}

// ─── 5. id as numeric string "42" → UPDATE ───────────────────────────

{
	const result = buildMutationFromMap(person, {
		id: "42",
		firstname: "Eva",
	});
	if (result.verb !== "update") {
		fail(
			'id "42" → UPDATE',
			`expected "update", got "${result.verb}"`,
		);
	} else if (!result.query.includes("id: 42")) {
		fail(
			'id "42" coerced to integer literal',
			`got query: ${result.query}`,
		);
	} else {
		ok('id "42" coerced to integer → UPDATE');
	}
}

// ─── 6. String escaping — quotes, backslash, newline ─────────────────

{
	const result = buildMutationFromMap(person, {
		firstname: 'He said "hi"',
		notes: "line1\nline2\\back",
	});
	const wantedFirstname = `firstname: "He said \\"hi\\""`;
	const wantedNotes = `notes: "line1\\nline2\\\\back"`;
	if (!result.query.includes(wantedFirstname)) {
		fail("escapes double-quote", `got: ${result.query}`);
	} else if (!result.query.includes(wantedNotes)) {
		fail(
			"escapes newline + backslash",
			`got: ${result.query}`,
		);
	} else {
		ok("string escaping — quote, backslash, newline");
	}
}

// ─── 7. Bool coercion from common LLM-emitted forms ──────────────────

{
	const fromString = buildMutationFromMap(person, {
		firstname: "F",
		active: "true",
	});
	const fromNumber = buildMutationFromMap(person, {
		firstname: "F",
		active: 1,
	});
	if (
		!fromString.query.includes("active: true") ||
		!fromNumber.query.includes("active: true")
	) {
		fail(
			'bool coercion ("true" / 1)',
			`string: ${fromString.query}\nnumber: ${fromNumber.query}`,
		);
	} else {
		ok('bool coercion — "true" and 1 both render true');
	}
}

// ─── 8. Unknown field → error (hallucination surfaces) ───────────────

{
	let threw = false;
	try {
		buildMutationFromMap(person, {
			firstname: "G",
			definitely_not_a_field: "value",
		});
	} catch (err) {
		threw = true;
		const msg = String(err);
		if (!msg.includes("unknown field") || !msg.includes("definitely_not_a_field")) {
			fail("unknown-field error message", msg);
			threw = false;
		}
	}
	if (!threw) {
		fail(
			"unknown field rejection",
			"expected throw, none happened",
		);
	} else {
		ok("unknown field → throws with descriptive message");
	}
}

// ─── 9. Empty data → error ───────────────────────────────────────────

{
	let threw = false;
	try {
		buildMutationFromMap(person, {});
	} catch {
		threw = true;
	}
	if (!threw) fail("empty data rejection", "expected throw");
	else ok("empty data → throws");
}

// ─── 10. Only readonly fields → error ────────────────────────────────

{
	let threw = false;
	try {
		buildMutationFromMap(person, { uuid: "abc", created_at: "x" });
	} catch (err) {
		threw = true;
		const msg = String(err);
		if (!msg.includes("no settable fields")) {
			fail("only-readonly error message", msg);
			threw = false;
		}
	}
	if (!threw) fail("only-readonly rejection", "expected throw");
	else ok("only readonly fields → throws with descriptive message");
}

// ─── Summary ─────────────────────────────────────────────────────────

if (failed > 0) {
	console.error(`\n${failed} test(s) failed.`);
	process.exit(1);
}
console.log("\nall buildMutationFromMap tests passed.");
