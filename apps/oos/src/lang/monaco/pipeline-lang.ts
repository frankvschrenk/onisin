// pipeline-lang.ts — Register the onisin-pipeline Monaco language in oos.
//
// Installs the Monarch syntax highlighter from oos-pipeline-ts and
// wires the KV completion provider for all string-argument keywords.
// Idempotent — safe to call on every editor mount.

import * as monaco          from "monaco-editor";
import pipelineMonarch      from "oos-pipeline-ts/monarch/pipeline";
import { registerKvCompletion } from "./kv-completion";

export const PIPELINE_LANGUAGE_ID = "onisin-pipeline";

let registered = false;

/**
 * registerPipelineLanguage installs the onisin-pipeline language with
 * Monaco. Must be called before the first <Editor language="onisin-pipeline" />
 * is rendered.
 */
export function registerPipelineLanguage(): void {
	if (registered) return;
	registered = true;

	monaco.languages.register({ id: PIPELINE_LANGUAGE_ID, extensions: [".pipeline"] });
	monaco.languages.setMonarchTokensProvider(PIPELINE_LANGUAGE_ID, pipelineMonarch as monaco.languages.IMonarchLanguage);
	monaco.languages.setLanguageConfiguration(PIPELINE_LANGUAGE_ID, {
		comments: { lineComment: "//", blockComment: ["/*", "*/"] },
		brackets: [["{", "}"]],
		autoClosingPairs: [
			{ open: "{", close: "}" },
			{ open: '"', close: '"' },
		],
		surroundingPairs: [
			{ open: "{", close: "}" },
			{ open: '"', close: '"' },
		],
	});

	// KV-backed completions for string arguments.
	// Admin populates bucket "oos-pipeline", keys matching the keyword (e.g. "llm", "embedding").
	registerKvCompletion(PIPELINE_LANGUAGE_ID, [
		{ keyword: "llm",       detail: "LLM model"           },
		{ keyword: "embedding", detail: "Embedding model"     },
		{ keyword: "fallback",  detail: "Fallback model"      },
		{ keyword: "pipeline",  detail: "Pipeline name"       },
		{ keyword: "system",    detail: "System prompt"       },
		{ keyword: "prompt",    detail: "User prompt"         },
	]);
}
