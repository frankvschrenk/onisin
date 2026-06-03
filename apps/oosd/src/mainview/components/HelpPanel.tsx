// HelpPanel.tsx — Right-pane help tab.
//
// Renders Markdown documents shipped under oosd/docs/help/ using
// react-markdown with Mantine components as element overrides. That
// keeps the help content visually consistent with the rest of the
// editor — headings use Mantine's Title scale, links use Anchor,
// tables use Mantine's Table, code uses Code.
//
// The Markdown bodies are bundled at build time via
// scripts/build-help.ts, which emits help-content.generated.ts.
// Importing the strings directly avoids depending on Electrobun's
// `views://` custom scheme to serve .md files; an early version
// fetched them at runtime and the scheme handler refused to deliver
// them on some configurations.
//
// To add a topic: drop a `<id>.md` into oosd/docs/help/ and add a
// matching entry to TOPICS below. The next dev build (or
// `bun run help:build`) regenerates help-content.generated.ts and
// the panel picks the new topic up automatically.

import { useMemo, useState } from "react";
import {
	Alert,
	Anchor,
	Box,
	Code,
	Divider,
	NavLink,
	ScrollArea,
	Stack,
	Table,
	Text,
	Title,
} from "@mantine/core";
import ReactMarkdown, { type Components } from "react-markdown";
import remarkGfm from "remark-gfm";

import { helpContent } from "./help/help-content.generated";

// One entry per shipped Markdown file. Order is the order shown in
// the navigation column.
type Topic = {
	id:    string;
	title: string;
};

const TOPICS: Topic[] = [
	{ id: "quickstart",  title: "Quick Start"       },
	{ id: "domain",      title: "Domain DSL"        },
	{ id: "view",        title: "View DSL"          },
	{ id: "widgets",     title: "Widget Reference"  },
	{ id: "filters",     title: "Filter Syntax"     },
	{ id: "permissions", title: "Permissions"       },
];

const DEFAULT_TOPIC = TOPICS[0]!.id;

export function HelpPanel() {
	const [topicId, setTopicId] = useState<string>(DEFAULT_TOPIC);
	const topic = useMemo(
		() => TOPICS.find((t) => t.id === topicId) ?? TOPICS[0]!,
		[topicId],
	);

	// In-page navigation: when a Markdown link points at another
	// topic by filename (e.g. ./widgets.md or widgets.md), switch
	// the active topic instead of opening the link in a browser.
	function navigate(id: string) {
		if (TOPICS.some((t) => t.id === id)) setTopicId(id);
	}

	return (
		<Box style={{ display: "flex", height: "100%", minHeight: 0 }}>
			<Box
				style={{
					width: 220,
					borderRight: "1px solid var(--mantine-color-default-border)",
					padding: "var(--mantine-spacing-sm)",
					overflowY: "auto",
				}}
			>
				<Text size="xs" c="dimmed" mb="xs" tt="uppercase" fw={600}>
					Topics
				</Text>
				<Stack gap={2}>
					{TOPICS.map((t) => (
						<NavLink
							key={t.id}
							label={t.title}
							active={t.id === topicId}
							onClick={() => setTopicId(t.id)}
						/>
					))}
				</Stack>
			</Box>
			<Box style={{ flex: 1, minWidth: 0 }}>
				<HelpDocument
					id={topic.id}
					onNavigate={navigate}
				/>
			</Box>
		</Box>
	);
}

// HelpDocument renders one help topic's bundled Markdown. The
// component's hooks run in a fixed order on every render — the
// missing-content path returns *after* the hook so React's
// rules-of-hooks invariant holds even when the user navigates
// to a stale topic id.
function HelpDocument({
	id,
	onNavigate,
}: {
	id: string;
	onNavigate: (id: string) => void;
}) {
	const components = useMemo(
		() => makeComponents(onNavigate),
		[onNavigate],
	);
	const md = helpContent[id];

	if (md === undefined) {
		return (
			<Stack p="md">
				<Alert color="red" title={`Unknown help topic: ${id}`}>
					No bundled content for <Code>{id}.md</Code>. Did you forget to
					run <Code>bun run help:build</Code>?
				</Alert>
			</Stack>
		);
	}

	return (
		<ScrollArea style={{ height: "100%" }}>
			<Box p="md" style={{ maxWidth: 860 }}>
				<ReactMarkdown remarkPlugins={[remarkGfm]} components={components}>
					{md}
				</ReactMarkdown>
			</Box>
		</ScrollArea>
	);
}

// makeComponents builds the Markdown→Mantine element map. It is a
// factory because the anchor handler closes over onNavigate; the
// rest of the components are pure.
function makeComponents(onNavigate: (id: string) => void): Components {
	return {
		h1: ({ children }) => (
			<Title order={2} mt="lg" mb="sm">
				{children}
			</Title>
		),
		h2: ({ children }) => (
			<Title order={3} mt="lg" mb="sm">
				{children}
			</Title>
		),
		h3: ({ children }) => (
			<Title order={4} mt="md" mb="xs">
				{children}
			</Title>
		),
		h4: ({ children }) => (
			<Title order={5} mt="md" mb="xs">
				{children}
			</Title>
		),
		p: ({ children }) => (
			<Text size="sm" mb="sm" style={{ lineHeight: 1.55 }}>
				{children}
			</Text>
		),
		a: ({ href, children }) => {
			const target = resolveTopicHref(href);
			if (target !== null) {
				return (
					<Anchor
						href="#"
						onClick={(e) => {
							e.preventDefault();
							onNavigate(target);
						}}
					>
						{children}
					</Anchor>
				);
			}
			return (
				<Anchor href={href} target="_blank" rel="noreferrer">
					{children}
				</Anchor>
			);
		},
		hr: () => <Divider my="md" />,
		ul: ({ children }) => (
			<Box component="ul" style={{ paddingLeft: 20, marginBottom: 12 }}>
				{children}
			</Box>
		),
		ol: ({ children }) => (
			<Box component="ol" style={{ paddingLeft: 20, marginBottom: 12 }}>
				{children}
			</Box>
		),
		li: ({ children }) => (
			<Text component="li" size="sm" style={{ lineHeight: 1.55 }}>
				{children}
			</Text>
		),
		strong: ({ children }) => <Text component="strong" fw={700}>{children}</Text>,
		em: ({ children }) => <Text component="em" fs="italic">{children}</Text>,
		code: ({ children }) => <Code>{children}</Code>,
		pre: ({ children }) => (
			<Box
				component="pre"
				style={{
					background: "var(--mantine-color-gray-0)",
					border: "1px solid var(--mantine-color-default-border)",
					borderRadius: 4,
					padding: "var(--mantine-spacing-sm)",
					overflowX: "auto",
					fontSize: 12,
					marginBottom: 12,
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
					margin: "0 0 12px 0",
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

// resolveTopicHref returns the topic id for an href that points at
// another help document, or null if the href is something else (an
// external URL, an anchor, a fragment).
//
// Recognised shapes (case-insensitive):
//
//   - "widgets.md"
//   - "./widgets.md"
//   - "widgets"
//   - "widgets.md#filter-row"   (the fragment is dropped)
//
// Anything with a scheme (http://, https://, mailto:) and any path
// containing a slash other than the leading "./" is treated as
// external.
function resolveTopicHref(href: string | undefined): string | null {
	if (!href) return null;
	if (/^[a-z]+:/i.test(href))           return null;
	const stripped = href.replace(/^\.\//, "");
	if (stripped.includes("/"))           return null;
	const noFragment = stripped.split("#")[0] ?? "";
	const candidate  = noFragment.replace(/\.md$/i, "");
	return candidate || null;
}
