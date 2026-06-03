// index.ts — Public API of the oos-dsls-ts package.
//
// This package owns the onisin DSL grammars (`.domain`, `.view`),
// the Langium-generated AST, and the AST → runtime mappers that
// produce renderer-ready definitions.
//
// Three layers, all exported:
//
//   1. Raw services and AST types — for advanced consumers (the
//      diagnostics worker, completion provider, etc.).
//   2. Runtime types (ViewDef, DomainDef, ...) — the contract
//      consumed by `oos-ui-ts` and any other renderer.
//   3. Convenience parsers (`parseView`, `parseDomain`) and direct
//      mappers, plus the LLM-chunk renderer for embedding pipelines.

// Layer 1 — services + AST.
export * from "./services";
export type * from "./generated/ast";

// Layer 2 — runtime types.
export * from "./types";

// Layer 3 — convenience parsers, direct mappers, renderers.
export { mapView } from "./mapper/view";
export { mapDomain } from "./mapper/domain";
export { parseView, parseDomain } from "./parse";
export { renderLLMChunk } from "./renderer/llm-chunk";
export { renderViewChunk } from "./renderer/view-chunk";
export { domainAliases } from "./renderer/aliases";

// Layer 3a — operator catalog. Re-exported so downstream packages
// (notably oos-gql-ts) can derive GraphQL filter argument names from
// the same source the LLM-chunk renderer uses. Single source of
// truth for which operators each field type supports.
export {
	operatorsForType,
	findOperator,
	formatExampleValue,
	renderFilterArg,
	type FilterOp,
} from "./renderer/operators";
