// AskResultPanel.tsx — Renders an Ask-mode answer as an editable tab.
//
// Opened when the user toggles "Editor" in the Footer before sending
// an Ask question. Instead of dropping the answer into a chat bubble,
// useChat calls addAskResult which spawns a tab carrying the question
// and the Markdown answer. This panel loads that Markdown into an
// MDXEditor so the user can refine, copy, save, or use it as a
// starting point for further work.
//
// Why MDXEditor and not MarkdownView: MarkdownView is a read-only
// renderer. Ask-to-tab exists precisely so the user can keep editing.
// PipelineRunPanel uses the same editor with the same plugin set, so
// the styling and toolbar match what the user already knows from the
// pipeline output.
//
// The panel is stateless across mounts on purpose: the answer is the
// answer, edits live only in the editor's internal state. If you
// later want to persist edits, push them back into the tab payload
// via an updateAskResultTab helper modelled on updatePipelineRunTab.

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
import { Box, Group, Stack, Text, ThemeIcon } from "@mantine/core";
import { IconMessageQuestion } from "@tabler/icons-react";
import { useRef } from "react";

import { MarkdownReadView } from "./MarkdownReadView";

interface Props {
	/** Original question text, shown as the panel header. */
	question: string;
	/** Markdown answer the LLM produced. Loaded into the editor at mount. */
	answer:   string;
	/** Model that produced the answer. Shown in the header as a hint. */
	model:    string;
}

export function AskResultPanel({ question, answer, model }: Props) {
	const editorRef = useRef<MDXEditorMethods>(null);

	return (
		<Box style={{ display: "flex", flexDirection: "column", height: "100%" }}>
			{/* Header — question + originating model. Mirrors the
			    PipelineRunPanel header layout so the two feel like
			    siblings in the results group. */}
			<Stack gap={4} p="md" style={{ borderBottom: "1px solid var(--mantine-color-gray-3)" }}>
				<Group gap="xs" wrap="nowrap" align="flex-start">
					<ThemeIcon size="md" radius="sm" variant="light" color="brand" style={{ flexShrink: 0, marginTop: 4 }}>
						<IconMessageQuestion size={18} />
					</ThemeIcon>
					{/* Question routed through the same read-only Markdown
					    renderer used in chat bubbles so it looks identical to
					    what the composer showed before sending — no raw **,
					    ==, or escape syntax leaking through. */}
					<Box style={{ flex: 1, minWidth: 0, fontWeight: 600 }}>
						<MarkdownReadView source={question} />
					</Box>
				</Group>
				<Text size="xs" c="dimmed" pl={36}>
					Ask · {model}
				</Text>
			</Stack>

			{/* Editor — same MDXEditor + plugins as PipelineRunPanel so the
			    toolbar matches. The user can format, link, table, undo/redo;
			    Markdown shortcuts work (## for headings, etc.). */}
			<Box style={{ flex: 1, overflow: "auto", minHeight: 0 }}>
				<MDXEditor
					ref={editorRef}
					markdown={answer}
					plugins={[
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
					]}
				/>
			</Box>
		</Box>
	);
}
