// mainview/embedder/dsl-resolver.ts — Pre-agent DSL intent resolver.
//
// Answers: "Which domain(s) and view(s) does the user mean?"
// before any LLM round-trip happens. The answer is passed to the
// agent as pre-loaded context so it never has to guess.
//
// Two recognition paths, same as oos/store/resolver.ts:
//
//   Phase 1 (sync)  — keyword match against domain/view ids and
//                     names. Fast, zero false positives on exact
//                     matches. Handles "unfall", "auto_schaden".
//
//   Phase 2 (async) — MiniLM cosine similarity. Handles typos,
//                     synonyms, German plurals: "Unfälle" → unfall,
//                     "Schadensvorgang" → auto_schaden.
//                     Gated by embedder.isReady(); falls back to
//                     Phase 1 when model is not yet loaded.
//
// The index is loaded once on first use via NATS (oos.cmd.domain.list
// + oos.cmd.view.list) and kept for the session.

import {
	cosine,
	embedText,
	isReady,
	loadEmbedder,
	maxCosine,
} from "./embedder";

// ─── Types ────────────────────────────────────────────────────────────

export interface DslEntry {
	id:      string;   // domain or view id
	kind:    "domain" | "view";
	/** Aliases used for embedding — id + name + common synonyms. */
	aliases: string[];
}

export interface ResolveIntent {
	/** Domain ids that matched — may be empty. */
	domainIds: string[];
	/** View id that matched — at most one. */
	viewId?:   string;
	/** Score of best embedding match (0..1). Undefined for keyword. */
	score?:    number;
}

// ─── Tunables ─────────────────────────────────────────────────────────

const EMBED_HIGH = 0.68;
const EMBED_LOW  = 0.52;

// ─── In-memory index ──────────────────────────────────────────────────

let entries:  DslEntry[] | null = null;
let building: Promise<void> | null = null;

// Alias-vector cache — built lazily after index + embedder are ready.
interface EntryVectors { entry: DslEntry; vectors: Float32Array[] }
let vecCache: EntryVectors[] | null = null;
let vecBuilding: Promise<void> | null = null;

// ─── Public API ───────────────────────────────────────────────────────

/**
 * initDslResolver loads the domain/view index from NATS and kicks
 * off MiniLM model loading in the background. Safe to call multiple
 * times — subsequent calls while loading are no-ops.
 *
 * `rpcListDomains` and `rpcListViews` are injected so this module
 * has no direct RPC dependency (easier to test, keeps imports clean).
 */
export async function initDslResolver(
	rpcListDomains: () => Promise<{ ids: string[] }>,
	rpcListViews:   () => Promise<{ ids: string[] }>,
): Promise<void> {
	if (entries) return;
	if (building) return building;

	building = (async () => {
		const [domains, views] = await Promise.all([
			rpcListDomains(),
			rpcListViews(),
		]);

		const all: DslEntry[] = [
			...domains.ids.map((id) => ({
				id,
				kind: "domain" as const,
				aliases: buildAliases(id),
			})),
			...views.ids.map((id) => ({
				id,
				kind: "view" as const,
				aliases: buildAliases(id),
			})),
		];

		entries = all;
		building = null;
		vecCache = null;
		vecBuilding = null;

		// Kick off model loading in the background — don't await,
		// the resolver falls back to keyword matching while it loads.
		void loadEmbedder();


	})();

	return building;
}

/**
 * resolveIntent returns which domains and views the user text refers
 * to. Tries embedding first (if model is ready), falls back to
 * keyword matching. Never throws.
 */
export async function resolveIntent(text: string): Promise<ResolveIntent> {
	if (!entries) return { domainIds: [] };

	if (isReady()) {
		try {
			return await resolveWithEmbedding(text);
		} catch {
			// Fall through to keyword path.
		}
	}

	return resolveKeyword(text);
}

// ─── Phase 1 — Keyword ───────────────────────────────────────────────

function resolveKeyword(text: string): ResolveIntent {
	if (!entries) return { domainIds: [] };
	const lower = text.toLowerCase();

	const domainIds: string[] = [];
	let viewId: string | undefined;

	for (const e of entries) {
		if (!e.aliases.some((a) => containsWord(lower, a))) continue;
		if (e.kind === "domain") domainIds.push(e.id);
		else if (!viewId) viewId = e.id;
	}

	return { domainIds, viewId };
}

// ─── Phase 2 — Embedding ─────────────────────────────────────────────

async function resolveWithEmbedding(text: string): Promise<ResolveIntent> {
	if (!entries) return { domainIds: [] };

	const vecs = await ensureVectors();
	const queryVec = await embedText(text);

	let bestDomain: EntryVectors | null = null;
	let bestView:   EntryVectors | null = null;
	let bestDScore = -Infinity;
	let bestVScore = -Infinity;

	for (const ev of vecs) {
		const { score } = maxCosine(queryVec, ev.vectors);
		if (ev.entry.kind === "domain") {
			if (score > bestDScore) { bestDScore = score; bestDomain = ev; }
		} else {
			if (score > bestVScore) { bestVScore = score; bestView = ev; }
		}
	}

	// Below LOW threshold → keyword fallback.
	if ((!bestDomain || bestDScore < EMBED_LOW) && (!bestView || bestVScore < EMBED_LOW)) {
		return resolveKeyword(text);
	}

	const domainIds: string[] = [];
	let viewId: string | undefined;
	let score: number | undefined;

	if (bestDomain && bestDScore >= EMBED_HIGH) {
		domainIds.push(bestDomain.entry.id);
		score = bestDScore;
	} else if (bestDomain && bestDScore >= EMBED_LOW) {
		// Ambiguous — check keyword too.
		const kw = resolveKeyword(text);
		if (kw.domainIds.length > 0) return kw;
		domainIds.push(bestDomain.entry.id);
		score = bestDScore;
	}

	if (bestView && bestVScore >= EMBED_HIGH) {
		viewId = bestView.entry.id;
	} else if (bestView && bestVScore >= EMBED_LOW) {
		const kw = resolveKeyword(text);
		if (kw.viewId) return kw;
		viewId = bestView.entry.id;
	}

	return { domainIds, viewId, score };
}

// ─── Helpers ──────────────────────────────────────────────────────────

/**
 * buildAliases expands an id like "auto_schaden_detail" into
 * multiple surface forms: the id itself, underscore-separated words
 * ("auto schaden detail"), and a camelCase-split variant. The alias
 * bundle gives MiniLM more signal to work with.
 */
function buildAliases(id: string): string[] {
	const parts = id.split("_").filter(Boolean);
	return [
		id,                          // "auto_schaden_detail"
		parts.join(" "),             // "auto schaden detail"
		parts.join(""),              // "autoschadendetail"
	].filter((a, i, arr) => arr.indexOf(a) === i);
}

async function ensureVectors(): Promise<EntryVectors[]> {
	if (vecCache) return vecCache;
	if (vecBuilding) { await vecBuilding; return vecCache ?? []; }

	vecBuilding = (async () => {
		const all = entries ?? [];
		const built: EntryVectors[] = [];
		for (const entry of all) {
			const vectors = await Promise.all(entry.aliases.map((a) => embedText(a)));
			built.push({ entry, vectors });
		}
		vecCache = built;
	})();

	try { await vecBuilding; } finally { vecBuilding = null; }
	return vecCache ?? [];
}

function containsWord(haystack: string, needle: string): boolean {
	if (!needle) return false;
	let from = 0;
	while (from <= haystack.length - needle.length) {
		const idx = haystack.indexOf(needle, from);
		if (idx < 0) return false;
		const before = idx === 0 ? "" : haystack[idx - 1]!;
		const after  = idx + needle.length === haystack.length
			? "" : haystack[idx + needle.length]!;
		if (!isWordChar(before) && !isWordChar(after)) return true;
		from = idx + 1;
	}
	return false;
}

function isWordChar(ch: string): boolean {
	return !!ch && /[a-z0-9_äöüß]/i.test(ch);
}
