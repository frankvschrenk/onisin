// TranslatePanel.tsx — Side-by-side Markdown translator as a content tab.
//
// Opened from the burger menu ("Translate"). The chat composer is the
// wrong home for translation: a translation is not a dialogue turn but
// the same content rendered in two languages at once. This panel puts
// the source on the left and the target on the right, both editable,
// with a language pair and a translate button.
//
// Why two MDXEditors and not Monaco-source + a Markdown renderer:
// render-engine symmetry. The composer and every read site already use
// MDXEditor; re-rendering its Markdown through a different engine is
// exactly what let ==highlight==, escaped brackets and friends leak
// through raw before. Keeping author-engine == reader-engine here means
// a translated document looks the same wherever it later lands.
//
// Markdown preservation itself is a backend concern: translateTurn
// instructs the model to translate prose only and leave every Markdown
// construct intact. This panel just moves text in and out of the two
// editors and renders the result.

import { useRef, useState } from "react";
import {
	MDXEditor,
	BoldItalicUnderlineToggles,
	UndoRedo,
	BlockTypeSelect,
	CreateLink,
	InsertTable,
	ListsToggle,
	toolbarPlugin,
	headingsPlugin,
	listsPlugin,
	quotePlugin,
	thematicBreakPlugin,
	tablePlugin,
	markdownShortcutPlugin,
	linkPlugin,
	linkDialogPlugin,
	type MDXEditorMethods,
} from "@mdxeditor/editor";
import { ActionIcon, Alert, Box, Button, Group, Select, Tooltip } from "@mantine/core";
import { IconAlertCircle, IconArrowsLeftRight, IconLanguage } from "@tabler/icons-react";

import { rpc } from "../rpc";
import { useAppSettings } from "../store/settings";
import { loadFinetuning } from "../store/finetuning";

// Sentinel for "let the model detect the source language". Mantine
// Select needs a concrete value, so AUTO stands in and is mapped to an
// empty sourceLang, which translateTurn reads as "unspecified".
const AUTO = "auto";

const LANGS = ["Deutsch", "English", "Français", "Español", "Italiano", "Português"];

const SOURCE_OPTIONS = [
	{ value: AUTO, label: "Automatisch" },
	...LANGS.map((l) => ({ value: l, label: l })),
];
const TARGET_OPTIONS = LANGS.map((l) => ({ value: l, label: l }));

/**
 * buildPlugins returns a fresh plugin array. MDXEditor plugins are
 * stateful per instance, so the two panes must not share one array —
 * each call mints its own. The set mirrors AskResultPanel so the
 * toolbar and rendering match the rest of the app.
 */
function buildPlugins() {
	return [
		toolbarPlugin({
			toolbarContents: () => (
				<>
					<UndoRedo />
					<BlockTypeSelect />
					<BoldItalicUnderlineToggles />
					<ListsToggle />
					<CreateLink />
					<InsertTable />
				</>
			),
		}),
		headingsPlugin(),
		listsPlugin(),
		quotePlugin(),
		thematicBreakPlugin(),
		tablePlugin(),
		linkPlugin(),
		linkDialogPlugin(),
		markdownShortcutPlugin(),
	];
}

/**
 * TranslatePanel is the body of the Translate tab. Stateless across
 * mounts on purpose — the text lives in the two editors' internal
 * state; closing the tab discards it, like a scratchpad.
 */
export function TranslatePanel() {
	const { settings, loaded } = useAppSettings();

	const sourceRef = useRef<MDXEditorMethods>(null);
	const targetRef = useRef<MDXEditorMethods>(null);

	const [sourceLang, setSourceLang] = useState<string>("Deutsch");
	const [targetLang, setTargetLang] = useState<string>("English");
	const [busy,       setBusy]       = useState(false);
	const [error,      setError]      = useState<string | null>(null);

	async function translate() {
		const source = sourceRef.current?.getMarkdown() ?? "";
		if (!source.trim() || busy) return;

		setBusy(true);
		setError(null);
		try {
			const tuning = await loadFinetuning(settings.llmModel);
			const res = await rpc.translateTurn({
				turnId: `translate_${Date.now()}`,
				settings: {
					llmBaseUrl: settings.llmBaseUrl,
					natsUrl:    settings.natsUrl,
					llmApiKey:  settings.llmApiKey,
					llmModel:   settings.llmModel,
				},
				tuning,
				source,
				// AUTO is surfaced to the model as an empty language so the
				// prompt's "from the source language" fallback applies.
				sourceLang: sourceLang === AUTO ? "" : sourceLang,
				targetLang,
			});
			if (res.error) {
				setError(res.error);
			} else {
				targetRef.current?.setMarkdown(res.text);
			}
		} catch (e) {
			setError(e instanceof Error ? e.message : String(e));
		} finally {
			setBusy(false);
		}
	}

	// Swap is only meaningful with a concrete source language; with
	// auto-detect there is nothing to put on the other side. It swaps
	// both the language pair and the two texts, so a quick reverse
	// translation is one click away.
	function swap() {
		if (sourceLang === AUTO) return;
		const s = sourceRef.current?.getMarkdown() ?? "";
		const t = targetRef.current?.getMarkdown() ?? "";
		sourceRef.current?.setMarkdown(t);
		targetRef.current?.setMarkdown(s);
		setSourceLang(targetLang);
		setTargetLang(sourceLang);
	}

	return (
		<Box style={{ display: "flex", flexDirection: "column", height: "100%" }}>
			{/* Toolbar — language pair + translate trigger. */}
			<Group gap="sm" p="sm" wrap="nowrap" style={{ borderBottom: "1px solid var(--mantine-color-gray-3)" }}>
				<Select
					aria-label="Source language"
					data={SOURCE_OPTIONS}
					value={sourceLang}
					onChange={(v) => v && setSourceLang(v)}
					w={160}
					comboboxProps={{ withinPortal: true }}
				/>
				<Tooltip label="Sprachen tauschen" withArrow>
					<ActionIcon
						variant="default"
						aria-label="Sprachen tauschen"
						onClick={swap}
						disabled={sourceLang === AUTO}
					>
						<IconArrowsLeftRight size={18} />
					</ActionIcon>
				</Tooltip>
				<Select
					aria-label="Target language"
					data={TARGET_OPTIONS}
					value={targetLang}
					onChange={(v) => v && setTargetLang(v)}
					w={160}
					comboboxProps={{ withinPortal: true }}
				/>
				<Button
					ml="auto"
					leftSection={<IconLanguage size={16} />}
					onClick={translate}
					loading={busy}
					disabled={!loaded}
				>
					Übersetzen
				</Button>
			</Group>

			{error && (
				<Alert
					color="red"
					icon={<IconAlertCircle size={16} />}
					mx="sm"
					mt="sm"
					title="Translation failed"
				>
					{error}
				</Alert>
			)}

			{/* Two panes — source left, target right, both editable. */}
			<Box style={{ flex: 1, minHeight: 0, display: "grid", gridTemplateColumns: "1fr 1fr" }}>
				<Box style={{ minWidth: 0, overflow: "auto", borderRight: "1px solid var(--mantine-color-gray-3)" }}>
					<MDXEditor ref={sourceRef} markdown="" plugins={buildPlugins()} />
				</Box>
				<Box style={{ minWidth: 0, overflow: "auto" }}>
					<MDXEditor ref={targetRef} markdown="" plugins={buildPlugins()} />
				</Box>
			</Box>
		</Box>
	);
}
