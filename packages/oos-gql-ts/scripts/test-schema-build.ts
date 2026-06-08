// scripts/test-schema-build.ts — Build the schema without touching
// the database, then introspect it to verify the shape.
//
// Postgres.js is mocked: the tagged-template / unsafe API is never
// called during this test, only the schema construction path. That
// keeps the test runnable in CI without a postgres dependency.

import { readFileSync, readdirSync } from "node:fs";
import { resolve } from "node:path";
import {
	GraphQLObjectType,
	type GraphQLField,
	type GraphQLNamedType,
} from "graphql";

import { parseDomain } from "oos-dsls-ts";
import type { DomainDef } from "oos-dsls-ts";

import { buildSchema } from "../src";

// Minimal Sql stub — only the type, never invoked because we don't
// run a query.
const fakeSql = (() => {
	const stub = () => {
		throw new Error("fakeSql should never be called in schema-build tests");
	};
	return stub as unknown as Parameters<typeof buildSchema>[1];
})();

const examplesDir = resolve(
	import.meta.dir,
	"..",
	"..",
	"oos-dsls-ts",
	"grammar",
	"examples",
);

const domains: DomainDef[] = [];
for (const file of readdirSync(examplesDir).filter((f) => f.endsWith(".domain"))) {
	const path = resolve(examplesDir, file);
	const source = readFileSync(path, "utf-8");
	const result = await parseDomain(source, `file://${path}`);
	if (!result.def) {
		console.error(`✗ failed to parse ${file}`);
		process.exit(1);
	}
	domains.push(result.def);
}

const schema = buildSchema(domains, fakeSql);

console.log(`✓ built schema with ${domains.length} domains`);

const queryType = schema.getQueryType();
const mutationType = schema.getMutationType();
if (!queryType || !mutationType) {
	console.error("✗ schema missing Query or Mutation");
	process.exit(1);
}

console.log(`  Query fields: ${Object.keys(queryType.getFields()).join(", ")}`);
console.log(
	`  Mutation fields: ${Object.keys(mutationType.getFields()).join(", ")}`,
);

// Spot-check a field's args to confirm the operator suffixes match
// the LLM-renderer's contract.
const personFields = queryType.getFields();
const personField = personFields.person as GraphQLField<unknown, unknown> | undefined;
if (!personField) {
	console.error("✗ person query field missing");
	process.exit(1);
}
const argNames = personField.args.map((a) => a.name).sort();
console.log(`  person args: ${argNames.join(", ")}`);

// Verify the meta object types exist with value+label.
const allTypes = Object.values(schema.getTypeMap()).filter(
	(t): t is GraphQLNamedType => !t.name.startsWith("__"),
);
const metaOptionTypes = allTypes.filter((t) => t.name.startsWith("MetaOption_"));
console.log(`  meta types: ${metaOptionTypes.length}`);
for (const t of metaOptionTypes) {
	if (!(t instanceof GraphQLObjectType)) continue;
	const fields = Object.keys(t.getFields()).sort().join(",");
	if (fields !== "label,value") {
		console.error(`✗ ${t.name} has unexpected fields: ${fields}`);
		process.exit(1);
	}
}
console.log("  ✓ all meta types expose value + label");
