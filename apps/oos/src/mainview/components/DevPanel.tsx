// DevPanel.tsx — Dev agent tab.
//
// Sends a plain-text prompt to the Tauri `dev_run` command, which runs a
// RIG agent loop backed by bench tools (fs, exec, git) in native Rust.
// The agent streams events back via Tauri's event bus (`dev_event`); this
// panel subscribes once on mount and renders them incrementally.
//
// Event kinds (from dev.rs DevEvent):
//   token      — intermediate text chunk
//   tool_call  — agent decided to call a tool
//   tool_result — raw result bench returned
//   done       — final answer; agent loop finished
//   error      — something went wrong
//
// The panel reads base_url / api_key / model / nats_url from app settings
// so the user does not have to re-enter them — same values the main chat uses.

import { useCallback, useEffect, useRef, useState } from "react";
import {
	ActionIcon,
	Badge,
	Box,
	Code,
	Group,
	ScrollArea,
	Stack,
	Text,
	Textarea,
	Tooltip,
} from "@mantine/core";
import { IconPlayerStop, IconSend } from "@tabler/icons-react";
import { invoke } from "@tauri-apps/api/core";
import { listen, type UnlistenFn } from "@tauri-apps/api/event";

import { useAppSettings } from "../store/settings";

// ── Event types (mirror of dev.rs DevEvent) ──────────────────────────

type DevEventKind = "token" | "tool_call" | "tool_result" | "done" | "error";

interface DevEventPayload {
	kind:    DevEventKind;
	// token / done / error
	text?:   string;
	answer?: string;
	message?: string;
	// tool_call
	name?:   string;
	args?:   string;
	// tool_result
	result?: string;
}

// ── Local message model ───────────────────────────────────────────────

type MsgKind = "user" | "token" | "tool_call" | "tool_result" | "answer" | "error";

interface Msg {
	id:   number;
	kind: MsgKind;
	text: string;
	/** For tool messages — the tool name. */
	tool?: string;
}

let msgSeq = 0;
function nextMsg(kind: MsgKind, text: string, tool?: string): Msg {
	return { id: ++msgSeq, kind, text, tool };
}

// ── Component ─────────────────────────────────────────────────────────

export function DevPanel() {
	const { settings }   = useAppSettings();
	const [prompt, setPrompt] = useState("");
	const [msgs,   setMsgs  ] = useState<Msg[]>([]);
	const [busy,   setBusy  ] = useState(false);
	const viewport = useRef<HTMLDivElement>(null);
	const unlistenRef = useRef<UnlistenFn | null>(null);

	// Auto-scroll to bottom whenever messages change.
	useEffect(() => {
		viewport.current?.scrollTo({ top: viewport.current.scrollHeight, behavior: "smooth" });
	}, [msgs]);

	// Attach Tauri event listener once on mount.
	useEffect(() => {
		let active = true;
		void listen<DevEventPayload>("dev_event", (ev) => {
			if (!active) return;
			const p = ev.payload;
			switch (p.kind) {
				case "token":
					// Append token text to the last token message, or start a new one.
					setMsgs((prev) => {
						const last = prev[prev.length - 1];
						if (last?.kind === "token") {
							return [
								...prev.slice(0, -1),
								{ ...last, text: last.text + (p.text ?? "") },
							];
						}
						return [...prev, nextMsg("token", p.text ?? "")];
					});
					break;
				case "tool_call":
					setMsgs((prev) => [
						...prev,
						nextMsg("tool_call", p.args ?? "", p.name),
					]);
					break;
				case "tool_result":
					setMsgs((prev) => [
						...prev,
						nextMsg("tool_result", p.result ?? "", p.name),
					]);
					break;
				case "done":
					setMsgs((prev) => [...prev, nextMsg("answer", p.answer ?? "")]);
					setBusy(false);
					break;
				case "error":
					setMsgs((prev) => [...prev, nextMsg("error", p.message ?? "Unknown error")]);
					setBusy(false);
					break;
			}
		}).then((fn) => {
			unlistenRef.current = fn;
		});
		return () => {
			active = false;
			unlistenRef.current?.();
		};
	}, []);

	const handleSend = useCallback(async () => {
		const text = prompt.trim();
		if (!text || busy) return;
		setPrompt("");
		setMsgs((prev) => [...prev, nextMsg("user", text)]);
		setBusy(true);
		try {
			await invoke("dev_run", {
				args: {
					prompt:   text,
					base_url: settings.llmBaseUrl,
					api_key:  settings.llmApiKey ?? "",
					model:    settings.llmModel,
					nats_url: settings.natsUrl,
				},
			});
			// The command returns immediately; events drive the rest.
		} catch (err) {
			setMsgs((prev) => [
				...prev,
				nextMsg("error", err instanceof Error ? err.message : String(err)),
			]);
			setBusy(false);
		}
	}, [prompt, busy, settings]);

	const handleKeyDown = (e: React.KeyboardEvent<HTMLTextAreaElement>) => {
		if (e.key === "Enter" && !e.shiftKey) {
			e.preventDefault();
			void handleSend();
		}
	};

	return (
		<Stack h="100%" gap={0}>
			{/* Message log */}
			<ScrollArea
				viewportRef={viewport}
				style={{ flex: 1, minHeight: 0 }}
				p="md"
			>
				<Stack gap="sm">
					{msgs.length === 0 && (
						<Text c="dimmed" size="sm" ta="center" mt="xl">
							Dev agent — bench tools available (fs, exec, git).
							<br />
							Model: {settings.llmModel}
						</Text>
					)}
					{msgs.map((m) => <MsgRow key={m.id} msg={m} />)}
				</Stack>
			</ScrollArea>

			{/* Input bar */}
			<Box
				p="md"
				style={{
					borderTop: "1px solid light-dark(var(--mantine-color-gray-2), var(--mantine-color-dark-5))",
				}}
			>
				<Group align="flex-end" gap="xs">
					<Textarea
						style={{ flex: 1 }}
						placeholder="Ask the Dev agent… (Enter to send, Shift+Enter for newline)"
						minRows={2}
						maxRows={8}
						autosize
						value={prompt}
						onChange={(e) => setPrompt(e.currentTarget.value)}
						onKeyDown={handleKeyDown}
						disabled={busy}
					/>
					<Tooltip label={busy ? "Running…" : "Send (Enter)"}>
						<ActionIcon
							size="lg"
							variant={busy ? "light" : "filled"}
							color={busy ? "gray" : "violet"}
							onClick={() => void handleSend()}
							disabled={busy}
						>
							{busy ? <IconPlayerStop size={16} /> : <IconSend size={16} />}
						</ActionIcon>
					</Tooltip>
				</Group>
			</Box>
		</Stack>
	);
}

// ── Message row renderer ──────────────────────────────────────────────

function MsgRow({ msg }: { msg: Msg }) {
	switch (msg.kind) {
		case "user":
			return (
				<Box ta="right">
					<Text
						ml="auto"
						px="sm"
						py={4}
						style={{
							background: "var(--mantine-color-violet-6)",
							color: "#fff",
							borderRadius: 8,
							whiteSpace: "pre-wrap",
							wordBreak: "break-word",
						}}
					>
						{msg.text}
					</Text>
				</Box>
			);

		case "token":
			return (
				<Text
					size="sm"
					c="dimmed"
					style={{ whiteSpace: "pre-wrap", wordBreak: "break-word" }}
				>
					{msg.text}
				</Text>
			);

		case "tool_call":
			return (
				<Box>
					<Group gap={6} mb={4}>
						<Badge size="xs" variant="outline" color="indigo">
							{msg.tool}
						</Badge>
						<Text size="xs" c="dimmed">call</Text>
					</Group>
					<Code block style={{ fontSize: 11 }}>
						{msg.text}
					</Code>
				</Box>
			);

		case "tool_result":
			return (
				<Box>
					<Group gap={6} mb={4}>
						<Badge size="xs" variant="light" color="teal">
							{msg.tool}
						</Badge>
						<Text size="xs" c="dimmed">result</Text>
					</Group>
					<Code
						block
						style={{ fontSize: 11, maxHeight: 240, overflow: "auto" }}
					>
						{msg.text}
					</Code>
				</Box>
			);

		case "answer":
			return (
				<Box
					p="sm"
					style={{
						background: "light-dark(var(--mantine-color-gray-0), var(--mantine-color-dark-6))",
						borderRadius: 8,
						whiteSpace: "pre-wrap",
						wordBreak: "break-word",
					}}
				>
					<Text size="sm">{msg.text}</Text>
				</Box>
			);

		case "error":
			return (
				<Text size="sm" c="red" style={{ whiteSpace: "pre-wrap" }}>
					⚠ {msg.text}
				</Text>
			);
	}
}
