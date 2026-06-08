// types.ts — Shared types for the mainview shell.
//
// Tab and tab-group types have moved into store/tabs.ts so the
// store is the single source of truth. Anything outside of tabs
// lives here.

/** One message in the chat scrollback. */
export interface ChatMessage {
	id:   string;
	role: "user" | "assistant" | "tool";
	text: string;
	ts:   string;
	/**
	 * Optional per-turn telemetry, attached to assistant messages
	 * once the turn finishes. Rendered as a small footer below
	 * the bubble (model · tokens · duration). Absent on user
	 * messages, on the placeholder bubble while a turn is running,
	 * and on persisted-but-old chats from before the telemetry
	 * pipeline existed — the renderer simply skips the footer.
	 *
	 * Mirrors the relevant fields from store/turns.ts/TurnRecord
	 * so the bubble does not have to query the turns table for
	 * every render. Carrying a copy in the chat state keeps the
	 * UI fast and tolerates the occasional turn delete in the
	 * Activity tab without blanking the chat footer.
	 *
	 * `turnId` lets the bubble link straight to the corresponding
	 * Activity-detail tab when the user clicks the footer.
	 */
	turnId?:     string;
	model?:      string;
	durationMs?: number;
	usage?: {
		promptTokens:     number;
		completionTokens: number;
		totalTokens:      number;
	};
}
