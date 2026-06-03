// containers.tsx — Mantine renderers for layout containers.
//
// Sections, Stacks, Rows, Grids, Tabs, Accordion, Card. Each takes a
// ContainerProps (mods + body) and dispatches the body through the
// shared `<BodyElementRenderer>` exported from `dispatch.tsx`.

import {
	Accordion,
	Card,
	Divider,
	Grid,
	Group,
	Paper,
	Stack,
	Tabs,
	Text,
	Title,
} from "@mantine/core";
import type { ReactElement } from "react";
import type {
	AccordionDef,
	CardDef,
	GridDef,
	LayoutModDef,
	RowDef,
	SectionDef,
	StackDef,
	TabsDef,
} from "oos-dsls-ts/types";

import { BodyElementRenderer } from "./dispatch";

// ─── Generic helpers ─────────────────────────────────────────────────

interface SpacingResolved {
	gap: string | number | undefined;
	padding: string | number | undefined;
}

/**
 * Reduce a heterogenous mod array to the spacing values Mantine
 * cares about. Token tokens are forwarded as Mantine's named sizes
 * ("xs"/"sm"/...); numeric scales are passed through as-is.
 */
export function resolveSpacing(mods: LayoutModDef[]): SpacingResolved {
	let gap: SpacingResolved["gap"];
	let padding: SpacingResolved["padding"];
	for (const m of mods) {
		switch (m.kind) {
			case "gap":
				gap = m.value.kind === "token" ? m.value.value : m.value.value;
				break;
			case "padding":
				if (m.prop === "p") {
					padding = m.value.kind === "token" ? m.value.value : m.value.value;
				}
				break;
		}
	}
	return { gap, padding };
}

// ─── Containers ──────────────────────────────────────────────────────

export function SectionRenderer({ def }: { def: SectionDef }): ReactElement {
	const { gap, padding } = resolveSpacing(def.mods);
	return (
		<Paper shadow="xs" p={padding ?? "md"} withBorder>
			{def.caption && (
				<Title order={4} mb="sm">
					{def.caption}
				</Title>
			)}
			<Stack gap={gap ?? "sm"}>
				{def.body.map((el, i) => (
					<BodyElementRenderer key={i} def={el} />
				))}
			</Stack>
		</Paper>
	);
}

export function StackRenderer({ def }: { def: StackDef }): ReactElement {
	const { gap } = resolveSpacing(def.mods);
	return (
		<Stack gap={gap ?? "sm"}>
			{def.body.map((el, i) => (
				<BodyElementRenderer key={i} def={el} />
			))}
		</Stack>
	);
}

export function RowRenderer({ def }: { def: RowDef }): ReactElement {
	const { gap } = resolveSpacing(def.mods);
	return (
		<Group gap={gap ?? "sm"} grow>
			{def.body.map((el, i) => (
				<BodyElementRenderer key={i} def={el} />
			))}
		</Group>
	);
}

export function GridRenderer({ def }: { def: GridDef }): ReactElement {
	const { gap } = resolveSpacing(def.mods);
	const span = Math.max(1, Math.floor(12 / def.cols));
	return (
		<Grid gap={gap ?? "sm"}>
			{def.body.map((el, i) => (
				<Grid.Col key={i} span={span}>
					<BodyElementRenderer def={el} />
				</Grid.Col>
			))}
		</Grid>
	);
}

export function TabsRenderer({ def }: { def: TabsDef }): ReactElement {
	const first = def.tabs[0]?.caption ?? "";
	return (
		<Tabs defaultValue={first}>
			<Tabs.List>
				{def.tabs.map((t) => (
					<Tabs.Tab key={t.caption} value={t.caption}>
						{t.caption}
					</Tabs.Tab>
				))}
			</Tabs.List>
			{def.tabs.map((t) => (
				<Tabs.Panel key={t.caption} value={t.caption} pt="sm">
					<Stack gap="sm">
						{t.body.map((el, i) => (
							<BodyElementRenderer key={i} def={el} />
						))}
					</Stack>
				</Tabs.Panel>
			))}
		</Tabs>
	);
}

export function AccordionRenderer({ def }: { def: AccordionDef }): ReactElement {
	const initial = def.items.find((it) => it.open)?.caption;
	return (
		<Accordion defaultValue={initial}>
			{def.items.map((it) => (
				<Accordion.Item key={it.caption} value={it.caption}>
					<Accordion.Control>{it.caption}</Accordion.Control>
					<Accordion.Panel>
						<Stack gap="sm">
							{it.body.map((el, i) => (
								<BodyElementRenderer key={i} def={el} />
							))}
						</Stack>
					</Accordion.Panel>
				</Accordion.Item>
			))}
		</Accordion>
	);
}

export function CardRenderer({ def }: { def: CardDef }): ReactElement {
	return (
		<Card shadow="sm" padding="md" withBorder>
			{def.caption && (
				<Card.Section inheritPadding py="xs">
					<Text fw={600}>{def.caption}</Text>
				</Card.Section>
			)}
			<Stack gap="sm" mt={def.caption ? "sm" : 0}>
				{def.body.map((el, i) => (
					<BodyElementRenderer key={i} def={el} />
				))}
			</Stack>
		</Card>
	);
}

export function DividerRenderer(): ReactElement {
	return <Divider my="sm" />;
}

export function SeparatorRenderer(): ReactElement {
	return <Divider variant="dashed" my="sm" />;
}
