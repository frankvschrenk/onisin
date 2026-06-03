// worker.ts — shared singleton accessor for the onisin DSL worker.
//
// Both diagnostics and preview share one worker instance: starting
// Langium is non-trivial and we don't want to pay that cost twice.
// The worker dispatches by `type` field on the request, so multiple
// clients can multiplex through a single message channel.
//
// Each client registers an `onmessage` handler via `addReplyHandler`;
// they all see every reply and pick the ones whose `type` they care
// about.

let worker: Worker | null = null;
type ReplyHandler = (event: MessageEvent) => void;
const handlers: ReplyHandler[] = [];

/**
 * sharedWorker returns the singleton worker, creating it on first
 * call. The bundled worker is copied into views/mainview/workers/ at
 * build time (see scripts/build-workers.ts) and served from the same
 * origin as the page itself.
 */
export function sharedWorker(): Worker {
	if (worker) return worker;

	// Classic worker — the build script bundles to IIFE so no ESM
	// imports are left for the worker to resolve at runtime.
	worker = new Worker("workers/diagnostics-worker.js");

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
