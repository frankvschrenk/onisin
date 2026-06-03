// types-only.ts — Type-only public surface of oos-dsls-ts.
//
// This entry point re-exports nothing but TypeScript types. It exists
// so consumers (notably oos-ui-ts and oosd's main-thread bundle) can
// import the runtime contract without any value-side dependency on
// Langium. With `isolatedModules: true` and `verbatimModuleSyntax`-
// compatible bundlers, importing from here is guaranteed not to pull
// the Langium-laden services or AST runtime.

export type * from "./types";
export type * from "./generated/ast";
