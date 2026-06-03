// ChatPanel.tsx — inline assistant panel for the oosd mainview.
//
// Same features as the old detached chatview/App.tsx but rendered
// directly inside the mainview shell — no cross-window RPC needed.
// Uses the mainview rpc for chat calls and the existing
// setChatTokenHandler / setChatDoneHandler from chatview/rpc.ts.

import { useCallback, useEffect, useRef, useState } from "react";
import {
	ActionIcon,
	Box,
	Group,
	Loader,
	Paper,
	ScrollArea,
	Text,
	Textarea,
	Title,
} from "@mantine/core";
import { IconPlayerStopFilled, IconSend } from "@tabler/icons-react";
import Editor from "@monaco-editor/react";

import { rpc, setChatTokenHandler, setChatDoneHandler } from "../rpc";
import { DslPills }                     from "./DslPills";
import type { ChatMessage, ChatSettings } from "../chat-types";
import { initDslResolver, resolveIntent } from "../embedder/dsl-resolver";
import { useOosdSettings }              from "../store/settings";

// ─── ChatPanel ───────────────────────────────────────────────────────

export function ChatPanel({ activeViewId, activeKind }: { activeViewId?: string; activeKind?: string }) {
	const { settings: oosdSettings } = useOosdSettings();

	const settings: ChatSettings = {
		llmBaseUrl: oosdSettings.llmBaseUrl,
		llmApiKey:  oosdSettings.llmApiKey,
		llmModel:   oosdSettings.llmModel,
	};

	const [history,   setHistory]   = useState<ChatMessage[]>([]);
	const [input,     setInput]     = useState("");
	const [busy,      setBusy]      = useState(false);
	const [streaming, setStreaming] = useState("");
	const [error,     setError]     = useState<string | null>(null);
	const [aborted,   setAborted]   = useState(false);
	const streamingRef = useRef("");
	const viewport     = useRef<HTMLDivElement>(null);
	const abortRef     = useRef(false);

	const [resolvedDomainIds, setResolvedDomainIds] = useState<string[]>([]);
	const [resolvedViewId,    setResolvedViewId]    = useState<string | undefined>();
	const [clearedDomains,    setClearedDomains]    = useState<Set<string>>(new Set());
	const [viewCleared,       setViewCleared]       = useState(false);

	// ── Init resolver ────────────────────────────────────────────
	useEffect(() => {
		void initDslResolver(
			async () => rpc.listDomainIds({}),
			async () => rpc.listViewIds({}),
		);
	}, []);

	// ── Streaming handlers ────────────────────────────────────────
	useEffect(() => {
		setChatTokenHandler(({ delta }) => {
			streamingRef.current += delta;
			setStreaming((prev) => prev + delta);
		});
		setChatDoneHandler(({ ok, error: err }) => {
			setBusy(false);
			const partial = streamingRef.current;
			streamingRef.current = "";
			setStreaming("");
			if (!ok) { setError(err ?? "Unknown error"); return; }
			if (partial) {
				extractDslBlocks(partial).forEach(({ kind, id, source }) => {
					void rpc.openDslInEditor({ kind, id, source });
				});
				setHistory((h) => [...h, { role: "assistant", content: partial }]);
			}
		});
	}, []);

	// ── Scroll ───────────────────────────────────────────────────
	useEffect(() => {
		viewport.current?.scrollTo({ top: viewport.current.scrollHeight, behavior: "smooth" });
	}, [history, streaming, busy]);

	// ── Live intent resolution ────────────────────────────────────
	const resolveDebounced = useCallback(() => {
		const text = input.trim();
		if (!text) {
			setResolvedDomainIds([]);
			setResolvedViewId(undefined);
			setClearedDomains(new Set());
			setViewCleared(false);
			return;
		}
		setClearedDomains(new Set());
		setViewCleared(false);
		void (async () => {
			const intent = await resolveIntent(text);
			setResolvedDomainIds(intent.domainIds);
			setResolvedViewId(intent.viewId);
		})();
	}, [input]);

	useEffect(() => {
		const h = setTimeout(resolveDebounced, 300);
		return () => clearTimeout(h);
	}, [resolveDebounced]);

	// ── Send ─────────────────────────────────────────────────────
	async function send() {
		const text = input.trim();
		if (!text || busy) return;
		setInput("");
		setError(null);
		setBusy(true);
		setStreaming("");
		streamingRef.current = "";

		const userMsg: ChatMessage = { role: "user", content: text };
		setHistory((h) => [...h, userMsg]);

		const domainSources: { id: string; source: string }[] = [];
		for (const id of resolvedDomainIds) {
			const res = await rpc.loadDomainSource({ id });
			if (res.source) domainSources.push({ id, source: res.source });
		}

		rpc.chat({
			settings,
			history,
			message: text,
			resolvedContext: domainSources.length > 0 || resolvedViewId
				? { domainSources, viewId: resolvedViewId }
				: undefined,
			activeViewId: activeKind === "view" ? activeViewId : undefined,
		}).catch((e) => { setError(String(e)); setBusy(false); });
	}

	function handleKeyDown(e: React.KeyboardEvent) {
		if (e.key === "Enter" && !e.shiftKey) { e.preventDefault(); void send(); }
	}

	function cancel() {
		abortRef.current = true;
		setAborted(true);
		setBusy(false);
		setStreaming("");
		const partial = streamingRef.current;
		streamingRef.current = "";
		if (partial) setHistory((h) => [...h, { role: "assistant", content: partial + "\n\n*Abgebrochen.*" }]);
		setAborted(false);
	}

	const visibleDomains = resolvedDomainIds.filter((id) => !clearedDomains.has(id));
	const visibleView    = viewCleared ? undefined : resolvedViewId;

	return (
		<Box
			style={{
				display:       "flex",
				flexDirection: "column",
				height:        "100%",
				borderRight:   "1px solid var(--mantine-color-default-border)",
				background:    "var(--mantine-color-gray-0)",
			}}
		>
			{/* Header */}
			<Box
				px="md" py="xs"
				style={{
					borderBottom: "1px solid var(--mantine-color-default-border)",
					background:   "white",
					flexShrink:   0,
				}}
			>
				<Title order={6} style={{ margin: 0 }}>Assistant</Title>
				<Text size="xs" c="dimmed">{settings.llmModel || "no model configured"}</Text>
			</Box>

			{/* Thread */}
			<ScrollArea flex={1} viewportRef={viewport} style={{ minHeight: 0 }} p="sm">
				{history.length === 0 && !streaming && (
					<Text size="xs" c="dimmed" ta="center" mt="xl">
						Ask about domains, views, event types or mappings.
					</Text>
				)}
				{history.map((msg, i) => <MessageBubble key={i} msg={msg} />)}
				{streaming && <MessageBubble msg={{ role: "assistant", content: streaming }} streaming />}
				{busy && !streaming && (
					<Group gap="xs" mt="sm">
						<Loader size="xs" />
						<Text size="xs" c="dimmed">Thinking…</Text>
					</Group>
				)}
				{error && <Text size="xs" c="red" mt="xs">{error}</Text>}
			</ScrollArea>

			{/* Pills */}
			{(visibleDomains.length > 0 || visibleView) && (
				<DslPills
					domainIds={visibleDomains}
					viewId={visibleView}
					onClearDomain={(id) => setClearedDomains((p) => new Set([...p, id]))}
					onClearView={() => setViewCleared(true)}
					onClickView={() => {
						if (resolvedViewId) void rpc.openDslInEditor({ kind: "view", id: resolvedViewId, source: "" });
					}}
				/>
			)}

			{/* Input */}
			<Box
				p="xs"
				style={{
					borderTop:  "1px solid var(--mantine-color-default-border)",
					background: "white",
					flexShrink: 0,
				}}
			>
				<Group gap="xs" align="flex-end">
					<Textarea
						style={{ flex: 1 }}
						placeholder="Frage stellen… (Enter zum Senden, Shift+Enter für Zeilenumbruch)"
						value={input}
						autosize
						minRows={2}
						maxRows={6}
						size="xs"
						onChange={(e) => setInput(e.currentTarget.value)}
						onKeyDown={handleKeyDown}
						disabled={busy}
					/>
					{busy ? (
						<ActionIcon
							size="lg"
							variant="filled"
							color="red"
							aria-label="Anfrage abbrechen"
							onClick={cancel}
						>
							<IconPlayerStopFilled size={16} />
						</ActionIcon>
					) : (
						<ActionIcon
							size="lg"
							variant="filled"
							color="indigo"
							aria-label="Senden"
							disabled={!input.trim()}
							onClick={() => void send()}
						>
							<IconSend size={16} />
						</ActionIcon>
					)}
				</Group>
			</Box>
		</Box>
	);
}

// ─── MessageBubble ────────────────────────────────────────────────────

function MessageBubble({ msg, streaming = false }: { msg: ChatMessage; streaming?: boolean }) {
	const isUser = msg.role === "user";
	const parts  = isUser ? null : parseMessageParts(msg.content);

	if (isUser) {
		return (
			<Box mb="xs" style={{ display: "flex", justifyContent: "flex-end" }}>
				<Paper px="sm" py={6} radius="md" style={{ maxWidth: "85%", background: "var(--mantine-color-indigo-6)", color: "white", whiteSpace: "pre-wrap", wordBreak: "break-word" }}>
					<Text size="xs">{msg.content}</Text>
				</Paper>
			</Box>
		);
	}

	return (
		<Box mb="xs" style={{ display: "flex", justifyContent: "flex-start" }}>
			<Paper px="sm" py={6} radius="md" style={{ width: "95%", background: "white", border: "1px solid var(--mantine-color-default-border)", wordBreak: "break-word" }}>
				{parts?.map((part, i) =>
					part.type === "text" ? (
						<Text key={i} size="xs" style={{ whiteSpace: "pre-wrap" }}>{part.content}</Text>
					) : (
						<CodeBlock key={i} content={part.content} />
					)
				)}
				{streaming && <Text span size="xs" style={{ opacity: 0.5 }}>▍</Text>}
			</Paper>
		</Box>
	);
}

// ─── CodeBlock ────────────────────────────────────────────────────────

function CodeBlock({ content }: { content: string }) {
	const firstLine = content.split("\n").find((l) => l.trim() && !l.trim().startsWith("//")) ?? "";
	const isDomain  = /^\s*domain\s+/.test(firstLine);
	const isView    = /^\s*(?:default\s+)?view\s+/.test(firstLine);
	const isDsl     = isDomain || isView;
	const kind: "domain" | "view" = isDomain ? "domain" : "view";
	const idMatch   = firstLine.match(/^\s*(?:default\s+)?(?:domain|view)\s+(\w+)/);
	const id        = idMatch?.[1] ?? "new";

	function openInEditor() {
		void rpc.openDslInEditor({ kind, id, source: content });
	}

	return (
		<Box my={4} style={{ borderRadius: 4, overflow: "hidden", border: "1px solid var(--mantine-color-gray-3)", width: "100%" }}>
			<Group justify="space-between" px={6} py={2} style={{ background: "var(--mantine-color-gray-1)", borderBottom: "1px solid var(--mantine-color-gray-3)" }}>
				<Text size="xs" c="dimmed" ff="monospace">{isDsl ? `${kind}: ${id}` : "code"}</Text>
				{isDsl && (
					<Text size="xs" c="indigo" style={{ cursor: "pointer", textDecoration: "underline" }} onClick={openInEditor}>
						→ Im Editor öffnen
					</Text>
				)}
			</Group>
			<Editor
				height={Math.min(40 + content.split("\n").length * 18, 350)}
				language="oos-domain"
				value={content}
				options={{ readOnly: true, minimap: { enabled: false }, scrollBeyondLastLine: false, fontSize: 11, lineNumbers: "off", folding: false, automaticLayout: true }}
			/>
		</Box>
	);
}

// ─── Helpers ──────────────────────────────────────────────────────────

type MessagePart = { type: "text"; content: string } | { type: "code"; lang: string; content: string };

function parseMessageParts(text: string): MessagePart[] {
	const parts: MessagePart[] = [];
	const fence = /```(\w*)\n([\s\S]*?)```/g;
	let last = 0;
	let m: RegExpExecArray | null;
	while ((m = fence.exec(text)) !== null) {
		if (m.index > last) parts.push({ type: "text", content: text.slice(last, m.index) });
		parts.push({ type: "code", lang: m[1] ?? "", content: m[2]?.trim() ?? "" });
		last = m.index + m[0].length;
	}
	if (last < text.length) parts.push({ type: "text", content: text.slice(last) });
	return parts.length > 0 ? parts : [{ type: "text", content: text }];
}

interface DslBlock { kind: "domain" | "view"; id: string; source: string }

function extractDslBlocks(text: string): DslBlock[] {
	const blocks: DslBlock[] = [];
	const fence = /```(?:dsl|domain|view|)\n([\s\S]*?)```/g;
	let m: RegExpExecArray | null;
	while ((m = fence.exec(text)) !== null) {
		const source = m[1]?.trim() ?? "";
		if (!source) continue;
		const firstLine = source.split("\n").find((l) => l.trim() && !l.trim().startsWith("//")) ?? "";
		const isDomain  = /^\s*domain\s+/.test(firstLine);
		const isView    = /^\s*(?:default\s+)?view\s+/.test(firstLine);
		if (!isDomain && !isView) continue;
		const kind = isDomain ? "domain" : "view";
		const idMatch = firstLine.match(/^\s*(?:default\s+)?(?:domain|view)\s+(\w+)/);
		blocks.push({ kind, id: idMatch?.[1] ?? "new", source });
	}
	return blocks;
}
