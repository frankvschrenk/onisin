// register.ts — register the onisin DSL languages with Monaco.
//
// Both registration and Monarch installation are idempotent: the
// helper records which languages it has already wired up so callers
// don't need to coordinate. SourceEditor calls this once on first
// mount.

import * as monaco from "monaco-editor";

import domainMonarch      from "oos-dsls-ts/monarch/domain";
import viewMonarch        from "oos-dsls-ts/monarch/view";
import eventSchemaMonarch from "oos-dsls-ts/monarch/event-schema";
import langiumMonarch     from "./langium.monarch";

import type { Kind } from "../../mainview/types";
import { registerOnisinCompletion } from "./completion";

/** Monaco language id used for the onisin domain DSL. */
export const DOMAIN_LANGUAGE_ID = "onisin-domain";

/** Monaco language id used for the onisin view DSL. */
export const VIEW_LANGUAGE_ID = "onisin-view";

/** Monaco language id used for the onisin event-schema DSL. */
export const EVENT_SCHEMA_LANGUAGE_ID = "onisin-event-schema";

/** Monaco language id used for raw Langium grammar source. */
export const LANGIUM_LANGUAGE_ID = "onisin-langium";

/** monacoLanguageFor maps the kind discriminator to the Monaco language id. */
export function monacoLanguageFor(kind: Kind): string {
	if (kind === "domain")       return DOMAIN_LANGUAGE_ID;
	if (kind === "event-types")  return EVENT_SCHEMA_LANGUAGE_ID;
	return VIEW_LANGUAGE_ID;
}

const registered = new Set<string>();

/**
 * registerOnisinLanguages installs both DSL languages with Monaco
 * — id, Monarch tokens, language config, and completion providers.
 * Safe to call repeatedly; subsequent calls are no-ops.
 */
export function registerOnisinLanguages(): void {
	registerOne(DOMAIN_LANGUAGE_ID,       [".domain"],       domainMonarch);
	registerOne(VIEW_LANGUAGE_ID,         [".view"],         viewMonarch);
	registerOne(EVENT_SCHEMA_LANGUAGE_ID, [".event-schema"], eventSchemaMonarch);
	registerOne(LANGIUM_LANGUAGE_ID,      [".langium"],      langiumMonarch);
	registerOnisinCompletion();
}

function registerOne(
	id: string,
	extensions: string[],
	monarch: monaco.languages.IMonarchLanguage,
) {
	if (registered.has(id)) return;

	monaco.languages.register({ id, extensions });
	monaco.languages.setMonarchTokensProvider(id, monarch);
	monaco.languages.setLanguageConfiguration(id, {
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

	registered.add(id);
}
