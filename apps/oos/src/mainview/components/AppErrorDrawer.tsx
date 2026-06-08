// src/mainview/components/AppErrorDrawer.tsx — app-level error drawer.
//
// Opened programmatically via openAppError(). Sits at the root of the
// component tree so it is always reachable regardless of which tab is
// active. Shows full error details with a copy-to-clipboard button.

import { useState, useCallback }         from "react";
import {
	Drawer, Stack, Text, Group,
	Tooltip, UnstyledButton, Code,
} from "@mantine/core";
import { IconCopy, IconCheck }           from "@tabler/icons-react";

export type AppErrorPayload = {
	title:     string;
	message:   string;
	service:   string;
	subject:   string;
	ts:        string;
};

// ─── Module-level singleton ──────────────────────────────────────────────────

let _open: ((payload: AppErrorPayload) => void) | null = null;

/**
 * openAppError triggers the app-level error drawer from anywhere —
 * including non-React code such as the RPC message handler.
 */
export function openAppError(payload: AppErrorPayload): void {
	_open?.(payload);
}

/**
 * openAppErrorLog opens the error drawer with a generic
 * "show the log" payload. Used by Spotlight where no specific
 * error is in context — the drawer shows the last error or empty.
 */
export function openAppErrorLog(): void {
	_open?.({
		title:   "Error Log",
		message: "Open the Settings · Logs tab to see the full local log.",
		service: "",
		subject: "",
		ts:      new Date().toISOString(),
	});
}

// ─── Component ─────────────────────────────────────────────────────────────

export function AppErrorDrawer() {
	const [error,  setError]  = useState<AppErrorPayload | null>(null);
	const [copied, setCopied] = useState(false);

	// Register the singleton opener once on mount.
	const ref = useCallback((node: unknown) => {
		if (node === null) { _open = null; return; }
		_open = setError;
	}, []);

	const handleCopy = async () => {
		if (!error) return;
		const text = [
			`Service: ${error.service}`,
			`Subject: ${error.subject}`,
			`Time:    ${new Date(error.ts).toLocaleString()}`,
			"",
			error.message,
		].join("\n");
		await navigator.clipboard.writeText(text);
		setCopied(true);
		setTimeout(() => setCopied(false), 1500);
	};

	return (
		<>
			{/* invisible sentinel so useCallback can register _open */}
			<span ref={ref as any} style={{ display: "none" }} />

			<Drawer
				opened={!!error}
				onClose={() => setError(null)}
				title={error?.title ?? "Error"}
				position="right"
				size="lg"
				padding="md"
				offset={8}
				radius="md"
			>
				{error && (
					<Stack gap="xs">
						<Text size="xs" c="dimmed">
							{error.service} · {error.subject} · {new Date(error.ts).toLocaleTimeString()}
						</Text>

						<Code
							block
							style={{ whiteSpace: "pre-wrap", wordBreak: "break-word", fontSize: 13 }}
						>
							{error.message}
						</Code>

						<Group justify="flex-end">
							<Tooltip label={copied ? "Copied!" : "Copy to clipboard"} withArrow>
								<UnstyledButton
									onClick={() => void handleCopy()}
									style={{
										display:        "inline-flex",
										alignItems:     "center",
										justifyContent: "center",
										padding:        6,
										borderRadius:   999,
										border:         "1px solid var(--mantine-color-gray-6)",
									}}
								>
									{copied
										? <IconCheck size={16} />
										: <IconCopy  size={16} />
									}
								</UnstyledButton>
							</Tooltip>
						</Group>
					</Stack>
				)}
			</Drawer>
		</>
	);
}
