// langium.monarch.ts — Monarch syntax highlighting for Langium grammar files.
//
// Derived from the official Langium VSCode extension tmLanguage:
// https://github.com/eclipse-langium/langium/blob/main/packages/langium-vscode/data/langium.tmLanguage.json
//
// Covers:
//   - Keywords: grammar, entry, fragment, terminal, hidden, import,
//     returns, infer, infers, interface, type, extends, with, current, etc.
//   - Single-quoted strings  'keyword'  (terminal literals)
//   - Double-quoted strings  "string"
//   - Regex literals         /pattern/flags  (terminal bodies)
//   - Symbols and operators: { } : [ ] ( ) += ?= = -> => | & ? * + @ ! ;
//   - Boolean literals: true, false
//   - Line and block comments

import type * as monaco from "monaco-editor";

const langiumMonarch: monaco.languages.IMonarchLanguage = {
	// Official keyword list from the tmLanguage
	keywords: [
		"left", "right", "assoc", "current",
		"entry", "extends", "fragment",
		"grammar", "hidden", "import",
		"infer", "infers", "infix",
		"interface", "returns", "terminal",
		"type", "with", "on",
	],

	constants: ["true", "false"],

	tokenizer: {
		initial: [
			// Regex literals /.../ — must come before operators so / is not eaten
			{ regex: /\/(?:[^\\/\n[]|\\.|\[(?:[^\]\\]|\\.)*\])+\/[gimsuy]*/, action: { token: "regexp" } },

			// Single-quoted strings (Langium terminal keyword literals)
			{
				regex: /'/,
				action: { token: "string.single", next: "@stringSingle" },
			},

			// Double-quoted strings
			{
				regex: /"/,
				action: { token: "string", next: "@stringDouble" },
			},

			// Line comments
			{ regex: /\/\/[^\n\r]*/, action: { token: "comment" } },

			// Block comments
			{ regex: /\/\*/, action: { token: "comment", next: "@blockComment" } },

			// Keywords and identifiers
			{
				regex: /\b[_a-zA-Z][\w_]*\b/,
				action: {
					cases: {
						"@keywords":  { token: "keyword" },
						"@constants": { token: "constant.language" },
						"@default":   { token: "identifier" },
					},
				},
			},

			// Numbers
			{ regex: /-?\d+(\.\d+)?/, action: { token: "number" } },

			// Operators and punctuation (from official tmLanguage keyword.symbol)
			{ regex: /\{|\}/, action: { token: "delimiter.curly" } },
			{ regex: /\[|\]/, action: { token: "delimiter.square" } },
			{ regex: /\(|\)/, action: { token: "delimiter.paren" } },
			{ regex: /=>|->/, action: { token: "keyword.operator" } },
			{ regex: /\?\?=|\+=|\?=|=/, action: { token: "operator" } },
			{ regex: /[|&]/, action: { token: "operator" } },
			{ regex: /[?*+!@;,:<>]/, action: { token: "operator" } },

			// Whitespace
			{ regex: /\s+/, action: { token: "white" } },
		],

		stringSingle: [
			{ regex: /\\./, action: { token: "string.escape" } },
			{ regex: /'/, action: { token: "string.single", next: "@pop" } },
			{ regex: /[^'\\]+/, action: { token: "string.single" } },
		],

		stringDouble: [
			{ regex: /\\./, action: { token: "string.escape" } },
			{ regex: /"/, action: { token: "string", next: "@pop" } },
			{ regex: /[^"\\]+/, action: { token: "string" } },
		],

		blockComment: [
			{ regex: /[^/*]+/, action: { token: "comment" } },
			{ regex: /\*\//, action: { token: "comment", next: "@pop" } },
			{ regex: /[/*]/, action: { token: "comment" } },
		],
	},
};

export default langiumMonarch;
