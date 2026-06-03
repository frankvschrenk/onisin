// mainview/embedder/embedder.ts — Local sentence embedder.
//
// Singleton wrapper around `@huggingface/transformers` that loads
// a small multilingual sentence-transformer once per renderer
// process and exposes a tiny synchronous-feeling API to the rest
// of the app.
//
// Why this lives in its own module:
//
//   - The transformers.js pipeline is heavy to construct (file
//     download on first run, ONNX session bring-up). We only ever
//     want one instance, kept warm for the rest of the session.
//   - The resolver should not care which model is in use, only
//     that there is "a function that turns text into a vector".
//     This module hides the model name, the prefixing rules, and
//     the warm-up dance behind a stable surface.
//   - Phase-1 keyword resolver still has to work when the embedder
//     is loading or has failed. `isReady()` lets the caller fall
//     back gracefully without awaiting.
//
// Model choice: paraphrase-multilingual-MiniLM-L12-v2 (~118 MB).
// This was picked over E5 variants and Model2Vec/Potion after a
// smoke test (see scripts/embed-smoketest.ts):
//
//   - E5 collapses cosines into a tight cluster around 0.85 for
//     short queries, regardless of whether they belong to the
//     domain or not. Even with role prefixes the separation
//     against negatives is essentially zero.
//   - Model2Vec/Potion is the obvious "schmal" candidate but does
//     NOT run in transformers.js: the ONNX graph expects an
//     EmbeddingBag-style `offsets` input that the generic
//     EncoderOnly pipeline never sets. There is no JS port of
//     model2vec on npm either, so going down that path would mean
//     standing up a Python sidecar — exactly the extra
//     infrastructure we want to avoid.
//   - MiniLM has the widest dynamic range on our test set
//     (negatives ~0.30, positives ~0.80) and reaches 100 %
//     accuracy on the toy labelled set when scored by max-cosine
//     across an alias bundle. It does not need role prefixes,
//     which keeps the embedder API symmetric.
//
// First-run cost: one ~118 MB download from the Hugging Face CDN
// into ~/.cache/huggingface/. Subsequent starts hit the cache and
// load in ~350 ms. Air-gapped bundling of the model files into the
// Electrobun build is a separate, later concern — the file layout
// is already designed to accept a `local_files_only` switch
// without touching callers.

import { pipeline } from "@huggingface/transformers";

// ─── Types ───────────────────────────────────────────────────────────

/** Raw output of a transformers.js feature-extraction pipeline.
 *
 * Declared narrowly here so the rest of the app does not need to
 * import the loose types from `@huggingface/transformers`.
 */
type Embedder = (
	text: string,
	opts: { pooling: "mean"; normalize: boolean },
) => Promise<{ data: Float32Array }>;

/** Lifecycle of the embedder singleton. */
export type EmbedderState =
	| { kind: "idle" }
	| { kind: "loading"; startedAt: number }
	| { kind: "ready";   model: string; loadMs: number }
	| { kind: "error";   message: string };

// ─── Module-level state ──────────────────────────────────────────────

/** Identifier of the model loaded into the renderer. Hard-coded
 * for now; can be promoted to settings.ts when more than one
 * model becomes interesting. */
const MODEL_NAME = "Xenova/paraphrase-multilingual-MiniLM-L12-v2";

let state: EmbedderState = { kind: "idle" };
let pipelinePromise: Promise<Embedder> | null = null;

type Listener = (s: EmbedderState) => void;
const listeners = new Set<Listener>();

function setState(next: EmbedderState): void {
	state = next;
	for (const fn of listeners) fn(state);
}

// ─── Public API ──────────────────────────────────────────────────────

/**
 * loadEmbedder kicks off model loading. Idempotent: a second call
 * while a load is in flight returns the same promise; a call after
 * a successful load is a cheap no-op. Errors are recorded in the
 * state but do NOT throw — the caller is expected to check
 * `isReady()` and fall back to the keyword path on failure.
 */
export function loadEmbedder(): Promise<void> {
	if (state.kind === "ready") return Promise.resolve();
	if (pipelinePromise) return pipelinePromise.then(() => undefined);

	const startedAt = performance.now();
	setState({ kind: "loading", startedAt });

	pipelinePromise = (async () => {
		const fn = (await pipeline(
			"feature-extraction",
			MODEL_NAME,
		)) as unknown as Embedder;
		const loadMs = performance.now() - startedAt;
		setState({ kind: "ready", model: MODEL_NAME, loadMs });
		return fn;
	})();

	return pipelinePromise.then(
		() => undefined,
		(err: unknown) => {
			const message = err instanceof Error ? err.message : String(err);
			setState({ kind: "error", message });
			pipelinePromise = null;
		},
	);
}

/**
 * embedText returns a unit-length sentence vector for `text`.
 * Awaits the pipeline lazily, so it is safe to call before
 * `loadEmbedder` has resolved — the promise simply chains onto the
 * in-flight load. Throws if the model has failed to load; callers
 * are expected to gate this with `isReady()` on hot paths.
 */
export async function embedText(text: string): Promise<Float32Array> {
	if (!pipelinePromise) {
		// Lazy ignition: someone called embedText before loadEmbedder.
		// Start the load now and chain onto it.
		void loadEmbedder();
	}
	if (!pipelinePromise) {
		throw new Error("embedder: load failed and is not retrying");
	}
	const fn = await pipelinePromise;
	const out = await fn(text, { pooling: "mean", normalize: true });
	return out.data;
}

/** isReady reports whether `embedText` will resolve quickly.
 * Useful as a feature flag in the resolver — when false, fall back
 * to keyword matching without awaiting the model. */
export function isReady(): boolean {
	return state.kind === "ready";
}

/** Snapshot the current state. Mostly for diagnostics / status
 * indicators. */
export function getEmbedderState(): EmbedderState {
	return state;
}

/** Subscribe to state transitions. Returns an unsubscribe fn. */
export function subscribe(fn: Listener): () => void {
	listeners.add(fn);
	fn(state);
	return () => {
		listeners.delete(fn);
	};
}

// ─── Vector helpers ──────────────────────────────────────────────────

/**
 * cosine returns the cosine similarity of two L2-normalised
 * vectors. Because `embedText` always returns normalised vectors,
 * this is just a dot product — exposed as its own function so the
 * resolver does not have to repeat the loop in three places.
 */
export function cosine(a: Float32Array, b: Float32Array): number {
	if (a.length !== b.length) {
		throw new Error(
			`cosine: vector length mismatch (${a.length} vs ${b.length})`,
		);
	}
	let dot = 0;
	for (let i = 0; i < a.length; i++) dot += a[i] * b[i];
	return dot;
}

/**
 * maxCosine scores `query` against a set of candidate vectors and
 * returns the index of the closest one along with its score. When
 * `candidates` is empty, returns `{ index: -1, score: -Infinity }`.
 *
 * This is the right aggregation for alias-bundle matching in the
 * resolver: a query belongs to a domain when it is close to ANY
 * single alias of that domain, not when it is close to the
 * centroid of all aliases. The smoke test showed mean-bundling
 * smooths the signal away (positives and negatives both land at
 * ~0.85 cosine), while max-cosine preserves the dynamic range.
 */
export function maxCosine(
	query: Float32Array,
	candidates: readonly Float32Array[],
): { index: number; score: number } {
	let bestIdx = -1;
	let bestScore = -Infinity;
	for (let i = 0; i < candidates.length; i++) {
		const s = cosine(query, candidates[i]!);
		if (s > bestScore) {
			bestScore = s;
			bestIdx = i;
		}
	}
	return { index: bestIdx, score: bestScore };
}
