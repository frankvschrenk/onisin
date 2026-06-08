// monaco-init.ts — wire Monaco's base worker and pin
// @monaco-editor/react to the bundled Monaco instance.
//
// Must run before any <Editor /> is rendered. See oosd's
// lang/monaco/init.ts for the full explanation.

import * as monaco from "monaco-editor";
import { loader }  from "@monaco-editor/react";
import { registerPipelineLanguage } from "../lang/monaco/pipeline-lang";

let initialised = false;

/**
 * initMonaco wires Monaco's base worker and pins
 * @monaco-editor/react to the bundled Monaco instance.
 * Safe to call repeatedly; only the first call has any effect.
 */
export function initMonaco(): void {
	if (initialised) return;

	(self as unknown as { MonacoEnvironment: monaco.Environment }).MonacoEnvironment = {
		getWorkerUrl(_moduleId: string, _label: string): string {
			return "workers/editor.worker.js";
		},
	};

	loader.config({ monaco });
	registerPipelineLanguage();

	initialised = true;
}
