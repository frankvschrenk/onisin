// MarkdownView.tsx — Read-mode Markdown renderer used across oos.
//
// One viewer, one styling pass: every place that displays Markdown
// (chat bubbles, event answers, pipeline chunks, activity details,
// future panels) routes through this component so the look stays
// consistent and tweaks happen in a single file.
//
// Styling maps Markdown elements onto Mantine primitives — `Title`
// for headings, `Text` for paragraphs and list items, `Anchor` for
// links, `Code` and a styled `pre` for code, `Table` for GFM tables.
// GFM (tables, task lists, strikethrough) is on by default via
// remark-gfm.
//
// Size variants:
//
//   "sm" (default) — body copy density. Used by chat bubbles,
//                    activity panels, event results.
//   "xs"           — compact density for inline auditing surfaces
//                    such as pipeline-step chunks.
//
// DocsPanel keeps its own renderer because it needs in-app navigation
// for cross-topic links; that concern does not belong here.

import { useMemo } from "react";
import {
	Anchor,
	Box,
	Code,
	Divider,
	Table,
	Text,
	Title,
} from "@mantine/core";
import ReactMarkdown, { type Components } from "react-markdown";
import remarkGfm from "remark-gfm";

export type MarkdownSize = "sm" | "xs";

interface MarkdownViewProps {
	/** Raw Markdown source to render. */
	source: string;
	/** Visual density. Defaults to "sm". */
	size?: MarkdownSize;
}

export function MarkdownView({ source, size = "sm" }: MarkdownViewProps) {
	const components = useMemo(() => buildComponents(size), [size]);
	return (
		<ReactMarkdown remarkPlugins={[remarkGfm]} components={components}>
			{source}
		</ReactMarkdown>
	);
}

// ─── Renderer map ───────────────────────────────────────────────────

/**
 * buildComponents returns the Markdown→Mantine element map for the
 * requested size. The map is memoised by the caller so React does
 * not see a fresh `components` object on every render.
 */
function buildComponents(size: MarkdownSize): Components {
	const textSize = size;          // Mantine accepts "xs" | "sm" directly
	const bodyLineHeight = size === "xs" ? 1.5 : 1.55;
	const blockGap       = size === "xs" ? 8   : 12;
	const codeFontSize   = size === "xs" ? 11  : 12;
	const headingTopGap  = size === "xs" ? "sm" : "md";

	return {
		h1: ({ children }) => (
			<Title order={size === "xs" ? 4 : 3} mt={headingTopGap} mb="xs">
				{children}
			</Title>
		),
		h2: ({ children }) => (
			<Title order={size === "xs" ? 5 : 4} mt={headingTopGap} mb="xs">
				{children}
			</Title>
		),
		h3: ({ children }) => (
			<Title order={size === "xs" ? 6 : 5} mt={headingTopGap} mb="xs">
				{children}
			</Title>
		),
		h4: ({ children }) => (
			<Title order={6} mt={headingTopGap} mb="xs">
				{children}
			</Title>
		),
		h5: ({ children }) => (
			<Title order={6} mt={headingTopGap} mb="xs">
				{children}
			</Title>
		),
		h6: ({ children }) => (
			<Title order={6} mt={headingTopGap} mb="xs">
				{children}
			</Title>
		),

		p: ({ children }) => (
			<Text size={textSize} style={{ lineHeight: bodyLineHeight, marginBottom: blockGap }}>
				{children}
			</Text>
		),

		a: ({ href, children }) => (
			<Anchor href={href} target="_blank" rel="noreferrer">
				{children}
			</Anchor>
		),

		hr: () => <Divider my={size === "xs" ? "xs" : "md"} />,

		ul: ({ children }) => (
			<Box
				component="ul"
				style={{ paddingLeft: 20, marginBottom: blockGap, marginTop: 0 }}
			>
				{children}
			</Box>
		),
		ol: ({ children }) => (
			<Box
				component="ol"
				style={{ paddingLeft: 20, marginBottom: blockGap, marginTop: 0 }}
			>
				{children}
			</Box>
		),
		li: ({ children }) => (
			<Text component="li" size={textSize} style={{ lineHeight: bodyLineHeight }}>
				{children}
			</Text>
		),

		strong: ({ children }) => <Text component="strong" fw={700}>{children}</Text>,
		em:     ({ children }) => <Text component="em" fs="italic">{children}</Text>,

		code:   ({ children }) => <Code>{children}</Code>,
		pre: ({ children }) => (
			<Box
				component="pre"
				style={{
					background: "light-dark(var(--mantine-color-gray-0), var(--mantine-color-dark-7))",
					border: "1px solid var(--mantine-color-default-border)",
					borderRadius: 4,
					padding: "var(--mantine-spacing-sm)",
					overflowX: "auto",
					fontSize: codeFontSize,
					marginBottom: blockGap,
					marginTop: 0,
				}}
			>
				{children}
			</Box>
		),

		blockquote: ({ children }) => (
			<Box
				component="blockquote"
				style={{
					borderLeft: "3px solid var(--mantine-color-default-border)",
					margin: `0 0 ${blockGap}px 0`,
					padding: "4px 12px",
					color: "var(--mantine-color-dimmed)",
				}}
			>
				{children}
			</Box>
		),

		table: ({ children }) => (
			<Table withTableBorder withColumnBorders striped my="sm">
				{children}
			</Table>
		),
		thead: ({ children }) => <Table.Thead>{children}</Table.Thead>,
		tbody: ({ children }) => <Table.Tbody>{children}</Table.Tbody>,
		tr:    ({ children }) => <Table.Tr>{children}</Table.Tr>,
		th:    ({ children }) => <Table.Th>{children}</Table.Th>,
		td:    ({ children }) => <Table.Td>{children}</Table.Td>,
	};
}
