// worker.ts — shared singleton accessor for the onisin DSL worker.
//
// Direct port of oosd/src/lang/worker/worker.ts. apps/oos uses the
// same worker bundle to parse .view source pulled from oosgql, so
// the renderer can render it through OnisinView. The diagnostics
// path that oosd's editor needs is unused here, but the worker
// bundle is shared verbatim so the contract stays one thing.
//
// Each client registers an `onmessage` handler via `addReplyHandler`;
// they all see every reply and pick the ones whose `type` they care
// about.

let worker: Worker | null = null;
type ReplyHandler = (event: MessageEvent) => void;
const handlers: ReplyHandler[] = [];

/**
 * sharedWorker returns the singleton worker, creating it on first call.
 *
 * Why `new URL(..., import.meta.url)` + `type: "module"`: this is the
 * worker form Vite detects and bundles itself (dev and build), pulling
 * the Langium deps into a separate worker chunk so they stay out of the
 * main bundle without an esbuild step or a public/ copy. The old
 * Electrobun path (esbuild → IIFE under views/<view>/workers/) does not
 * apply under Vite — nothing is served from there, which is why the
 * classic `new Worker("workers/…")` URL 404'd and the parse promise hung.
 */
export function sharedWorker(): Worker {
	if (worker) return worker;

	worker = new Worker(new URL("./diagnostics-worker.ts", import.meta.url), {
		type: "module",
	});

	worker.onerror = (err) => {
		console.error("[onisin] worker error:", err.message, err);
	};

	worker.onmessage = (event) => {
		for (const h of handlers) h(event);
	};

	return worker;
}

/** Register a reply handler. Returns an unsubscribe function. */
export function addReplyHandler(h: ReplyHandler): () => void {
	handlers.push(h);
	return () => {
		const i = handlers.indexOf(h);
		if (i >= 0) handlers.splice(i, 1);
	};
}

/** Monotonic id allocator for request/reply correlation. */
let nextId = 1;
export function newRequestId(): number {
	return nextId++;
}
