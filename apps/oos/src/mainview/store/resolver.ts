// store/resolver.ts — Pre-LLM domain and view resolver.
//
// The resolver runs in the renderer before any LLM round-trip.
// Given a free-text user input, it answers:
//
//   1. Did the user mention a known domain? (alias matching)
//   2. Which views exist for that domain, and which one is the
//      default?
//   3. Did the user name a specific view by id?
//
// The point is to feed the agent loop a precise hint —
// "use view `person_list` and select only these fields" — instead
// of letting the LLM rediscover the domain from scratch every
// turn. Faster, cheaper, and the result tab gets the right
// columns up front.
//
// Two recognition paths live side by side:
//
//   * Phase 1 — `resolveText` (sync). Plain keyword matching:
//     each domain comes with a canonical alias list from the
//     backend (see `oos-dsls-ts/renderer/aliases.ts`); the
//     resolver lower-cases both the user text and the aliases
//     and looks for a substring hit on a word boundary.
//     Case-insensitive, language-agnostic, no false positives on
//     partial words. Cheap enough to run on every keystroke.
//
//   * Phase 2 — `resolveTextWithEmbedding` (async). Uses a local
//     MiniLM embedder (see ../embedder/embedder.ts) to score the
//     user query against every alias of every domain, taking the
//     max-cosine per domain. Catches plurals and synonyms that
//     the keyword path misses (e.g. "Personen" → person,
//     "Mitarbeiter" → person). Gated by `embedder.isReady()`;
//     when the model is still loading or has failed, falls back
//     to the keyword path so the user never blocks on a model.
//
// Loading model: one network round-trip on App mount fetches
// /domains and /views from oosai in parallel. The catalogues are
// small (a handful of rows × a few hundred bytes each) and rarely
// change, so we cache for the lifetime of the renderer process.
// pg_notify-driven invalidation can come later.

import { useEffect, useState } from "react";

import { embedText, isReady, maxCosine } from "../embedder/embedder";
import { rpc } from "../rpc";
import { loadAppSettings } from "./settings";

// ─── Types ───────────────────────────────────────────────────────────

/** One domain as exposed by oosai's /domains endpoint. */
export interface DomainIndexEntry {
	name:    string;
	source:  string;
	scope?:  string;
	aliases: string[];
}

/** One view as exposed by oosai's /views endpoint. */
export interface ViewIndexEntry {
	name:    string;
	title:   string;
	domain:  string;
	default: boolean;
	fields:  string[];
}

/** Result of resolveText for one user input. */
export interface ResolveResult {
	/** The matched domain, when one was recognised. */
	domain?:     DomainIndexEntry;
	/** Every view bound to the matched domain (default first). */
	views:       ViewIndexEntry[];
	/**
	 * The view the agent should target right now. Either the one
	 * the user named explicitly, or the domain's default. May be
	 * undefined when no domain was recognised at all, or when the
	 * matched domain has no views yet.
	 */
	suggestion?: ViewIndexEntry;
	/**
	 * Set when the embedding path made the call. Useful for the
	 * UI to show "matched on: <alias>" hints. Undefined when the
	 * keyword path resolved.
	 */
	matchedAlias?: string;
	/** Score of the embedding match (0..1). Undefined for keyword. */
	score?: number;
}

/** Current state of the resolver index. */
export type ResolverState =
	| { kind: "loading" }
	| { kind: "ready"; domains: DomainIndexEntry[]; views: ViewIndexEntry[] }
	| { kind: "error"; message: string };

// ─── Tunables ────────────────────────────────────────────────────────

/**
 * Score thresholds for the embedding path, calibrated against
 * paraphrase-multilingual-MiniLM-L12-v2 with max-cosine
 * aggregation over an alias bundle (see scripts/embed-smoketest.ts):
 *
 *   * `>= EMBED_THRESHOLD_HIGH`  → confident match, take it.
 *   * `>= EMBED_THRESHOLD_LOW`   → ambiguous; fall through to the
 *                                  keyword path. If keyword also
 *                                  hits, keyword wins (more
 *                                  specific).
 *   * `<  EMBED_THRESHOLD_LOW`   → no match, keyword fallback.
 *
 * The smoke test showed positives clustering around 0.57–0.95 and
 * negatives at 0.20–0.55 with this model. 0.55 is a tight gap —
 * better domain alias lists (more synonyms per domain) will widen
 * it; in the meantime the keyword path catches what slips through.
 */
const EMBED_THRESHOLD_HIGH = 0.70;
const EMBED_THRESHOLD_LOW  = 0.55;

// ─── In-memory cache + bus ───────────────────────────────────────────

let state: ResolverState = { kind: "loading" };

type Listener = (s: ResolverState) => void;
const listeners = new Set<Listener>();

function notify(): void {
	for (const fn of listeners) fn(state);
}

function setState(next: ResolverState): void {
	state = next;
	notify();
}

/**
 * Cache of alias-vectors keyed by domain name. Built lazily on
 * first embedding call after the index is ready and the embedder
 * has loaded — neither side blocks the other. Cleared whenever
 * the index transitions away from "ready".
 *
 * Each entry holds one Float32Array per alias of that domain,
 * preserving order so we can map argmax back to the alias text.
 */
interface DomainVectors {
	aliases: string[];
	vectors: Float32Array[];
}
let aliasVectors: Map<string, DomainVectors> | null = null;
let aliasVectorsBuilding: Promise<void> | null = null;

// ─── Public API ──────────────────────────────────────────────────────

/**
 * loadResolverIndex hits oosai once for /domains and /views in
 * parallel. Idempotent — calling twice while a load is in flight
 * is a no-op. After success the resolver is in `ready` state for
 * the rest of the session.
 */
export async function loadResolverIndex(): Promise<void> {
	if (state.kind === "ready") {
		return;
	}

	const app = await loadAppSettings();
	

	try {
		// Both calls go through the bun gateway. Direct fetch from
		// the webview to localhost:4100 fails the same-origin
		// check (origin is views://mainview, oosai serves no CORS
		// headers). bun lives in the same process, has no origin,
		// and is what hosts the agent loop — same mechanism.
		const [domainsRes, viewsRes] = await Promise.all([
			rpc.getDomains({}),
			rpc.getViews({}),
		]);

		if (domainsRes.error) throw new Error(`/domains: ${domainsRes.error}`);
		if (viewsRes.error)   throw new Error(`/views: ${viewsRes.error}`);

		const domainsJson = JSON.parse(domainsRes.json) as { domains?: DomainIndexEntry[] };
		const viewsJson   = JSON.parse(viewsRes.json)   as { views?:   ViewIndexEntry[]   };


		// Index changed → invalidate the alias-vector cache; it will
		// be rebuilt on the next embedding call.
		aliasVectors = null;
		aliasVectorsBuilding = null;

		setState({
			kind:    "ready",
			domains: Array.isArray(domainsJson.domains) ? domainsJson.domains : [],
			views:   Array.isArray(viewsJson.views)     ? viewsJson.views     : [],
		});

	} catch (err) {
		const msg = err instanceof Error ? err.message : String(err);
		console.error("[resolver] load failed:", msg);
		setState({ kind: "error", message: msg });
	}
}

/**
 * resolveText — synchronous keyword path. Stable, cheap, runs on
 * every keystroke. Plurals and synonyms not in the alias list
 * will be missed; for those, use `resolveTextWithEmbedding`.
 *
 * Recognition is alias-based: every domain ships with a list of
 * German phrasings ("alle person", "person Liste", …); a case-
 * insensitive substring match against word boundaries decides.
 * The first matching domain wins.
 */
export function resolveText(text: string): ResolveResult {
	if (state.kind !== "ready") {
		return { views: [] };
	}

	const lower = text.toLowerCase();
	const domain = state.domains.find((d) =>
		d.aliases.some((a) => containsWord(lower, a.toLowerCase())),
	);
	if (!domain) {
		return { views: [] };
	}

	return shapeResult(domain, lower);
}

/**
 * resolveTextWithEmbedding — async embedding path with keyword
 * fallback. The intended workflow:
 *
 *   1. Caller debounces user input (50–100 ms) and calls this.
 *   2. If the embedder is not ready, fall straight back to
 *      `resolveText`.
 *   3. Otherwise, embed the query, score against every cached
 *      alias-vector, take the best (domain, alias, score).
 *   4. score >= HIGH      → return that domain's result.
 *   5. score >= LOW       → ambiguous; defer to keyword path.
 *   6. otherwise / error  → keyword path.
 *
 * The function never throws. Resolver state errors propagate as
 * `{ views: [] }`, embedder failures degrade to keyword.
 */
export async function resolveTextWithEmbedding(text: string): Promise<ResolveResult> {
	if (state.kind !== "ready") return { views: [] };
	if (!isReady())             return resolveText(text);

	try {
		const vectors = await ensureAliasVectors(state.domains);
		const queryVec = await embedText(text);

		// Score every domain by max-cosine over its aliases. Track
		// the global winner; we only need one per resolve.
		let bestDomain: DomainIndexEntry | null = null;
		let bestScore = -Infinity;
		let bestAlias = "";

		for (const domain of state.domains) {
			const dv = vectors.get(domain.name);
			if (!dv || dv.vectors.length === 0) continue;
			const { index, score } = maxCosine(queryVec, dv.vectors);
			if (score > bestScore) {
				bestScore  = score;
				bestDomain = domain;
				bestAlias  = dv.aliases[index] ?? "";
			}
		}

		if (bestDomain && bestScore >= EMBED_THRESHOLD_HIGH) {
			const result = shapeResult(bestDomain, text.toLowerCase());
			result.matchedAlias = bestAlias;
			result.score        = bestScore;
			return result;
		}

		// Ambiguous or low-confidence → keyword path. If keyword
		// also misses, return its empty result — at least it's
		// consistent with the synchronous behaviour.
		return resolveText(text);
	} catch {
		return resolveText(text);
	}
}

/**
 * useResolver returns the current resolver state. Triggers an
 * initial load on first mount; later mounts get the cached state.
 */
export function useResolver(): ResolverState {
	const [snap, setSnap] = useState<ResolverState>(state);

	useEffect(() => {
		const fn: Listener = (s) => setSnap(s);
		listeners.add(fn);
		setSnap(state);
		if (state.kind === "loading") void loadResolverIndex();
		return () => {
			listeners.delete(fn);
		};
	}, []);

	return snap;
}

// ─── Internals ───────────────────────────────────────────────────────

/**
 * shapeResult builds the ResolveResult after the domain has been
 * picked. Same logic for both keyword and embedding paths: views
 * sorted with default first, view-name explicit mention wins over
 * default, default wins over first alphabetical.
 *
 * `lower` is the already lower-cased user text; we still need it
 * here to detect explicit view-name mentions (case-insensitive).
 */
function shapeResult(
	domain: DomainIndexEntry,
	lower: string,
): ResolveResult {
	const views = state.kind === "ready"
		? state.views
			.filter((v) => v.domain === domain.name)
			.sort((a, b) =>
				Number(b.default) - Number(a.default) || a.name.localeCompare(b.name),
			)
		: [];

	// Did the user name a view by id? Match against view names
	// directly. A literal mention of the view id (e.g. "aus
	// person_list_long") should win over the default view.
	const named = views.find((v) => containsWord(lower, v.name.toLowerCase()));
	const suggestion =
		named ??
		views.find((v) => v.default) ??
		pickListyView(views) ??
		views[0];

	return { domain, views, suggestion };
}

/**
 * ensureAliasVectors builds (or returns) the per-domain alias
 * vector cache. Lazy: we only pay the embedding cost the first
 * time someone calls `resolveTextWithEmbedding` after the index
 * is ready. Concurrent callers share the same in-flight build.
 */
async function ensureAliasVectors(
	domains: readonly DomainIndexEntry[],
): Promise<Map<string, DomainVectors>> {
	if (aliasVectors) return aliasVectors;
	if (aliasVectorsBuilding) {
		await aliasVectorsBuilding;
		return aliasVectors ?? new Map();
	}

	aliasVectorsBuilding = (async () => {
		const map = new Map<string, DomainVectors>();
		for (const domain of domains) {
			const aliases = domain.aliases.filter((a) => a.trim().length > 0);
			if (aliases.length === 0) continue;
			const vectors = await Promise.all(aliases.map((a) => embedText(a)));
			map.set(domain.name, { aliases, vectors });
		}
		aliasVectors = map;
	})();

	try {
		await aliasVectorsBuilding;
	} finally {
		aliasVectorsBuilding = null;
	}
	return aliasVectors ?? new Map();
}

/**
 * pickListyView is a fallback heuristic for when no view in a
 * domain is marked `default`. It prefers list-style views over
 * detail / edit views, because "show me all X" is the dominant
 * intent in the chat input. Without this, alphabetical order
 * picks `person_detail` over `person_list`, which is the wrong
 * surface for an undirected query.
 *
 * The DSL author should still mark the canonical view as
 * `default` — this heuristic is a safety net, not a replacement.
 *
 * Returns undefined when no view looks list-y, leaving the
 * caller to fall through to alphabetical order.
 */
function pickListyView(
	views: readonly ViewIndexEntry[],
): ViewIndexEntry | undefined {
	// Positive markers: clearly list-shaped names. The trailing
	// underscore variant catches "_list_long" etc.
	const listMarkers = ["_list", "_index", "_table", "_overview"];
	for (const v of views) {
		const n = v.name.toLowerCase();
		if (listMarkers.some((m) => n === m.slice(1) || n.includes(m))) {
			return v;
		}
	}

	// Negative fallback: anything that does NOT smell like a
	// detail / edit / form / new view. Useful for views named
	// just `person` or `customers`.
	const detailMarkers = ["_detail", "_edit", "_form", "_new"];
	for (const v of views) {
		const n = v.name.toLowerCase();
		if (!detailMarkers.some((m) => n.includes(m))) {
			return v;
		}
	}
	return undefined;
}

/**
 * containsWord checks whether `needle` appears in `haystack` at a
 * word boundary on both sides. Plain `.includes` would match
 * "personalisiert" for the alias "person", which is not what we
 * want; this function rejects partial matches without paying for a
 * full regex compile per call (the alias list is tiny and stable
 * but the user types one character at a time and the resolver may
 * be invoked on every keystroke later).
 */
function containsWord(haystack: string, needle: string): boolean {
	if (!needle) return false;
	let from = 0;
	while (from <= haystack.length - needle.length) {
		const idx = haystack.indexOf(needle, from);
		if (idx < 0) return false;
		const before = idx === 0 ? "" : haystack[idx - 1]!;
		const after  = idx + needle.length === haystack.length
			? ""
			: haystack[idx + needle.length]!;
		if (!isWordChar(before) && !isWordChar(after)) return true;
		from = idx + 1;
	}
	return false;
}

function isWordChar(ch: string): boolean {
	if (!ch) return false;
	return /[a-z0-9_äöüß]/i.test(ch);
}


