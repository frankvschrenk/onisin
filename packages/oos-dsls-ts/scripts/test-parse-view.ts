// scripts/test-parse-view.ts — Smoke test: parse the person_list.view
// example and print the resulting ViewDef as JSON. No test framework
// dependency; run via `bun scripts/test-parse-view.ts`.

import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { parseView } from "../src/parse";

const examplePath = resolve(
	import.meta.dir,
	"..",
	"grammar",
	"examples",
	"person_list.view",
);

const source = readFileSync(examplePath, "utf-8");
const result = await parseView(source, `file://${examplePath}`);

if (result.diagnostics.length > 0) {
	console.error("diagnostics:");
	for (const d of result.diagnostics) {
		const line = d.range.start.line + 1;
		const col = d.range.start.character + 1;
		console.error(`  ${line}:${col} [${d.severity}] ${d.message}`);
	}
}

if (!result.def) {
	console.error("no ViewDef produced (parse failed)");
	process.exit(1);
}

console.log(JSON.stringify(result.def, null, 2));
