// Chat.tsx — Left column: scrollable conversation + input bar.
//
// Pure presentation: messages come in via props, the input field
// has local state, Send delegates to the parent through `onSend`.
//
// View-hint pipeline:
//   - As the user types, two resolvers run in sequence: a fast
//     synchronous keyword match for instant feedback, and a
//     debounced embedding match (MiniLM) that catches plurals and
//     synonyms the keyword path misses. If a domain is
//     recognised, the suggested view (default unless the user
//     named one) becomes the auto-pinned hint and a Pill appears
//     below the input.
//   - The user can press Ctrl+Plus (Cmd+Plus on macOS) to open a
//     picker showing every view bound to that domain, and pick a
//     different one. Choice replaces the auto-pinned hint and
//     freezes the pill so further keystrokes do not override it.
//   - The X on the pill clears the hint entirely; subsequent
//     keystrokes resume auto-pinning.
//   - Send forwards `(text, viewName | null)` to the parent so the
//     agent can plumb the hint through to bun.
//
// Layout stability:
//   The pill row is rendered with a fixed height regardless of
//   whether a pill is showing. Without this the row collapses to
//   0px when the resolver yields no match, then expands when a
//   later keystroke matches again, and the input bar above
//   visibly jumps. Reserving the row height once means the rest
//   of the chat never moves.
//
// Pill stickiness:
//   Once a domain has been auto-pinned, intermediate keystrokes
//   that yield no match do NOT clear it — typing one extra
//   character past "alle Personen" should not blink the pill.
//   Three things still clear an auto-pinned pill: emptying the
//   draft, pressing the X, or the resolver matching a different
//   domain (which replaces in place, no flicker).

import {
	useEffect,
	useRef,
	useState,
	type DragEvent,
	type KeyboardEvent,
} from "react";
import {
	ActionIcon,
	Box,
	Menu,
	Paper,
	ScrollArea,
	Stack,
	Text,
} from "@mantine/core";
import { IconEraser, IconPlayerStopFilled, IconSend } from "@tabler/icons-react";

import type { EventTurnContext } from "../hooks/useChat";
import {
	resolveText,
	resolveTextWithEmbedding,
	type ViewIndexEntry,
} from "../store/resolver";

import { useUiState } from "../store/ui-state";
import type { ChatMessage } from "../types";
import { Bubble } from "./Bubble";
import { consumePendingDragText } from "./ChatHistoryPanel";
import { ChatViewPicker } from "./ChatViewPicker";
import { MappingPicker } from "./MappingPicker";
import { StreamPicker } from "./StreamPicker";
import { ViewPill } from "./ViewPill";
import { ComposerRichText, type ComposerRichTextHandle } from "./ComposerRichText";
import { Split } from "@gfazioli/mantine-split-pane";
import "@gfazioli/mantine-split-pane/styles.css";

interface ChatProps {
	messages: ChatMessage[];
	/** True while the agent loop is running on the bun side. */
	busy?:    boolean;
	/**
	 * Called on Send. `eventCtx` is supplied only when the chat is
	 * in events mode and a stream is picked; otherwise undefined
	 * and the call routes through the regular agent loop. `askMode`
	 * is true when the chat is in ask mode — a plain tool-less LLM
	 * completion. eventCtx and askMode are mutually exclusive.
	 */
	onSend?:  (
		text:      string,
		viewName:  string | null,
		eventCtx?: EventTurnContext,
		askMode?:  boolean,
	) => void;
	/** Called when the user clicks Stop while a turn is running. */
	onCancel?: () => void;
	/** Called when the user clears the conversation history. */
	onClear?:  () => void;
}

/**
 * Reserved vertical space for the pill row, in pixels. Sized for a
 * single Mantine Badge in `lg` height plus its CloseButton, with a
 * couple of pixels of breathing room. Adjust here if ViewPill grows.
 */
const PILL_ROW_HEIGHT = 32;

export function Chat({ messages, busy = false, onSend, onCancel, onClear }: ChatProps) {
	const [draft, setDraft] = useState("");
	// Imperative handle for the rich composer. The host owns the
	// `draft` string for the resolver effect and the disabled/empty
	// checks, but a few flows (drag-drop replace, clear-on-send) need
	// to push a value into the editor's internal state explicitly.
	const composerRef = useRef<ComposerRichTextHandle>(null);

	// Controls the right-click context menu on the Send button.
	const [sendMenuOpen, setSendMenuOpen] = useState(false);

	// UI mode (forms / events / documents) plus the active mapping and stream.
	// All three live in the same Dexie blob via the ui store, so a
	// switch by ModeSwitch / MappingPicker / StreamPicker is
	// reflected here in the same render tick.
	const { state: uiState } = useUiState();

	// Composer is locked while in events mode without both a
	// mapping and a stream picked — without a stream filter the
	// search would dump the whole mapping into the LLM context.
	const eventsMode    = uiState.mode === "events";
	const documentsMode = uiState.mode === "documents";
	// Dev mode shares ask-mode behaviour: no domain resolver, no view pill.
	const askMode       = uiState.mode === "ask" || uiState.mode === "dev";
	const hasMapping    = !!uiState.mapping;
	const hasStream     = !!uiState.streamId;
	// Events mode requires both mapping and stream before sending.
	// Documents mode locks the composer entirely — interaction
	// happens through the Pipeline browser, not the chat input.
	const composerLocked = (eventsMode && (!hasMapping || !hasStream)) || documentsMode;

	// Two layers of "active view":
	//   - `manualView` is set when the user picked something from
	//     the picker, or null when they cleared the pill. It wins
	//     over auto-pinning until the user clears the draft.
	//   - `autoView` mirrors the resolver's suggestion as the user
	//     types. Only used when manualView is undefined (= no
	//     manual choice yet).
	// Sentinel `null` for manualView means "explicitly cleared",
	// distinct from `undefined` ("never touched").
	const [manualView, setManualView] = useState<ViewIndexEntry | null | undefined>(undefined);
	const [autoView,   setAutoView]   = useState<ViewIndexEntry | null>(null);
	const [pickerOpen, setPickerOpen] = useState(false);
	const [resolvedViews, setResolvedViews] = useState<ViewIndexEntry[]>([]);

	// Monotonic stamp to discard stale embedding results — see
	// the per-keystroke resolve effect below.
	const resolveStampRef = useRef(0);

	// Tracks the in-flight embedding resolve fired by onDrop.
	// send() awaits this so a Drop+Enter does not race past the
	// embedder when the keyword path missed (e.g. plural form).
	// The promise resolves to the matched view name (or null) so
	// send() can use it directly without waiting for React state
	// to catch up.
	const dropResolveRef = useRef<Promise<string | null> | null>(null);

	// Two-stage resolve, both fired on every keystroke:
	//
	//   1. Synchronous keyword match — instant, used for the very
	//      first paint after the user types a character.
	//   2. Async embedding match — debounced 60 ms, gated by the
	//      embedder being ready. Catches plurals and synonyms the
	//      keyword path misses ("Personen", "Mitarbeiter"). Drops
	//      its result if the draft has changed since dispatch.
	//
	// Sticky behaviour: a "no match" result from either path does
	// NOT clear an existing autoView. A non-empty draft that
	// previously matched should keep its pill while the user
	// keeps typing — only an empty draft, an explicit X, or a
	// match on a *different* domain replaces what's there.
	useEffect(() => {
		// Ask mode has no domain context, so the view-hint resolver
		// must stay silent: no keyword match, no embedding round-trip,
		// no pill. Clear any pill left over from a previous mode and
		// bail before the resolver runs. askMode is in the dependency
		// list so switching into Ask removes a stale pill immediately.
		if (askMode) {
			setAutoView(null);
			setResolvedViews([]);
			return;
		}

		// Empty draft is the one place the auto-pin is reset:
		// the user has effectively started over.
		if (draft.trim() === "") {
			setAutoView(null);
			setResolvedViews([]);
			return;
		}

		const sync = resolveText(draft);
		if (sync.suggestion) {
			setAutoView(sync.suggestion);
			setResolvedViews(sync.views);
		}
		// else: keep the previous autoView / resolvedViews untouched.
		// The pill stays pinned to whatever the last successful
		// match was.

		// Stamp this dispatch so a stale embedding result cannot
		// clobber a newer keystroke's sync result.
		const stamp = ++resolveStampRef.current;

		const handle = setTimeout(() => {
			void (async () => {
				const async_ = await resolveTextWithEmbedding(draft);
				if (stamp !== resolveStampRef.current) return;
				// Only commit if it adds information — found a
				// domain the sync path missed, or picked a
				// different one. A null embedding result also
				// does not clear the pill.
				const syncDomain  = sync.domain?.name;
				const asyncDomain = async_.domain?.name;
				if (asyncDomain && asyncDomain !== syncDomain) {
					if (async_.suggestion) {
						setAutoView(async_.suggestion);
						setResolvedViews(async_.views);
					}
				}
			})();
		}, 60);

		return () => clearTimeout(handle);
	}, [draft, askMode]);

	// Resolver index and embedder are ignited from App.tsx so all
	// consumers share the same warm-up cycle.

	// Effective view = manual choice when set, else the auto-pinned one.
	const effectiveView: ViewIndexEntry | null =
		manualView === undefined ? autoView : manualView;

	// Accept drops from the Chat-History tab. Two payload sources:
	//   - the upgraded text in pendingDragText, set asynchronously
	//     by ChatHistoryPanel after loadChat resolves;
	//   - the synchronous fallback set on dataTransfer at dragstart
	//     (the chat title), used when the user drops before the
	//     async fetch lands.
	// Either way the dropped text *replaces* the draft, matching
	// the agreed UX: pick a chat, drag it across, edit before send.
	const onDragOver = (e: DragEvent<HTMLDivElement>) => {
		// Calling preventDefault is what marks the element as a
		// valid drop target. Without it the browser refuses the
		// drop and shows the "no entry" cursor.
		if (e.dataTransfer.types.includes("text/plain")) {
			e.preventDefault();
			e.dataTransfer.dropEffect = "copy";
		}
	};
	const onDrop = (e: DragEvent<HTMLDivElement>) => {
		const upgraded = consumePendingDragText();
		const fallback = e.dataTransfer.getData("text/plain");
		const text = upgraded ?? fallback;
		if (!text) return;
		// preventDefault stops the inner <textarea> from running
		// its native drop handler (which would insert the text at
		// the caret position instead of replacing the draft).
		e.preventDefault();
		setDraft(text);
		composerRef.current?.setMarkdown(text);

		// Synchronous keyword resolve first — for domains whose
		// aliases match a user's exact phrasing, the pill appears
		// instantly. The plural-form gap (alias "person" vs user
		// "Personen") is closed by the embedding step below.
		const sync = resolveText(text);
		if (sync.suggestion) {
			setAutoView(sync.suggestion);
			setResolvedViews(sync.views);
		}

		// Async embedding resolve — catches plurals and synonyms
		// the keyword path misses ("Personen" vs alias "person",
		// "Mitarbeiter" vs "person"). The promise resolves to the
		// matched view name (or null) so send() can use it
		// directly without depending on React state catching up.
		const embedPromise: Promise<string | null> = (async () => {
			try {
				const result = await resolveTextWithEmbedding(text);
				if (result.suggestion) {
					setAutoView(result.suggestion);
					setResolvedViews(result.views);
					return result.suggestion.name;
				}
				return sync.suggestion?.name ?? null;
			} catch (err) {
				console.warn("[chat] onDrop embedding resolve failed:", err);
				return sync.suggestion?.name ?? null;
			}
		})();
		dropResolveRef.current = embedPromise;
		// Self-clear the ref once the promise settles, by reference
		// comparison so a newer drop's promise is not clobbered.
		void embedPromise.finally(() => {
			if (dropResolveRef.current === embedPromise) {
				dropResolveRef.current = null;
			}
		});
	};

	// View-picker toggle handler for the rich composer. Cmd/Ctrl + "+"
	// only opens the picker if at least one resolved view exists; with
	// no candidates the shortcut falls through silently (the browser's
	// zoom default still triggers, which is fine).
	const togglePicker = () => {
		if (resolvedViews.length > 0) {
			setPickerOpen((open) => !open);
		}
	};

	const send = async () => {
		const trimmed = draft.trim();
		if (!trimmed) return;

		// If a Drop just fired its embedding resolve, await it and
		// take its return value as the authoritative viewHint.
		// React state from setAutoView inside the promise might not
		// have flushed yet at this point (continuation timing),
		// but the return value is right out of resolveTextWithEmbedding
		// so it is always current. effectiveView is checked only
		// when no drop is pending.
		let viewHint: string | null = null;
		if (dropResolveRef.current) {
			viewHint = await dropResolveRef.current;
		} else {
			viewHint = effectiveView?.name ?? null;
		}

		// Last-chance synchronous keyword resolve. Belt-and-braces
		// for any path that did not run the embedding upgrade —
		// typed-then-Enter-fast, programmatic setDraft, etc.
		// Skipped when the user explicitly cleared the pill
		// (manualView === null).
		if (!viewHint && manualView !== null) {
			const sync = resolveText(trimmed);
			if (sync.suggestion) viewHint = sync.suggestion.name;
		}

		// Events mode: skip the view-hint resolver and ship the
		// mapping + stream straight from the ui store. The composer
		// is gated upstream on (hasMapping && hasStream) so both
		// are present here — but we still guard, in case of a race
		// where the user toggled mode between Enter and dispatch.
		if (askMode) {
			// Ask mode: no view hint, no event context — just the
			// question, flagged so useChat routes to the tool-less path.
			onSend?.(trimmed, null, undefined, true);
		} else if (eventsMode && uiState.mapping && uiState.streamId) {
			onSend?.(trimmed, null, {
				mapping:  uiState.mapping,
				streamId: uiState.streamId,
			});
		} else {
			onSend?.(trimmed, viewHint);
		}
		setDraft("");
		composerRef.current?.setMarkdown("");
		setManualView(undefined);
		setAutoView(null);
		setResolvedViews([]);
		setPickerOpen(false);
	};

	const clearHint = () => {
		// User explicitly removed the pill → freeze in "no hint"
		// state until the draft is sent or fully cleared.
		setManualView(null);
		setPickerOpen(false);
	};

	return (
		<Split
			orientation="horizontal"
			autoResizers
			style={{
				borderRight: "1px solid var(--mantine-color-gray-3)",
				minHeight: 0,
				height:    "100%",
			}}
		>
			{/* Top pane: scrolling message list. Defaults to 65 % of the
			    column; the user drags the resizer below to give the
			    composer more or less room as needed. */}
			<Split.Pane initialHeight="65%" minHeight="20%">
			<ScrollArea
				style={{ height: "100%" }}
				type="hover"
				offsetScrollbars
				scrollbarSize={6}
			>
				<Stack gap="md" p="md">
					{messages.length === 0 && (
						<Text c="dimmed" ta="center" mt="xl">
							Stell eine Frage, um anzufangen.
						</Text>
					)}
					{messages.map((m) => (
						<Bubble key={m.id} message={m} />
					))}
				</Stack>
			</ScrollArea>
			</Split.Pane>

			{/* Bottom pane: pickers + composer + send. Initial 35 % gives
			    the rich-text editor enough room for two-to-three lines
			    of toolbar + content without scrolling; the user can drag
			    the resizer up to gain more authoring space. */}
			<Split.Pane initialHeight="35%" minHeight="15%">
			<Paper
				p="sm"
				radius={0}
				style={{
					borderTop: "1px solid var(--mantine-color-gray-3)",
					background: "light-dark(var(--mantine-color-gray-0), var(--mantine-color-dark-7))",
					height: "100%",
					display: "flex",
					flexDirection: "column",
					minHeight: 0,
				}}
			>
				{/*
					Mapping + stream pickers — only present in events
					mode. The two Mantine selects render their own
					floating labels ("Kontext" / "Stream"), which
					sit inside the input border and slide up to the
					corner when a value is chosen — the same pattern
					Settings → Connection uses, so the events row
					reads as native UI rather than a bolt-on.
					Mapping is fixed-width (police / support never
					need more); stream takes the rest of the row.
				*/}
				{eventsMode && (
					<Box mb="sm">
						<Box
							style={{
								display: "flex",
								gap: 8,
								alignItems: "flex-start",
							}}
						>
							<Box style={{ width: 160, flexShrink: 0 }}>
								<MappingPicker />
							</Box>
							<Box style={{ flex: 1, minWidth: 0 }}>
								<StreamPicker />
							</Box>
						</Box>
						{/*
							Reserved hint slot. Always rendered with a
							fixed height so the composer below does not
							jump as the user picks Kontext / Stream.
							The text inside is conditional, the slot
							itself isn't.
						*/}
						<Text
							size="xs"
							c="dimmed"
							mt={6}
							style={{ minHeight: 18, lineHeight: "18px" }}
						>
							{composerLocked
								? (!hasMapping
									? "Bitte zuerst Kontext und Stream wählen."
									: "Bitte Stream wählen, bevor eine Frage gestellt wird.")
								: "\u00A0"}
						</Text>
					</Box>
				)}

				<Box
					style={{
						display: "flex",
						gap: 8,
						alignItems: "stretch",
						flex: 1,
						minHeight: 0,
					}}
				>
					{/*
						Wrapping the Textarea in a Box that owns the
						drag handlers. Putting onDragOver/onDrop on
						the Mantine <Textarea> directly does not
						reliably reach the inner <textarea> element
						— the native drop default of the textarea
						(insert-at-caret) and the propagated handler
						compete, and on Mantine v9 the props land on
						a wrapper that does not always sit between
						the user's cursor and the textarea. The
						outer Box catches the drop first, calls
						preventDefault to suppress the textarea's
						native insert, and replaces the draft.
					*/}
					<Box
						onDragOver={onDragOver}
						onDrop={onDrop}
						style={{ flex: 1, minWidth: 0, display: "flex", flexDirection: "column" }}
					>
						<ComposerRichText
							ref={composerRef}
							placeholder="Frage stellen… (Enter zum Senden, Strg/Cmd+Plus für View-Auswahl, Shift+Enter für Zeilenumbruch)"
							value={draft}
							onChange={setDraft}
							onSend={() => { if (!composerLocked) void send(); }}
							onTogglePicker={togglePicker}
							disabled={composerLocked}
						/>
					</Box>
					{busy ? (
						/*
							Stop button. Shown while the agent loop
							runs on the bun side; clicking it fires
							cancelTurn, which aborts the in-flight
							LLM fetch and unwinds the loop. The
							placeholder bubble fills with
							"Abgebrochen." once the cancellation
							lands. Filled red so the user reads it
							as "interrupt", not "send".
						*/
						<ActionIcon
							size="lg"
							variant="filled"
							color="red"
							aria-label="Anfrage abbrechen"
							onClick={() => onCancel?.()}
						>
							<IconPlayerStopFilled size={16} />
						</ActionIcon>
					) : (
						/*
							Send button with right-click context menu.
							Left-click sends normally; right-click opens
							a small Menu with "Verlauf löschen" — keeping
							the UI surface clean while the action is still
							one click away.
						*/
						<Menu
							opened={sendMenuOpen}
							onClose={() => setSendMenuOpen(false)}
							position="top-end"
							withArrow
						>
							<Menu.Target>
								<ActionIcon
									size="lg"
									variant="filled"
									color="brand"
									aria-label="Senden"
									onClick={() => void send()}
									onContextMenu={(e) => {
										e.preventDefault();
										setSendMenuOpen((o) => !o);
									}}
									disabled={!draft.trim() || composerLocked}
								>
									<IconSend size={18} />
								</ActionIcon>
							</Menu.Target>
							<Menu.Dropdown>
								<Menu.Item
									leftSection={<IconEraser size={14} />}
									disabled={messages.length === 0}
									onClick={() => {
										setSendMenuOpen(false);
										onClear?.();
									}}
								>
									Verlauf löschen
								</Menu.Item>
							</Menu.Dropdown>
						</Menu>
					)}
				</Box>

				{/*
					Pill row with reserved height — forms mode only.
					The Box always occupies PILL_ROW_HEIGHT pixels so
					the chat above never shifts when the pill toggles
					on or off. When no pill or picker is showing the
					row is simply empty space — the input bar stays
					put.

					Events mode does not surface a domain-driven view
					hint (the LLM doesn't pick tools there), so the
					whole row is omitted: no pill, no reserved space.
				*/}
				{!eventsMode && !documentsMode && !askMode && (
					<>
						<Box
							mt="xs"
							style={{
								minHeight: PILL_ROW_HEIGHT,
								display:   "flex",
								alignItems: "center",
							}}
						>
							{effectiveView && (
								<ViewPill
									viewName={effectiveView.name}
									viewTitle={effectiveView.title}
									onClear={clearHint}
									onClick={() => setPickerOpen((open) => !open)}
								/>
							)}
						</Box>

						{pickerOpen && resolvedViews.length > 0 && (
							<Box mt="xs">
								<ChatViewPicker
									views={resolvedViews}
									activeName={effectiveView?.name ?? null}
									onSelect={(v) => {
										setManualView(v);
										setPickerOpen(false);
									}}
									onClose={() => setPickerOpen(false)}
								/>
							</Box>
						)}
					</>
				)}
			</Paper>
			</Split.Pane>
		</Split>
	);
}
