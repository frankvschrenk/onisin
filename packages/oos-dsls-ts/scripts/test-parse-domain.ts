// scripts/test-parse-domain.ts — Parse every .domain example and report.
//
// Smoke test mirroring `test-parse-all.ts`: useful as a regression
// check after grammar or mapper changes.

import { readFileSync, readdirSync } from "node:fs";
import { resolve } from "node:path";
import { parseDomain } from "../src/parse";

const examplesDir = resolve(import.meta.dir, "..", "grammar", "examples");
const domainFiles = readdirSync(examplesDir).filter((f) => f.endsWith(".domain"));

let failed = 0;
for (const file of domainFiles) {
	const path = resolve(examplesDir, file);
	const source = readFileSync(path, "utf-8");
	const result = await parseDomain(source, `file://${path}`);

	const errors = result.diagnostics.filter((d) => d.severity === 1);
	if (!result.def || errors.length > 0) {
		failed++;
		console.error(`✗ ${file}`);
		for (const d of errors) {
			console.error(
				`    ${d.range.start.line + 1}:${d.range.start.character + 1}  ${d.message}`,
			);
		}
		continue;
	}

	const d = result.def;
	console.log(
		`✓ ${file}  domain=${d.name}  fields=${d.fields.length}  ` +
			`metas=${d.metas.length}  perms=${d.permissions.length}  ` +
			`relations=${d.relations.length}  ai=${d.aiHints.length}`,
	);
}

if (failed > 0) {
	process.exit(1);
}
