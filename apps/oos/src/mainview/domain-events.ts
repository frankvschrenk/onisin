// domain-events.ts — Tiny in-process pub/sub bus for "domain X
// changed" notifications between view tabs.
//
// Why this exists: a detail tab that just successfully saved or
// deleted a record needs to tell every list-view tab on the same
// domain to refresh. The two are siblings in the React tree (both
// children of the tab manager), so React state cannot link them
// directly without dragging every tab through one shared store
// — a heavier hammer than the use case warrants.
//
// This module is the lightweight alternative: a Map<channel,
// Set<listener>> keyed by `<domain>:<event>`. Detail tabs publish;
// list tabs subscribe. No global state, no React magic, nothing
// the caller has to remember to clean up beyond the unsubscribe
// function the subscription returns.
//
// The channel naming is `<domain>:<event>` rather than two
// separate args so callers can build the key once and stash it.
// Today the only event in active use is "changed"; the shape is
// kept open for future names ("created", "deleted", workflow
// signals) without an interface change.

/** Listener callback shape. Payload is reserved for future use. */
export type DomainEventListener = (payload?: unknown) => void;

/**
 * Subscribe to a domain event. Returns an unsubscribe function.
 *
 *   const off = subscribe("person", "changed", () => reload());
 *   ...
 *   off();
 *
 * Multiple subscribers on the same channel are supported — every
 * one fires, in registration order, on each publish.
 */
export function subscribe(
	domain:   string,
	event:    string,
	listener: DomainEventListener,
): () => void {
	const key = channelKey(domain, event);
	let set = channels.get(key);
	if (!set) {
		set = new Set();
		channels.set(key, set);
	}
	set.add(listener);
	return () => {
		const s = channels.get(key);
		if (!s) return;
		s.delete(listener);
		if (s.size === 0) channels.delete(key);
	};
}

/**
 * Publish a domain event. Every subscriber on the matching channel
 * fires synchronously, in registration order. Listener errors are
 * logged but do not abort the rest of the dispatch.
 */
export function publish(
	domain:   string,
	event:    string,
	payload?: unknown,
): void {
	const set = channels.get(channelKey(domain, event));
	if (!set) return;
	for (const fn of set) {
		try {
			fn(payload);
		} catch (err) {
			// One bad listener should not silence the rest. Log and
			// keep going.
			console.error(
				`[domain-events] listener for ${domain}.${event} threw:`,
				err,
			);
		}
	}
}

// ─── Internals ───────────────────────────────────────────────────────

const channels = new Map<string, Set<DomainEventListener>>();

function channelKey(domain: string, event: string): string {
	return `${domain}:${event}`;
}
