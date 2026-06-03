// monaco-init.ts — make @monaco-editor/react use our local Monaco and
// teach Monaco where to find its base web worker.
//
// Two unrelated stitching jobs live here, but they have to run in this
// order, before any editor mounts:
//
//   1. self.MonacoEnvironment.getWorkerUrl
//      Monaco wants to spawn its own worker for tokenization and the
//      language pipeline. Without a worker URL it logs a warning and
//      falls back to running everything on the main thread — and in
//      that mode our Monarch highlighter never gets installed. We
//      point it at the editor.worker.js bundled into views/mainview/
//      workers/ by scripts/build-workers.ts.
//
//   2. loader.config({ monaco })
//      By default `@monaco-editor/loader` fetches Monaco from a CDN,
//      which gives us a *second* Monaco instance distinct from the
//      one we get via `import * as monaco from "monaco-editor"`.
//      That split breaks every cross-cutting feature: language
//      registration, marker writes, model lookups, theme application
//      — anything done on one instance is invisible to the other.
//      Pinning the loader to the bundled instance unifies them.

import * as monaco from "monaco-editor";
import { loader } from "@monaco-editor/react";

let initialised = false;

/**
 * initMonaco wires Monaco's base worker and pins
 * `@monaco-editor/react` to the bundled Monaco instance. Safe to call
 * repeatedly; only the first call has any effect. Must run before
 * any `<Editor />` is rendered.
 */
export function initMonaco(): void {
	if (initialised) return;

	// Step 1: hand Monaco a worker URL so it does not fall back to
	// main-thread mode. We only need the generic editor worker for our
	// custom DSLs — no JSON/TS/CSS workers required.
	(self as unknown as { MonacoEnvironment: monaco.Environment }).MonacoEnvironment = {
		getWorkerUrl(_moduleId: string, _label: string): string {
			return "workers/editor.worker.js";
		},
	};

	// Step 2: pin @monaco-editor/react to the bundled Monaco.
	loader.config({ monaco });

	initialised = true;
}
