// DocsPanel.tsx — Renders one documentation topic as the active
// tab content.
//
// Each documentation topic is now its own tab in the Docs group, so
// the panel itself is just a Markdown viewer for one body. The topic
// id arrives via props (set by TabContent's dispatch). Cross-topic
// links stay in-app: clicking [Settings](./settings.md) inside a
// rendered doc switches the active tab to the settings doc rather
// than opening the link in a browser.

import { useMemo } from "react";
import {
	Alert,
	Anchor,
	Box,
	Code,
	Divider,
	ScrollArea,
	Stack,
	Table,
	Text,
	Title,
} from "@mantine/core";
import ReactMarkdown, { type Components } from "react-markdown";
import remarkGfm from "remark-gfm";

import { docsContent } from "../docs/docs-content.generated";
import { setActive, useTabs } from "../store/tabs";

interface DocsPanelProps {
	topicId: string;
}

export function DocsPanel({ topicId }: DocsPanelProps) {
	const { groups } = useTabs();
	const components = useMemo(
		() => makeComponents((id) => navigateToDocTab(groups, id)),
		[groups],
	);
	const md = docsContent[topicId];

	if (md === undefined) {
		return (
			<Stack p="md">
				<Alert color="red" title={`Unknown topic: ${topicId}`}>
					No bundled content for <Code>{topicId}.md</Code>. Did you
					forget to run <Code>bun run docs:build</Code>?
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

/**
 * navigateToDocTab activates the doc tab whose payload references
 * the given topic id. No-op if no such tab exists in the current
 * snapshot — that means the user has navigated away from the docs
 * group, and re-opening the link would mean re-launching the whole
 * group, which is more disruptive than the user expects.
 */
function navigateToDocTab(
	groups: ReturnType<typeof useTabs>["groups"],
	topicId: string,
): void {
	for (const g of groups) {
		for (const t of g.tabs) {
			if (t.payload.kind === "doc" && t.payload.topicId === topicId) {
				setActive(t.id);
				return;
			}
		}
	}
}

// makeComponents builds the Markdown→Mantine element map. The
// anchor handler closes over a navigate callback so cross-topic
// links stay inside the panel; the rest is pure styling.
function makeComponents(navigate: (id: string) => void): Components {
	return {
		h1: ({ children }) => <Title order={2} mt="lg" mb="sm">{children}</Title>,
		h2: ({ children }) => <Title order={3} mt="lg" mb="sm">{children}</Title>,
		h3: ({ children }) => <Title order={4} mt="md" mb="xs">{children}</Title>,
		h4: ({ children }) => <Title order={5} mt="md" mb="xs">{children}</Title>,
		p:  ({ children }) => (
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
							navigate(target);
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

/**
 * resolveTopicHref returns the topic id for an href that points at
 * another doc page, or null if the href is something else.
 */
function resolveTopicHref(href: string | undefined): string | null {
	if (!href) return null;
	if (/^[a-z]+:/i.test(href)) return null;
	const stripped = href.replace(/^\.\//, "");
	if (stripped.includes("/")) return null;
	const noFragment = stripped.split("#")[0] ?? "";
	const candidate  = noFragment.replace(/\.md$/i, "");
	return candidate || null;
}
