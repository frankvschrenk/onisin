// kv-completion.ts — Context-aware Monaco completion backed by JetStream KV.
//
// For each registered DSL language a set of "completion contexts" is
// declared: which keyword triggers completions and which KV key holds
// the list of values. When the user types inside a quoted string that
// follows one of those keywords the provider fetches the value list
// from KV and returns them as suggestions.
//
// The KV bucket is "onisin-completion". Keys follow the pattern:
//   <language>.<keyword>   e.g. "pipeline.llm", "pipeline.embedding"
// Values are JSON arrays of strings.
//
// An admin populates these keys via oos.config or directly via
// natsKvPut. The provider caches results for 60s to avoid hammering
// NATS on every keystroke.

import * as monaco from "monaco-editor";
import { rpc }    from "../../mainview/rpc";

const BUCKET = "oos-pipeline";

/** Cache entry with a 60-second TTL. */
interface CacheEntry {
	values:    Suggestion[];
	expiresAt: number;
}

const cache = new Map<string, CacheEntry>();

/**
 * Suggestion is the normalised shape used to build a Monaco completion
 * item. It unifies the two on-disk formats a KV key may hold:
 *   - legacy `string[]`  → { insert: s, display: s }
 *   - current `object[]`  → { insert: alias, display: label ?? alias,
 *                            info: model, doc: description + metadata }
 * Keeping both readable lets us migrate one key at a time.
 */
interface Suggestion {
	/** Text inserted into the document (the alias, or the bare string). */
	insert:  string;
	/** Primary label shown in the list (alias or friendly label). */
	display: string;
	/** Right-aligned detail; for aliases this is the real model name. */
	info?:   string;
	/** Longer markdown documentation shown in the details fly-out. */
	doc?:    string;
}

/**
 * normaliseEntry converts one raw KV array element into a Suggestion,
 * tolerating both the legacy string form and the alias-object form.
 * Disabled entries (enabled === false) are dropped by returning null.
 */
function normaliseEntry(raw: unknown): Suggestion | null {
	if (typeof raw === "string") {
		return { insert: raw, display: raw };
	}
	if (raw && typeof raw === "object" && "alias" in raw) {
		const e = raw as {
			alias: string; model?: string; label?: string; description?: string;
			placement?: string; capabilities?: string[]; contextWindow?: number;
			fallback?: string; enabled?: boolean;
		};
		if (e.enabled === false) return null;

		// Compose a small markdown doc block from the topology metadata so
		// the author sees what an alias resolves to without leaving Monaco.
		const lines: string[] = [];
		if (e.model)         lines.push(`**model:** \`${e.model}\``);
		if (e.placement)     lines.push(`**placement:** ${e.placement}`);
		if (e.capabilities?.length) lines.push(`**capabilities:** ${e.capabilities.join(", ")}`);
		if (e.contextWindow) lines.push(`**context:** ${e.contextWindow} tokens`);
		if (e.fallback)      lines.push(`**fallback:** ${e.fallback}`);
		if (e.description)   lines.push("", e.description);

		return {
			insert:  e.alias,
			display: e.label ?? e.alias,
			info:    e.model ?? e.alias,
			doc:     lines.length > 0 ? lines.join("  \n") : undefined,
		};
	}
	return null;
}

async function fetchValues(key: string): Promise<Suggestion[]> {
	const hit = cache.get(key);
	if (hit && hit.expiresAt > Date.now()) return hit.values;

	try {
		const res = await rpc.natsKvGet({ bucket: BUCKET, key });
		const arr = Array.isArray(res.value) ? (res.value as unknown[]) : [];
		const values = arr
			.map(normaliseEntry)
			.filter((s): s is Suggestion => s !== null);
		cache.set(key, { values, expiresAt: Date.now() + 60_000 });
		return values;
	} catch {
		return [];
	}
}

/**
 * getContextKeyword returns the keyword immediately left of the
 * opening quote at the cursor, or null when the cursor is not inside
 * a quoted string argument.
 *
 * Examples (cursor marked with |):
 *   llm "|          → "llm"
 *   embedding "|    → "embedding"
 *   step llm foo {  → null  (not inside a string)
 */
function getContextKeyword(
	model: monaco.editor.ITextModel,
	position: monaco.Position,
): string | null {
	const line = model.getLineContent(position.lineNumber);
	const col  = position.column - 1; // 0-based

	// Walk left to find the opening quote.
	let quoteIdx = -1;
	for (let i = col - 1; i >= 0; i--) {
		if (line[i] === '"') { quoteIdx = i; break; }
		// Abort on a closing quote to the left — we are not inside a string.
		if (line[i] === '"' && i < col) return null;
	}
	if (quoteIdx < 0) return null;

	// Extract the token immediately before the quote.
	const before = line.slice(0, quoteIdx).trimEnd();
	const match  = before.match(/([\w-]+)\s*$/);
	return match ? match[1]! : null;
}

/** One registered completion context for a language. */
interface CompletionContext {
	/** Keyword that triggers this context, e.g. "llm". */
	keyword: string;
	/** Human-readable detail shown next to each suggestion. */
	detail?: string;
}

const installed = new Set<string>();

/**
 * registerKvCompletion installs an async completion provider for the
 * given Monaco language id. Each context maps a keyword to a KV key
 * under "onisin-completion.<language>.<keyword>".
 *
 * Idempotent — safe to call repeatedly.
 */
export function registerKvCompletion(
	languageId: string,
	contexts:   CompletionContext[],
): void {
	if (installed.has(languageId)) return;
	installed.add(languageId);

	const keywords = new Set(contexts.map((c) => c.keyword));
	const detailMap = new Map(contexts.map((c) => [c.keyword, c.detail ?? c.keyword]));

	monaco.languages.registerCompletionItemProvider(languageId, {
		triggerCharacters: ['"'],
		async provideCompletionItems(model, position) {
			const keyword = getContextKeyword(model, position);
			if (!keyword || !keywords.has(keyword)) return { suggestions: [] };

			const kvKey = keyword;
			const values = await fetchValues(kvKey);
			if (values.length === 0) return { suggestions: [] };

			const word  = model.getWordUntilPosition(position);
			const range: monaco.IRange = {
				startLineNumber: position.lineNumber,
				endLineNumber:   position.lineNumber,
				startColumn:     word.startColumn,
				endColumn:       word.endColumn,
			};

			return {
				suggestions: values.map((v) => ({
					label:      v.display,
					kind:       monaco.languages.CompletionItemKind.Value,
					insertText: v.insert,
					// For aliases, info holds the real model name; fall back to
					// the per-keyword detail label ("LLM model") for legacy entries.
					detail:        v.info ?? detailMap.get(keyword),
					documentation: v.doc ? { value: v.doc } : undefined,
					range,
				})),
			};
		},
	});
}
