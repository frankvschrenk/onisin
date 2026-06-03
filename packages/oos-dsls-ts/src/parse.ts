// parse.ts — Convenience parsers that combine service-level parsing
// with the AST → runtime mapping in one call.
//
// Internally a single OnisinServices instance is created lazily and
// reused; callers that need full control over the services should
// import `createOnisinServices()` directly and call its `parse()`
// method, then run the mapper themselves.

import type { Diagnostic } from "vscode-languageserver-types";

import { createOnisinServices, type OnisinServices } from "./services";
import { mapDomain } from "./mapper/domain";
import { mapView } from "./mapper/view";
import type { DomainDef, ViewDef } from "./types";

let cachedServices: OnisinServices | undefined;

function services(): OnisinServices {
	if (!cachedServices) {
		cachedServices = createOnisinServices();
	}
	return cachedServices;
}

/** Result of `parseView`: the runtime def, plus diagnostics. */
export interface ParseViewResult {
	/** Runtime ViewDef. Undefined only if the AST root is missing. */
	def: ViewDef | undefined;
	/** Validation and parse diagnostics in LSP shape. */
	diagnostics: Diagnostic[];
}

/**
 * parseView turns DSL source text into a runtime ViewDef.
 *
 * Diagnostics are returned alongside the def so callers can decide
 * whether to render anyway (best-effort) or to abort and surface
 * errors to the user.
 *
 * @param text - The full `.view` source.
 * @param uri  - Optional model URI for diagnostic locations. Pass a
 *               filename or in-memory URI such as
 *               `inmemory://model/person_list`. Defaults to a stable
 *               in-memory URI.
 */
export async function parseView(
	text: string,
	uri = "inmemory://oos-dsl/view",
): Promise<ParseViewResult> {
	const result = await services().parse({ language: "view", uri, text });
	const def = result.root ? mapView(result.root) : undefined;
	return { def, diagnostics: result.diagnostics };
}

/** Result of `parseDomain`: the runtime def, plus diagnostics. */
export interface ParseDomainResult {
	/** Runtime DomainDef. Undefined only if the AST root is missing. */
	def: DomainDef | undefined;
	/** Validation and parse diagnostics in LSP shape. */
	diagnostics: Diagnostic[];
}

/**
 * parseDomain turns DSL source text into a runtime DomainDef.
 *
 * Same contract as parseView: diagnostics ride alongside the def, the
 * mapper is total, and no exception is thrown on syntactic or semantic
 * errors. Cross-references are *not* resolved here; the LLM-chunk
 * renderer reads `optionsRef` by name from the same DomainDef.
 *
 * @param text - The full `.domain` source.
 * @param uri  - Optional model URI; defaults to a stable in-memory URI.
 */
export async function parseDomain(
	text: string,
	uri = "inmemory://oos-dsl/domain",
): Promise<ParseDomainResult> {
	const result = await services().parse({ language: "domain", uri, text });
	const def = result.root ? mapDomain(result.root) : undefined;
	return { def, diagnostics: result.diagnostics };
}
