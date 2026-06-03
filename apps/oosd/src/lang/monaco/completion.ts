// completion.ts — register CompletionItemProviders for both onisin DSLs.
//
// Every keyword from the Monarch highlighter is offered as a plain
// completion. On top of that we register a generous set of snippets
// — schablonen with tab-stops — for the constructs people reach for
// often: `view`, `domain`, `section`, `field`, `text`, `column`, ...
//
// The provider is context-free for now: it offers everything,
// everywhere. Real grammar-aware completion (only what the rule at
// the cursor actually allows) is a step 2 item that needs to call
// into the Langium services. This step 1 already removes most of
// the "what was the keyword again?" friction.

import * as monaco from "monaco-editor";

import domainMonarch from "oos-dsls-ts/monarch/domain";
import viewMonarch from "oos-dsls-ts/monarch/view";

import { DOMAIN_LANGUAGE_ID, VIEW_LANGUAGE_ID } from "./register";

// ─── Snippet definitions ─────────────────────────────────────────────
//
// Monaco's snippet syntax mirrors VSCode: `$1`, `$2`, ... are tab
// stops; `${1:label}` is a tab stop with a placeholder; `$0` is the
// final caret position.

type Snippet = {
	label: string; // shown in the completion list
	detail: string; // hint shown on the right-hand side
	body: string;
};

const DOMAIN_SNIPPETS: Snippet[] = [
	{
		label: "domain",
		detail: "domain block",
		body: [
			"domain ${1:name} from ${2:source}@demo {",
			"\t$0",
			"}",
		].join("\n"),
	},
	{
		label: "permission",
		detail: "permission line",
		body: "permission ${1:role} ${2:read}",
	},
	{
		label: "field",
		detail: "field declaration",
		body: "field ${1:name} : ${2:string}",
	},
	{
		label: "field-block",
		detail: "field with body (filterable + examples)",
		body: [
			"field ${1:name} : ${2:string} filterable {",
			"\texample ${3:eq} ${4:\"value\"} \"${5:description}\"",
			"}",
		].join("\n"),
	},
	{
		label: "example",
		detail: "filterable example",
		body: 'example ${1:eq} ${2:"value"} "${3:description}"',
	},
	{
		label: "relation-has-many",
		detail: "1:n relation",
		body: "relation ${1:name} : has_many ${2:target} bind=id -> ${3:fk}",
	},
	{
		label: "relation-has-one",
		detail: "1:1 relation",
		body: "relation ${1:name} : has_one ${2:target} bind=${3:fk} -> id",
	},
	{
		label: "relation-belongs-to",
		detail: "n:1 relation",
		body: "relation ${1:name} : belongs_to ${2:target} bind=${3:fk} -> id",
	},
	{
		label: "meta",
		detail: "dropdown source",
		body: "meta ${1:name} from ${2:table} ${3:key} ${4:label} order_by ${5:label}",
	},
	{
		label: "ai",
		detail: "AI hint block",
		body: ['ai "${1:topic}"', '   "${2:guidance}"'].join("\n"),
	},
];

const VIEW_SNIPPETS: Snippet[] = [
	// ─── Top-level scaffolds ────────────────────────────────────────
	{
		label: "view",
		detail: "view block",
		body: [
			'view ${1:name} "${2:Title}" over ${3:domain} {',
			"\t$0",
			"}",
		].join("\n"),
	},
	{
		label: "view-detail",
		detail: "detail view skeleton",
		body: [
			'view ${1:name}_detail "${2:Title}" over ${3:domain} {',
			"",
			"\ttoolbar {",
			"\t\tsave",
			"\t\texit",
			"\t}",
			"",
			'\tsection "${4:Daten}" p=md {',
			"\t\t$0",
			"\t}",
			"}",
		].join("\n"),
	},
	{
		label: "view-list",
		detail: "list view skeleton",
		body: [
			'view ${1:name}_list "${2:Title}" over ${3:domain} {',
			"",
			"\ttoolbar {",
			'\t\tnew "Neu" -> ${1:name}_detail as tab',
			"\t}",
			"",
			"\ttable -> ${3:domain}.rows {",
			"\t\ton_select -> ${1:name}_detail bind=${3:domain}.id as tab",
			"",
			"\t\tcolumn ${3:domain}.${4:id} \"${5:ID}\" width=${6:60}",
			"\t\t$0",
			"\t}",
			"}",
		].join("\n"),
	},

	// ─── Layout containers ──────────────────────────────────────────
	{
		label: "toolbar",
		detail: "toolbar block",
		body: ["toolbar {", "\t$0", "}"].join("\n"),
	},
	{
		label: "section",
		detail: "section block",
		body: ['section "${1:label}" p=md {', "\t$0", "}"].join("\n"),
	},
	{
		label: "tabs",
		detail: "tabs container",
		body: ["tabs p=sm {", "\t$0", "}"].join("\n"),
	},
	{
		label: "tab",
		detail: "single tab",
		body: ['tab "${1:label}" {', "\t$0", "}"].join("\n"),
	},
	{
		label: "grid",
		detail: "grid container",
		body: ["grid cols=${1:2} gap=md {", "\t$0", "}"].join("\n"),
	},
	{
		label: "row",
		detail: "row container",
		body: ["row p=md {", "\t$0", "}"].join("\n"),
	},
	{
		label: "stack",
		detail: "stack container",
		body: ["stack {", "\t$0", "}"].join("\n"),
	},
	{
		label: "card",
		detail: "card container",
		body: ["card p=md {", "\t$0", "}"].join("\n"),
	},
	{
		label: "accordion",
		detail: "accordion container",
		body: ["accordion p=md {", "\t$0", "}"].join("\n"),
	},
	{
		label: "item",
		detail: "accordion item",
		body: ['item "${1:label}" {', "\t$0", "}"].join("\n"),
	},
	{
		label: "richtext",
		detail: "richtext block",
		body: [
			"richtext {",
			'\theading "${1:Title}"',
			'\tbold    "${2:Subtitle}"',
			"}",
		].join("\n"),
	},

	// ─── Fields / inputs ────────────────────────────────────────────
	{ label: "text",       detail: "text field",       body: 'text "${1:label}" -> ${2:domain.field}' },
	{ label: "textarea",   detail: "textarea field",   body: 'textarea "${1:label}" -> ${2:domain.field} placeholder="$3"' },
	{ label: "number",     detail: "number field",     body: 'number "${1:label}" -> ${2:domain.field}' },
	{ label: "email",      detail: "email field",      body: 'email "${1:label}" -> ${2:domain.field}' },
	{ label: "password",   detail: "password field",   body: 'password "${1:label}" -> ${2:domain.field}' },
	{ label: "date",       detail: "date field",       body: 'date "${1:label}" -> ${2:domain.field}' },
	{ label: "datetime",   detail: "datetime field",   body: 'datetime "${1:label}" -> ${2:domain.field}' },
	{ label: "time",       detail: "time field",       body: 'time "${1:label}" -> ${2:domain.field}' },
	{ label: "select",     detail: "dropdown field",   body: 'select "${1:label}" -> ${2:domain.field}' },
	{ label: "combobox",   detail: "combobox field",   body: 'combobox "${1:label}" -> ${2:domain.field}' },
	{ label: "multiselect",detail: "multi-select",     body: 'multiselect "${1:label}" -> ${2:domain.field}' },
	{ label: "radio",      detail: "radio group",      body: 'radio "${1:label}" -> ${2:domain.field}' },
	{ label: "check",      detail: "checkbox",         body: 'check "${1:label}" -> ${2:domain.field}' },
	{ label: "switch",     detail: "switch",           body: 'switch "${1:label}" -> ${2:domain.field}' },
	{ label: "slider",     detail: "slider",           body: 'slider "${1:label}" -> ${2:domain.field} min=${3:0} max=${4:100} step=${5:1}' },
	{ label: "rangeslider",detail: "range slider",     body: 'rangeslider "${1:label}" -> ${2:domain.field} min=${3:0} max=${4:100}' },
	{ label: "rating",     detail: "rating field",     body: 'rating "${1:label}" -> ${2:domain.field}' },
	{ label: "progress",   detail: "progress bar",     body: 'progress "${1:label}" -> ${2:domain.field}' },
	{ label: "color",      detail: "color picker",     body: 'color "${1:label}" -> ${2:domain.field}' },
	{ label: "file",       detail: "file picker",      body: 'file "${1:label}" -> ${2:domain.field}' },

	// ─── Tables ─────────────────────────────────────────────────────
	{
		label: "table",
		detail: "table block",
		body: [
			"table -> ${1:domain}.rows {",
			"\ton_select -> ${2:detail_view} bind=${1:domain}.id as tab",
			"",
			'\tcolumn ${1:domain}.${3:id} "${4:ID}" width=${5:60}',
			"\t$0",
			"}",
		].join("\n"),
	},
	{
		label: "column",
		detail: "table column",
		body: 'column ${1:domain.field} "${2:label}" width=${3:120}',
	},

	// ─── Toolbar items ──────────────────────────────────────────────
	{ label: "save",   detail: "toolbar save",   body: "save" },
	{ label: "exit",   detail: "toolbar exit",   body: "exit" },
	{
		label: "delete",
		detail: "toolbar delete",
		body: 'delete confirm="${1:Wirklich löschen?}"',
	},
	{
		label: "new",
		detail: "toolbar new",
		body: 'new "${1:Neu}" -> ${2:detail_view} as tab',
	},

	// ─── Misc ───────────────────────────────────────────────────────
	{ label: "icon",    detail: "icon",    body: 'icon "${1:account}" size=${2:24}' },
	{ label: "divider", detail: "divider", body: "divider" },
	{ label: "sep",     detail: "separator", body: "sep" },
	{
		label: "on_select",
		detail: "row click handler",
		body: "on_select -> ${1:detail_view} bind=${2:domain.id} as tab",
	},
];

// ─── Provider registration ───────────────────────────────────────────

const installed = new Set<string>();

/**
 * registerOnisinCompletion installs CompletionItemProviders for both
 * DSLs. Idempotent — call as often as needed.
 */
export function registerOnisinCompletion(): void {
	if (!installed.has(DOMAIN_LANGUAGE_ID)) {
		monaco.languages.registerCompletionItemProvider(
			DOMAIN_LANGUAGE_ID,
			buildProvider(domainMonarch.keywords as string[], DOMAIN_SNIPPETS),
		);
		installed.add(DOMAIN_LANGUAGE_ID);
	}
	if (!installed.has(VIEW_LANGUAGE_ID)) {
		monaco.languages.registerCompletionItemProvider(
			VIEW_LANGUAGE_ID,
			buildProvider(viewMonarch.keywords as string[], VIEW_SNIPPETS),
		);
		installed.add(VIEW_LANGUAGE_ID);
	}
}

function buildProvider(
	keywords: string[],
	snippets: Snippet[],
): monaco.languages.CompletionItemProvider {
	// Snippet labels override plain keywords with the same name so we
	// don't end up offering both "view" the keyword and "view" the
	// snippet.
	const snippetLabels = new Set(snippets.map((s) => s.label));

	return {
		provideCompletionItems(model, position) {
			const word = model.getWordUntilPosition(position);
			const range: monaco.IRange = {
				startLineNumber: position.lineNumber,
				endLineNumber: position.lineNumber,
				startColumn: word.startColumn,
				endColumn: word.endColumn,
			};

			const suggestions: monaco.languages.CompletionItem[] = [];

			// `sortText` overrides Monaco's default alphabetical ordering.
			// Snippets get a leading "0" so they sort above keywords ("1").
			// Within each group, secondary sort is by label so the list
			// stays predictable.
			for (const s of snippets) {
				suggestions.push({
					label: s.label,
					kind: monaco.languages.CompletionItemKind.Snippet,
					insertText: s.body,
					insertTextRules:
						monaco.languages.CompletionItemInsertTextRule.InsertAsSnippet,
					detail: s.detail,
					sortText: `0${s.label}`,
					range,
				});
			}

			for (const kw of keywords) {
				if (snippetLabels.has(kw)) continue;
				suggestions.push({
					label: kw,
					kind: monaco.languages.CompletionItemKind.Keyword,
					insertText: kw,
					sortText: `1${kw}`,
					range,
				});
			}

			return { suggestions };
		},
	};
}
