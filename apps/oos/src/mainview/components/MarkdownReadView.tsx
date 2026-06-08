// MarkdownReadView.tsx — Read-only Markdown renderer using MDXEditor.
//
// Why this exists alongside MarkdownView: ComposerRichText writes
// Markdown that MDXEditor's parser produces and consumes round-trip
// safely. Rendering the same string through a different engine
// (remark-gfm-via-MarkdownView) makes user input and assistant
// output look subtly different and lets edge syntax slip through
// raw — ==highlight==, escaped brackets, math constructs. Using the
// same MDXEditor instance read-only at render sites guarantees that
// what the composer showed before sending is exactly what the user
// sees afterwards in the bubble or in the Ask-tab header.
//
// Used by:
//   - Bubble.tsx for user and assistant messages
//   - AskResultPanel for the question header
//
// Styling is intentionally minimal: no border, no min/max height, no
// internal scroll, transparent background. The host card supplies
// padding and width; this component only renders content.

import {
	MDXEditor,
	headingsPlugin,
	listsPlugin,
	quotePlugin,
	thematicBreakPlugin,
	tablePlugin,
	linkPlugin,
	markdownShortcutPlugin,
} from "@mdxeditor/editor";
import { Box } from "@mantine/core";

interface Props {
	/** Markdown source to render. */
	source: string;
}

export function MarkdownReadView({ source }: Props) {
	return (
		<Box
			className="markdown-read-view"
			style={{
				width: "100%",
				// Background stays transparent so the host card's tint
				// (e.g. brand-0 for user bubbles) shows through.
				background: "transparent",
			}}
		>
			<MDXEditor
				markdown={source}
				readOnly
				plugins={[
					// No toolbarPlugin — read-only views show no toolbar.
					headingsPlugin(),
					listsPlugin(),
					quotePlugin(),
					thematicBreakPlugin(),
					tablePlugin(),
					linkPlugin(),
					markdownShortcutPlugin(),
				]}
			/>
		</Box>
	);
}
