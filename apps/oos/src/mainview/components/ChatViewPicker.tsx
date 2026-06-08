// ChatViewPicker.tsx — Inline view picker shown below the chat input.
//
// Triggered by Ctrl+Plus (Cmd+Plus on macOS) while typing a
// question. Lists every view
// available for the domain the resolver has just identified. The
// user picks with Up/Down + Enter or click; selection bubbles back
// up to the parent which sets the active view hint.
//
// The picker also has a "Kein Hinweis" entry that explicitly
// clears the hint, so a user who would rather let the LLM choose
// the columns has a one-keystroke escape.
//
// Visually parked below the input as a flat panel — same look as
// command palettes (Linear, Slack), no modal chrome. Closes on
// Escape, on outside click, and after a selection.

import { useEffect, useRef } from "react";
import { Box, Group, Paper, Text } from "@mantine/core";
import { IconLayoutGrid } from "@tabler/icons-react";

import type { ViewIndexEntry } from "../store/resolver";

interface ChatViewPickerProps {
	views:        ViewIndexEntry[];
	activeName?:  string | null;
	onSelect:     (view: ViewIndexEntry | null) => void;
	onClose:      () => void;
}

export function ChatViewPicker({
	views,
	activeName,
	onSelect,
	onClose,
}: ChatViewPickerProps) {
	const containerRef = useRef<HTMLDivElement>(null);

	// Close on Escape and on a click outside the picker.
	useEffect(() => {
		const onKey = (e: KeyboardEvent) => {
			if (e.key === "Escape") {
				e.preventDefault();
				onClose();
			}
		};
		const onDown = (e: MouseEvent) => {
			if (!containerRef.current) return;
			if (containerRef.current.contains(e.target as Node)) return;
			onClose();
		};
		document.addEventListener("keydown", onKey);
		document.addEventListener("mousedown", onDown);
		return () => {
			document.removeEventListener("keydown", onKey);
			document.removeEventListener("mousedown", onDown);
		};
	}, [onClose]);

	if (views.length === 0) {
		return (
			<Paper ref={containerRef} withBorder p="xs" radius="sm" shadow="sm">
				<Text size="xs" c="dimmed">
					Keine View-DSL für diese Domain gefunden.
				</Text>
			</Paper>
		);
	}

	return (
		<Paper
			ref={containerRef}
			withBorder
			p={4}
			radius="sm"
			shadow="sm"
			style={{
				maxHeight: 240,
				overflow:  "auto",
			}}
		>
			<Text size="xs" c="dimmed" px="xs" pt={4} pb={2}>
				View wählen
			</Text>
			{views.map((v) => (
				<PickerRow
					key={v.name}
					view={v}
					active={v.name === activeName}
					onClick={() => onSelect(v)}
				/>
			))}
			<Box my={4} style={{ height: 1, background: "light-dark(var(--mantine-color-gray-2), var(--mantine-color-dark-5))" }} />
			<ClearRow active={!activeName} onClick={() => onSelect(null)} />
		</Paper>
	);
}

interface PickerRowProps {
	view:    ViewIndexEntry;
	active:  boolean;
	onClick: () => void;
}

function PickerRow({ view, active, onClick }: PickerRowProps) {
	return (
		<Box
			onClick={onClick}
			style={{
				display:    "flex",
				alignItems: "center",
				gap:        8,
				padding:    "6px 8px",
				borderRadius: 4,
				cursor:     "pointer",
				background: active
					? "var(--mantine-color-brand-0)"
					: "transparent",
			}}
			onMouseEnter={(e) => {
				if (!active) e.currentTarget.style.background = "light-dark(var(--mantine-color-gray-1), var(--mantine-color-dark-6))";
			}}
			onMouseLeave={(e) => {
				if (!active) e.currentTarget.style.background = "transparent";
			}}
		>
			<IconLayoutGrid size={14} color="var(--mantine-primary-color-filled)" />
			<Box style={{ flex: 1, minWidth: 0 }}>
				<Group gap={6} wrap="nowrap">
					<Text size="sm" fw={500}>
						{view.title}
					</Text>
					{view.default && (
						<Text size="xs" c="brand">
							default
						</Text>
					)}
				</Group>
				<Text size="xs" c="dimmed" truncate>
					{view.name} · {fieldCountLabel(view.fields.length)}
				</Text>
			</Box>
		</Box>
	);
}

/**
 * fieldCountLabel turns the field-count number into a label that
 * makes sense to a human. The catalog walks Tables only — detail
 * views with widget bindings produce zero. "0 Felder" reads as
 * "this view shows nothing", which is wrong; for those views we
 * mean "no column hint, agent fetches the full domain".
 *
 * If we ever start collecting widget bindings into the field
 * list, the count will be the real subset and this function does
 * the right thing without further changes.
 */
function fieldCountLabel(n: number): string {
	if (n === 0) return "ohne Spalten-Hinweis";
	if (n === 1) return "1 Feld";
	return `${n} Felder`;
}

function ClearRow({ active, onClick }: { active: boolean; onClick: () => void }) {
	return (
		<Box
			onClick={onClick}
			style={{
				padding:      "6px 8px",
				borderRadius: 4,
				cursor:       "pointer",
				background:   active
					? "light-dark(var(--mantine-color-gray-1), var(--mantine-color-dark-6))"
					: "transparent",
			}}
			onMouseEnter={(e) => {
				if (!active) e.currentTarget.style.background = "light-dark(var(--mantine-color-gray-1), var(--mantine-color-dark-6))";
			}}
			onMouseLeave={(e) => {
				if (!active) e.currentTarget.style.background = "transparent";
			}}
		>
			<Text size="sm" c="dimmed">
				Kein View-Hinweis
			</Text>
		</Box>
	);
}
