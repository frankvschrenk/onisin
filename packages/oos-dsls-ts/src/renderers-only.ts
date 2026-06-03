// renderers-only.ts — Pure-renderer public surface of oos-dsls-ts.
//
// Companion to `types-only.ts`. Re-exports the chunk renderers and
// the alias generator — every value here is a function from a
// runtime def (DomainDef, ViewDef) to a string, with no Langium
// dependency anywhere in its transitive imports.
//
// Why a separate entry point: the package's main `index.ts` bundles
// the Langium services for the convenience of the worker side
// (`createOnisinServices`, `parseDomain`, `parseView`). A consumer
// that only renders chunks — most notably oosd's main-thread
// ChunkPanel — must NOT pull Langium into its bundle, because
// vscode-jsonrpc's cancellation namespace import trips up
// Electrobun's main-thread bundler ("Can't find variable:
// exports_cancellation"). Importing through this entry guarantees
// the Langium tail stays in the worker bundle where it belongs.

export { renderLLMChunk } from "./renderer/llm-chunk";
export { renderViewChunk } from "./renderer/view-chunk";
export { domainAliases } from "./renderer/aliases";
