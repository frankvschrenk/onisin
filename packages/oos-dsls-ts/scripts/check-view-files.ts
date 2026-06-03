// scripts/check-view-files.ts — Smoke-test the view DSL parser against
// every .view file in the workspace. Prints one line per file and
// exits non-zero on the first parse error or validator diagnostic.
//
// Run from the repo root:
//
//     bun run packages/oos-dsls-ts/scripts/check-view-files.ts

import { readFileSync } from "node:fs";
import { execSync } from "node:child_process";

import { parseView } from "oos-dsls-ts";

const repoRoot = execSync("git rev-parse --show-toplevel", { encoding: "utf8" }).trim();
const list     = execSync(
	`find ${repoRoot} -name '*.view' -not -path '*/node_modules/*'`,
	{ encoding: "utf8" },
).trim().split("\n").filter(Boolean);

let failures = 0;
for (const path of list) {
	const source = readFileSync(path, "utf8");
	const result = await parseView(source, `file://${path}`);
	const errors = result.diagnostics.filter((d) => d.severity === 1);
	const status = errors.length === 0 && result.def ? "OK " : "ERR";
	const domains = result.def
		? result.def.domains
			.map((d) => (d.alias === d.name ? d.name : `${d.name}(${d.alias})`))
			.join(", ")
		: "?";
	console.log(`[${status}] ${path}  over ${domains}`);
	if (errors.length > 0) {
		failures++;
		for (const err of errors) {
			console.log(`        ${err.message}`);
		}
	}
}

if (failures > 0) {
	console.log(`\n${failures} file(s) failed to parse.`);
	process.exit(1);
}
console.log(`\nAll ${list.length} files parsed cleanly.`);
