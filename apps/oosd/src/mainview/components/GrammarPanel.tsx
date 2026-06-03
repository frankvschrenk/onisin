// GrammarPanel.tsx — read-only viewer for the Langium grammar sources.
//
// Layout: tab strip (Domain / View / Event Schema) above a full-height
// Monaco editor in read-only mode. Useful for contributors who want to
// understand the DSL grammar without digging through the package tree.
//
// The grammar files live in packages/oos-dsls-ts/grammar/ and are read
// on demand via the bun-side loadGrammarSource RPC handler.

import { useEffect, useRef, useState } from "react";
import { Alert, Box, Loader, Tabs, Text } from "@mantine/core";
import { IconAlertCircle } from "@tabler/icons-react";
import Editor, { type OnMount } from "@monaco-editor/react";

import { rpc } from "../rpc";
import {
	LANGIUM_LANGUAGE_ID,
	registerOnisinLanguages,
} from "../../lang/monaco/register";

// ─── Grammar tab descriptors ──────────────────────────────────────────

type GrammarTab = {
	id:    string;
	label: string;
};

const GRAMMAR_TABS: GrammarTab[] = [
	{ id: "domain",        label: "Domain" },
	{ id: "view",          label: "View" },
	{ id: "event-schema",  label: "Event Schema" },
];

// ─── Component ───────────────────────────────────────────────────────

export function GrammarPanel({ disabled }: { disabled: boolean }) {
	const [activeTab, setActiveTab] = useState<string>(GRAMMAR_TABS[0].id);
	const [source,    setSource]    = useState<string | null>(null);
	const [loading,   setLoading]   = useState(false);
	const [error,     setError]     = useState<string | null>(null);

	// Cache already-fetched sources to avoid re-fetching on tab switch.
	const cache = useRef<Record<string, string>>({});

	// ── Load grammar source ─────────────────────────────────────────
	useEffect(() => {
		if (disabled) return;

		const cached = cache.current[activeTab];
		if (cached !== undefined) {
			setSource(cached);
			setError(null);
			return;
		}

		setLoading(true);
		setSource(null);
		setError(null);

		rpc.loadGrammarSource({ kind: activeTab })
			.then((res) => {
				if (res.error) {
					setError(res.error);
					return;
				}
				const text = res.source ?? "";
				cache.current[activeTab] = text;
				setSource(text);
			})
			.catch((err: unknown) => {
				setError(err instanceof Error ? err.message : String(err));
			})
			.finally(() => setLoading(false));
	}, [activeTab, disabled]);

	// ── Monaco setup ────────────────────────────────────────────────
	const onMount: OnMount = (editor) => {
		registerOnisinLanguages();
		// Prevent accidental edits — content is package source, not DB data.
		editor.updateOptions({ readOnly: true });
	};

	// ── Render ──────────────────────────────────────────────────────
	return (
		<Box style={{ display: "flex", flexDirection: "column", height: "100%" }}>

			{/* ── Header ── */}
			<Box
				px="md"
				py="xs"
				style={{
					borderBottom: "1px solid var(--mantine-color-default-border)",
					flexShrink:   0,
				}}
			>
				<Text size="sm" fw={500} mb={6}>Grammar</Text>
				<Text size="xs" c="dimmed">
					Langium grammar source — read-only reference for DSL contributors.
				</Text>
			</Box>

			{/* ── Tab strip ── */}
			<Tabs
				value={activeTab}
				onChange={(v) => { if (v) setActiveTab(v); }}
				style={{ flexShrink: 0 }}
			>
				<Tabs.List>
					{GRAMMAR_TABS.map((t) => (
						<Tabs.Tab key={t.id} value={t.id}>
							{t.label}
						</Tabs.Tab>
					))}
				</Tabs.List>
			</Tabs>

			{/* ── Content ── */}
			<Box style={{ flex: 1, overflow: "hidden", position: "relative" }}>
				{loading && (
					<Box
						style={{
							position:       "absolute",
							inset:          0,
							display:        "flex",
							alignItems:     "center",
							justifyContent: "center",
							zIndex:         10,
						}}
					>
						<Loader size="sm" />
					</Box>
				)}

				{error && !loading && (
					<Box p="md">
						<Alert
							icon={<IconAlertCircle size={16} />}
							color="red"
							title="Could not load grammar"
						>
							{error}
						</Alert>
					</Box>
				)}

				{!error && (
					<Editor
						height="100%"
						language={LANGIUM_LANGUAGE_ID}
						value={source ?? ""}
						onMount={onMount}
						options={{
							readOnly:            true,
							minimap:             { enabled: false },
							fontSize:            12,
							wordWrap:            "on",
							scrollBeyondLastLine: false,
							automaticLayout:     true,
							renderLineHighlight: "none",
							cursorStyle:         "line",
						}}
					/>
				)}
			</Box>
		</Box>
	);
}
