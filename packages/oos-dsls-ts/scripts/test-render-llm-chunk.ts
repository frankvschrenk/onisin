// scripts/test-render-llm-chunk.ts — End-to-end renderer test.
//
// Parses every .domain example, runs the LLM-chunk renderer, and
// prints the output to stdout for visual inspection. Compare with
// the legacy Go renderer's output to spot regressions before they
// reach the embedding pipeline.

import { readFileSync, readdirSync } from "node:fs";
import { resolve } from "node:path";
import { parseDomain } from "../src/parse";
import { renderLLMChunk } from "../src/renderer/llm-chunk";

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

	const chunk = renderLLMChunk(result.def);
	console.log(`──── ${file} ────`);
	console.log(chunk);
	console.log();
}

if (failed > 0) {
	process.exit(1);
}
