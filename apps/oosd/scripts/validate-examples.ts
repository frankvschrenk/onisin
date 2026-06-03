// scripts/validate-examples.ts
//
// Smoke test: parse every example .domain and .view file in
// grammar/examples/ through the freshly generated services and
// report any errors. Exits non-zero on any parse failure.
//
// Run with:  bun run scripts/validate-examples.ts

import { readdirSync, readFileSync } from "node:fs";
import { join } from "node:path";

import {
	createDefaultCoreModule,
	createDefaultSharedCoreModule,
	EmptyFileSystem,
	inject,
} from "langium";

import {
	DomainGeneratedModule,
	OnisinGeneratedSharedModule,
	ViewGeneratedModule,
} from "../src/lang/generated/module";

// Build minimal core services for both languages. We only need the
// parser, so we avoid pulling in the full LSP service set.
const shared = inject(
	createDefaultSharedCoreModule(EmptyFileSystem),
	OnisinGeneratedSharedModule,
);
const domainServices = inject(
	createDefaultCoreModule({ shared }),
	DomainGeneratedModule,
);
const viewServices = inject(
	createDefaultCoreModule({ shared }),
	ViewGeneratedModule,
);

const dir = "grammar/examples";
const files = readdirSync(dir);

let failures = 0;
for (const file of files.sort()) {
	const path = join(dir, file);
	const text = readFileSync(path, "utf8");

	const services = file.endsWith(".domain")
		? domainServices
		: file.endsWith(".view")
			? viewServices
			: null;
	if (!services) continue;

	const parser = services.parser.LangiumParser;
	const result = parser.parse(text);
	const errors = result.lexerErrors.length + result.parserErrors.length;

	if (errors === 0) {
		console.log(`OK    ${path}`);
	} else {
		failures++;
		console.log(`FAIL  ${path}`);
		for (const e of result.lexerErrors) {
			console.log(`  lex   ${e.line}:${e.column}  ${e.message}`);
		}
		for (const e of result.parserErrors) {
			const tok = e.token;
			console.log(
				`  parse ${tok.startLine ?? "?"}:${tok.startColumn ?? "?"}  ${e.message}`,
			);
		}
	}
}

process.exit(failures === 0 ? 0 : 1);
