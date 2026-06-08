// hooks/useChat.ts — Drives one chat turn end-to-end from the UI side.
//
// The hook owns the in-memory message scrollback and exposes a `send`
// callback the chat input wires to. Each send:
//
//   1. appends the user message to the scrollback and a placeholder
//      assistant bubble (text filled in when the turn ends).
//   2. opens an RPC chatTurn on the bun backend.
//   3. listens to streamed agentEvents while the turn runs:
//        - tool_call_start / _end → could render a tool card; for
//          the first cut we just log them and let the eventual reply
//          do the talking.
//        - tab_open               → routes into the tabs store as a
//          graphql_result tab.
//        - assistant_message      → fills the placeholder bubble.
//   4. resolves with the final text once chatTurn returns.
//
// Persistence:
//   The hook keeps an `activeChatId` ref. The first user send in a
//   fresh session mints a new id; every subsequent send rewrites
//   the same row. After the turn finishes the row is upserted so
//   the chat-list drawer reflects the latest title and timestamp.
//
//   newChat() clears the scrollback and resets the active id, the
//   way ChatGPT's "+ New chat" works. loadChatById(id) replaces
//   the scrollback with a saved chat and switches the active id
//   so further sends extend that conversation.
//
// The history fed into the next chatTurn excludes tool messages and
// system content; only user/assistant text travels along. This keeps
// the prompt compact across turns even after long agentic runs, at
// the cost of the model not "remembering" tool results across turns
// — an acceptable trade for now.

import { useCallback, useEffect, useRef, useState } from "react";

import type { AgentEvent, ChatMessage as AgentChatMessage } from "../agent/types";
import { rpc, subscribeAgentEvent } from "../rpc";
import {
	createChatId,
	deriveTitle,
	loadChat,
	saveChat,
	type PersistedChat,
} from "../store/chats";
import { loadFinetuning } from "../store/finetuning";
import type { AppSettings } from "../store/settings";
import { addEventResult, addGraphqlResult, addAskResult } from "../store/tabs";
import { loadUiState } from "../store/ui-state";
import { saveTurn, type TurnRecord, type TurnStatus } from "../store/turns";
import type { ChatMessage } from "../types";

/** Public shape of the hook. */
/**
 * EventTurnContext — passed into send() when the chat is in
 * events mode. The ui store carries the streamId already; the
 * mapping flows in from StreamPicker via Chat.tsx because the
 * picker is the only place that resolves it (first-enabled-with-
 * listener-active heuristic).
 */
export interface EventTurnContext {
	mapping:  string;
	streamId: string;
}

export interface UseChatResult {
	messages:       ChatMessage[];
	busy:           boolean;
	activeChatId:   string | null;
	send:           (
		text:     string,
		viewName: string | null,
		eventCtx?: EventTurnContext,
	) => Promise<void>;
	/**
	 * Cancel the in-flight turn, if any. The bun side aborts the
	 * fetch, the loop emits a `cancelled` event, and the placeholder
	 * bubble fills with "Abgebrochen.". Idempotent — calling cancel
	 * with no turn in flight is a no-op.
	 */
	cancel:         () => void;
	newChat:        () => void;
	loadChatById:   (id: string) => Promise<void>;
}

/**
 * useChat wires the bun chatTurn RPC and the agentEvent stream into a
 * React state container the chat column can consume directly.
 */
export function useChat(settings: AppSettings): UseChatResult {
	const [messages, setMessages] = useState<ChatMessage[]>([]);
	const [busy,     setBusy]     = useState(false);
	const [activeChatId, setActiveChatId] = useState<string | null>(null);

	// The current turn's pending assistant bubble id, so streaming
	// events can update the right entry. Held in a ref to avoid
	// stale-closure surprises across awaits.
	const pendingIdRef = useRef<string | null>(null);

	// The current turn's id on the bun side. Set when send dispatches
	// a chatTurn, cleared when it returns. cancel() reads it and fires
	// the cancelTurn RPC; if it's null there's nothing to cancel.
	const activeTurnIdRef = useRef<string | null>(null);

	// Kept in a ref alongside the state value so the post-turn
	// auto-save can read the latest id without depending on a
	// re-render. The state is what the UI consumes.
	const chatIdRef    = useRef<string | null>(null);
	const createdAtRef = useRef<string | null>(null);

	useEffect(() => {
		const unsubscribe = subscribeAgentEvent((event) =>
			onAgentEvent(event, setMessages, pendingIdRef),
		);
		return unsubscribe;
	}, []);

	const send = useCallback(
		async (
			text: string,
			viewName: string | null,
			eventCtx?: EventTurnContext,
			askMode?: boolean,
		) => {
			const trimmed = text.trim();
			if (!trimmed || busy) return;

			// Mint a chat id for the very first user send if no
			// chat is active yet. The same id sticks for every
			// subsequent send in this conversation.
			if (chatIdRef.current === null) {
				const id = createChatId();
				chatIdRef.current    = id;
				createdAtRef.current = new Date().toISOString();
				setActiveChatId(id);
			}

			const userMsg: ChatMessage = {
				id:   `u_${Date.now()}`,
				role: "user",
				text: trimmed,
				ts:   formatNow(),
			};
			const placeholderId = `a_${Date.now()}`;
			const placeholder: ChatMessage = {
				id:   placeholderId,
				role: "assistant",
				text: "…",
				ts:   formatNow(),
			};
			pendingIdRef.current = placeholderId;
			setMessages((prev) => [...prev, userMsg, placeholder]);
			setBusy(true);

			// Hoisted out of the try so the catch block can reference
			// it when persisting a transport-error fallback row.
			const turnId = `turn_${Date.now()}`;
			activeTurnIdRef.current = turnId;

			// Ask-mode short path: a plain tool-less LLM completion.
			// No agent loop, no view hint, no RAG. History flows in
			// so a short back-and-forth works; the answer fills the
			// placeholder bubble directly like a normal chat reply.
			if (askMode) {
				try {
					const history = toAgentHistory(messages);
					const tuning  = await loadFinetuning(settings.llmModel);
					const result  = await rpc.askTurn({
						turnId,
						settings: {
							llmBaseUrl: settings.llmBaseUrl,
							natsUrl:    settings.natsUrl,
							llmApiKey:  settings.llmApiKey,
							llmModel:   settings.llmModel,
						},
						tuning,
						history,
						user: trimmed,
					});
					if (result.error) {
						patchAssistant(setMessages, placeholderId, `Fehler: ${result.error}`);
					} else {
						// Footer's "Editor" switch routes the answer to a tab
						// instead of the chat bubble. The bubble keeps a short
						// pointer so the conversation log still shows that a
						// turn happened; the actual content lives in the tab.
						const ui = await loadUiState();
						if (ui.askToTab) {
							addAskResult({
								question: trimmed,
								answer:   result.text,
								model:    settings.llmModel,
							});
							patchAssistant(setMessages, placeholderId, "_Antwort in neuem Tab geöffnet._");
						} else {
							patchAssistant(setMessages, placeholderId, result.text);
						}
						patchAssistantTelemetry(setMessages, placeholderId, {
							turnId,
							model:      settings.llmModel,
							durationMs: result.trace.durationMs,
							usage:      result.trace.usage,
						});
					}

					// Persist the ask turn so it shows up in the Activity
					// tab like any other. No tool calls, no view hint.
					const finalText = result.error ? `Fehler: ${result.error}` : (result.text || "");
					const turnRecord: TurnRecord = {
						id:           turnId,
						chatId:       chatIdRef.current,
						createdAt:    result.trace.startedAt,
						finishedAt:   result.trace.finishedAt,
						durationMs:   result.trace.durationMs,
						model:        settings.llmModel,
						llmBaseUrl:   settings.llmBaseUrl,
						userText:     trimmed,
						viewHint:     null,
						finalText,
						status:       result.trace.status as TurnStatus,
						steps:        0,
						toolCalls:    result.trace.toolCalls,
						usage:        result.trace.usage,
						errorMessage: result.trace.errorMessage ?? null,
					};
					void saveTurn(turnRecord).catch((err) => {
						console.warn("[chat] saveTurn failed (ask):", err);
					});
				} catch (err) {
					const msg = err instanceof Error ? err.message : String(err);
					patchAssistant(setMessages, placeholderId, `Fehler: ${msg}`);
				} finally {
					pendingIdRef.current    = null;
					activeTurnIdRef.current = null;
					setBusy(false);
				}
				return;
			}

			// Events-mode short path: no agent loop, no view hint.
			// Calls eventTurn on bun, opens an event_result tab,
			// fills the placeholder bubble with a tiny pointer to
			// the tab. We deliberately do NOT plug the events turn
			// into the chat history — board and events are two
			// different conversations sharing the same scrollback.
			if (eventCtx) {
				try {
					const tuning = await loadFinetuning(settings.llmModel);
					const result = await rpc.eventTurn({
						turnId,
						settings: {
							llmBaseUrl: settings.llmBaseUrl,
							natsUrl:    settings.natsUrl,
							llmApiKey:  settings.llmApiKey,
							llmModel:   settings.llmModel,
						},
						tuning,
						mapping:  eventCtx.mapping,
						streamId: eventCtx.streamId,
						question: trimmed,
					});

					if (result.error) {
						patchAssistant(
							setMessages,
							placeholderId,
							`Fehler: ${result.error}`,
						);
					} else {
						addEventResult({
							mapping:  eventCtx.mapping,
							streamId: eventCtx.streamId,
							question: trimmed,
							answer:   result.text,
							hits:     result.hits,
							model:    result.model,
						});
						const tail = result.hits.length === 1
							? "1 Treffer im neuen Tab."
							: `${result.hits.length} Treffer im neuen Tab.`;
						patchAssistant(
							setMessages,
							placeholderId,
							result.text
								? `${result.text}\n\n_${tail}_`
								: `_${tail}_`,
						);
					}
				} catch (err) {
					const msg = err instanceof Error ? err.message : String(err);
					patchAssistant(setMessages, placeholderId, `Fehler: ${msg}`);
				} finally {
					pendingIdRef.current    = null;
					activeTurnIdRef.current = null;
					setBusy(false);
				}
				return;
			}

			try {
				const history = toAgentHistory(messages);

				// Load fine-tuning for the currently selected model.
				// One IndexedDB read per turn — negligible against the
				// LLM round-trip, and avoids a stale cached value when
				// the user just saved new settings in the panel.
				const tuning  = await loadFinetuning(settings.llmModel);

				const result  = await rpc.chatTurn({
					turnId,
					settings: {
						llmBaseUrl: settings.llmBaseUrl,
							natsUrl:    settings.natsUrl,
						llmApiKey:  settings.llmApiKey,
						llmModel:   settings.llmModel,
					},
					tuning,
					history,
					user: trimmed,
					viewHint: viewName ?? undefined,
				});

				// Fall back to the request response when assistant_message
				// did not arrive over the message channel (e.g. transport
				// hiccup) — the chat must never be left on "…".
				if (result.error) {
					patchAssistant(setMessages, placeholderId, `Fehler: ${result.error}`);
				} else if (result.text) {
					patchAssistant(setMessages, placeholderId, result.text);
				}

				// Persist the turn telemetry. Fire-and-forget — a
				// failed save warns to the console but should not
				// surface in the chat. The Activity tab subscribes
				// through the turns store's pub/sub bus and refreshes
				// automatically once the row lands.
				const finalText = result.error
					? `Fehler: ${result.error}`
					: (result.text || "");
				const turnRecord: TurnRecord = {
					id:           turnId,
					chatId:       chatIdRef.current,
					createdAt:    result.trace.startedAt,
					finishedAt:   result.trace.finishedAt,
					durationMs:   result.trace.durationMs,
					model:        settings.llmModel,
					llmBaseUrl:   settings.llmBaseUrl,
					userText:     trimmed,
					viewHint:     viewName ?? null,
					finalText,
					status:       result.trace.status as TurnStatus,
					steps:        result.steps,
					toolCalls:    result.trace.toolCalls,
					usage:        result.trace.usage,
					errorMessage: result.trace.errorMessage ?? null,
				};
				void saveTurn(turnRecord).catch((err) => {
					console.warn("[chat] saveTurn failed:", err);
				});

				// Hang the telemetry footer onto the placeholder
				// bubble so the chat shows model · tokens ·
				// duration under the assistant reply. Click on
				// the footer opens the corresponding Activity-
				// detail tab.
				patchAssistantTelemetry(setMessages, placeholderId, {
					turnId,
					model:      settings.llmModel,
					durationMs: result.trace.durationMs,
					usage:      result.trace.usage,
				});
			} catch (err) {
				const msg = err instanceof Error ? err.message : String(err);
				patchAssistant(setMessages, placeholderId, `Fehler: ${msg}`);
				// Best-effort persistence for transport-level failures
				// (rpc threw, no result object). The Activity tab
				// shows these as zero-duration "error" rows — better
				// than silence when something goes wrong on the wire.
				const now = new Date().toISOString();
				const turnRecord: TurnRecord = {
					id:           turnId,
					chatId:       chatIdRef.current,
					createdAt:    now,
					finishedAt:   now,
					durationMs:   0,
					model:        settings.llmModel,
					llmBaseUrl:   settings.llmBaseUrl,
					userText:     trimmed,
					viewHint:     viewName ?? null,
					finalText:    `Fehler: ${msg}`,
					status:       "error",
					steps:        0,
					toolCalls:    [],
					usage:        { promptTokens: 0, completionTokens: 0, totalTokens: 0 },
					errorMessage: msg,
				};
				void saveTurn(turnRecord).catch((saveErr) => {
					console.warn("[chat] saveTurn (transport-error path) failed:", saveErr);
				});
			} finally {
				pendingIdRef.current     = null;
				activeTurnIdRef.current  = null;
				setBusy(false);
			}
		},
		[busy, messages, settings],
	);

	// Auto-save after every turn settles. We watch `busy` falling
	// back to false because that's the single point where the turn
	// is fully done — the placeholder either has the real text or
	// an error message, both of which are worth persisting.
	//
	// Using a ref-cycle effect (busy true → false) rather than a
	// post-await call inside `send` keeps the persistence logic
	// out of the request happy path; if the save fails the user
	// still gets their answer.
	const messagesRef = useRef(messages);
	messagesRef.current = messages;

	useEffect(() => {
		if (busy) return;
		const id = chatIdRef.current;
		if (!id) return;
		const current = messagesRef.current;
		if (current.length === 0) return;

		const now = new Date().toISOString();
		const chat: PersistedChat = {
			id,
			title:     deriveTitle(current),
			createdAt: createdAtRef.current ?? now,
			updatedAt: now,
			messages:  current,
		};
		void saveChat(chat).catch((err) => {
			console.warn("[chat] save failed:", err);
		});
	}, [busy]);

	const cancel = useCallback(() => {
		const turnId = activeTurnIdRef.current;
		if (!turnId) return;
		// Fire-and-forget: the bun side aborts the controller,
		// the loop emits a `cancelled` event, useChat's send
		// callback unwinds normally and clears the busy flag.
		// We don't await — the user already moved on the moment
		// they pressed Stop.
		void rpc.cancelTurn({ turnId }).catch((err) => {
			console.warn("[chat] cancelTurn failed:", err);
		});
	}, []);

	const newChat = useCallback(() => {
		// Resetting the refs first means a quick second send while
		// the user is mid-typing won't accidentally overwrite the
		// previous chat — the chat-id mint will run again.
		chatIdRef.current    = null;
		createdAtRef.current = null;
		setActiveChatId(null);
		setMessages([]);
	}, []);

	const loadChatById = useCallback(async (id: string) => {
		const chat = await loadChat(id);
		if (!chat) return;
		chatIdRef.current    = chat.id;
		createdAtRef.current = chat.createdAt;
		setActiveChatId(chat.id);
		setMessages(chat.messages);
	}, []);

	return { messages, busy, activeChatId, send, cancel, newChat, loadChatById };
}

// ─── Event handling ──────────────────────────────────────────────────

function onAgentEvent(
	event:        AgentEvent,
	setMessages:  React.Dispatch<React.SetStateAction<ChatMessage[]>>,
	pendingIdRef: React.MutableRefObject<string | null>,
): void {
	switch (event.type) {
		case "tab_open":
			// Adapter: oos.cmd.data.query returns a flat `rows` array, but the
			// result tab + ResultTable still speak the `{ <domain>: [...] }`
			// envelope, so wrap rows under the context name. `query` is empty —
			// there is no GraphQL string in the data.query world.
			addGraphqlResult({
				contextName: event.contextName,
				query:       "",
				data:        { [event.contextName]: event.rows },
				viewName:    event.viewName,
			});
			break;
		case "assistant_message": {
			const id = pendingIdRef.current;
			if (id) patchAssistant(setMessages, id, event.text || "(leere Antwort)");
			break;
		}
		case "agent_error": {
			const id = pendingIdRef.current;
			if (id) patchAssistant(setMessages, id, `Fehler: ${event.message}`);
			break;
		}
		case "tool_call_start":
		case "tool_call_end":
			// Cards in the bubble come later. Today we just rely on
			// assistant_message + tab_open for visible feedback.
			break;
		case "cancelled":
			// The agent loop also fires an assistant_message
			// carrying "Abgebrochen." right after this event,
			// which fills the placeholder bubble. Nothing more
			// to do here today; future UI iterations could
			// render a distinct "cancelled" chip.
			break;
	}
}

function patchAssistant(
	setMessages: React.Dispatch<React.SetStateAction<ChatMessage[]>>,
	id:          string,
	text:        string,
): void {
	setMessages((prev) =>
		prev.map((m) => (m.id === id ? { ...m, text, ts: formatNow() } : m)),
	);
}

/**
 * patchAssistantTelemetry attaches the per-turn telemetry footer
 * (model, tokens, duration, turnId) to a finished assistant
 * bubble. Called once the chatTurn RPC returns, alongside the
 * final-text patch. Splitting it from patchAssistant keeps the
 * streaming-text path free of telemetry-shape knowledge — the
 * loop fires assistant_message before chatTurn returns, so the
 * text is usually already in place by the time we land here.
 */
function patchAssistantTelemetry(
	setMessages: React.Dispatch<React.SetStateAction<ChatMessage[]>>,
	id:          string,
	telemetry:   {
		turnId:     string;
		model:      string;
		durationMs: number;
		usage: {
			promptTokens:     number;
			completionTokens: number;
			totalTokens:      number;
		};
	},
): void {
	setMessages((prev) =>
		prev.map((m) => (m.id === id ? { ...m, ...telemetry } : m)),
	);
}

// ─── Helpers ─────────────────────────────────────────────────────────

/**
 * toAgentHistory strips UI-only messages (placeholders, error
 * variants) down to the user/assistant text pairs the agent loop
 * expects.
 */
function toAgentHistory(uiMessages: ChatMessage[]): AgentChatMessage[] {
	const out: AgentChatMessage[] = [];
	for (const m of uiMessages) {
		if (m.role === "user") {
			out.push({ role: "user", content: m.text });
		} else if (m.role === "assistant" && m.text && m.text !== "…") {
			out.push({ role: "assistant", content: m.text });
		}
	}
	return out;
}

/** formatNow returns the current local time as "HH:MM". */
function formatNow(): string {
	const d  = new Date();
	const hh = String(d.getHours()).padStart(2, "0");
	const mm = String(d.getMinutes()).padStart(2, "0");
	return `${hh}:${mm}`;
}
