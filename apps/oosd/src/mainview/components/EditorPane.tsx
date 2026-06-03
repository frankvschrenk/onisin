// EditorPane.tsx — wraps SourceEditor + a live Preview tab + a Help
// tab.
//
// For domain rows the preview tab is hidden — domain rendering needs
// the cross-reference resolver (which views to render the domain in)
// and that lands in a follow-up step. For view rows the user can
// flip between editing source, seeing the rendered Mantine form, and
// looking up which widget tag maps to which Mantine component. Help
// is always available because the reference is useful regardless of
// what the user is editing.
//
// The Preview tab also exposes a detach button: clicking it asks
// the bun process to spawn a separate preview window. When the
// window is open the button changes to a re-attach affordance and
// closing the window flips it back. The in-tab preview keeps
// rendering either way — the detached window mirrors it rather
// than replacing it.

import { ActionIcon, Box, Group, Tabs, Tooltip } from "@mantine/core";
import { useState } from "react";

import type { Kind } from "../types";
import { ChunkPanel } from "./ChunkPanel";
import { SourceEditor } from "./SourceEditor";
import { Preview } from "./Preview";
import { HelpPanel } from "./HelpPanel";

type Tab = "source" | "preview" | "chunk" | "help";

export function EditorPane({
	kind,
	selected,
	source,
	dirty,
	saving,
	onSourceChange,
	onSave,
	previewOpen,
	onOpenPreview,
	onClosePreview,
}: {
	kind: Kind;
	selected: string | null;
	source: string;
	dirty: boolean;
	saving: boolean;
	onSourceChange: (v: string) => void;
	onSave: () => void;
	previewOpen: boolean;
	onOpenPreview: () => void;
	onClosePreview: () => void;
}) {
	const [tab, setTab] = useState<Tab>("source");
	const previewAvailable = kind === "view";
	const showDetachButton = previewAvailable && tab === "preview";

	return (
		<Box style={{ display: "flex", flexDirection: "column", height: "100%" }}>
			<Tabs
				value={tab}
				onChange={(v) => setTab((v as Tab) ?? "source")}
				keepMounted={false}
				style={{ flex: 1, display: "flex", flexDirection: "column", minHeight: 0 }}
			>
				<Group
					justify="space-between"
					wrap="nowrap"
					style={{
						borderBottom: "1px solid var(--mantine-color-default-border)",
					}}
				>
					<Tabs.List style={{ borderBottom: "none" }}>
						<Tabs.Tab value="source">Source</Tabs.Tab>
						<Tabs.Tab value="preview" disabled={!previewAvailable}>
							Preview
						</Tabs.Tab>
						<Tabs.Tab value="chunk">LLM chunk</Tabs.Tab>
						<Tabs.Tab value="help" ml="auto">
							Help
						</Tabs.Tab>
					</Tabs.List>
					{showDetachButton && (
						<Box pr="sm">
							<DetachButton
								open={previewOpen}
								onOpen={onOpenPreview}
								onClose={onClosePreview}
							/>
						</Box>
					)}
				</Group>

				<Tabs.Panel value="source" style={{ flex: 1, minHeight: 0 }}>
					<SourceEditor
						kind={kind}
						selected={selected}
						source={source}
						dirty={dirty}
						saving={saving}
						onSourceChange={onSourceChange}
						onSave={onSave}
					/>
				</Tabs.Panel>

				<Tabs.Panel value="preview" style={{ flex: 1, minHeight: 0 }}>
					<Preview source={source} viewId={selected} />
				</Tabs.Panel>

				<Tabs.Panel value="chunk" style={{ flex: 1, minHeight: 0 }}>
					<ChunkPanel kind={kind} source={source} selected={selected} />
				</Tabs.Panel>

				<Tabs.Panel value="help" style={{ flex: 1, minHeight: 0 }}>
					<HelpPanel />
				</Tabs.Panel>
			</Tabs>
		</Box>
	);
}

// DetachButton flips between two states: opening the preview
// window when none is around, and closing it when one is. The
// glyph is plain text (no icon set is wired into oosd yet) but
// the tooltip carries the verb.
function DetachButton({
	open,
	onOpen,
	onClose,
}: {
	open: boolean;
	onOpen: () => void;
	onClose: () => void;
}) {
	const tooltip = open
		? "Detached preview window is open — click to close"
		: "Open preview in a separate window";
	const handler = open ? onClose : onOpen;
	const color   = open ? "teal" : "gray";

	return (
		<Tooltip label={tooltip} position="bottom" withArrow>
			<ActionIcon
				variant={open ? "filled" : "subtle"}
				color={color}
				onClick={handler}
				aria-label={tooltip}
			>
				{open ? "⊟" : "⊡"}
			</ActionIcon>
		</Tooltip>
	);
}
