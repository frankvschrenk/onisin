// chat-events.ts — Pub/sub for chat-management actions emitted by
// the in-tab Chat-History panel.
//
// Why this exists: the Chat-History panel renders inside a tab,
// disconnected from the React tree where the active chat actually
// lives (App.tsx → useChat). Click-to-load and delete need to reach
// that hook without prop-drilling through the tab system. This bus
// is the lightweight bridge — same pattern as domain-events.ts but
// for chat-lifecycle signals.
//
// Two channels:
//
//   - "load" — the user clicked a chat row and wants its history
//              swapped into the live conversation. Payload is the
//              chat id.
//   - "delete" — the user clicked the trash icon. Payload is the
//                chat id. The store handles the deletion; this bus
//                just carries the request to the place that knows
//                how to fall back to a fresh chat if the deleted
//                one was active.
//
// Drag-Drop into the composer does NOT go through this bus: the
// drop target sits directly on the Textarea and pulls plain text
// out of the DataTransfer object. The two flows are intentionally
// orthogonal — drag fills the input, click loads the whole chat.

export type ChatEvent =
	| { kind: "load";   chatId: string }
	| { kind: "delete"; chatId: string };

type Listener = (event: ChatEvent) => void;

const listeners = new Set<Listener>();

/**
 * subscribe registers a listener and returns an unsubscribe handle.
 * Components call this in a useEffect and clean up on unmount.
 */
export function subscribe(listener: Listener): () => void {
	listeners.add(listener);
	return () => {
		listeners.delete(listener);
	};
}

/** publish fans the event out to every active subscriber. */
export function publish(event: ChatEvent): void {
	for (const fn of listeners) {
		try {
			fn(event);
		} catch (err) {
			console.error("[chat-events] listener threw:", err);
		}
	}
}
