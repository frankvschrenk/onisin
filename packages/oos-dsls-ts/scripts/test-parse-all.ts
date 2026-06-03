// scripts/test-parse-all.ts — Parse every example and report whether
// each produced a ViewDef. Use as a regression check.

import { readFileSync, readdirSync } from "node:fs";
import { resolve } from "node:path";
import { parseView } from "../src/parse";

const examplesDir = resolve(import.meta.dir, "..", "grammar", "examples");
const viewFiles = readdirSync(examplesDir).filter((f) => f.endsWith(".view"));

let failed = 0;
for (const file of viewFiles) {
	const path = resolve(examplesDir, file);
	const source = readFileSync(path, "utf-8");
	const result = await parseView(source, `file://${path}`);

	const errors = result.diagnostics.filter((d) => d.severity === 1);
	if (!result.def || errors.length > 0) {
		failed++;
		console.error(`✗ ${file}`);
		for (const d of errors) {
			console.error(`    ${d.range.start.line + 1}:${d.range.start.character + 1}  ${d.message}`);
		}
		continue;
	}

	const bodyCount = result.def.body.length;
	const toolbarCount = result.def.toolbar.length;
	console.log(
		`✓ ${file}  view=${result.def.name}  toolbar=${toolbarCount}  body=${bodyCount}`,
	);
}

if (failed > 0) {
	process.exit(1);
}
