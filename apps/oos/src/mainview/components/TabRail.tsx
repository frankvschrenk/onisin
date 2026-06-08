// TabRail.tsx — Vertical tab strip, grouped by tab group.
//
// Each tab is a row tall enough to hold its full title plus an
// optional subtitle, so multi-word names like "Agent design" or
// "Keyboard shortcuts" stay readable instead of being abbreviated
// to initials. Active tab gets an indigo left border. Hovering a
// tab reveals the close button on the right edge.
//
// Groups are separated by a thin divider so the eye can tell
// "documentation" tabs apart from "results" tabs without an
// explicit header.
//
// Welcome tabs are not closeable — they materialise automatically
// when nothing else is open, so the close button would only flash
// the same group back into existence on the next render.

import { ActionIcon, Box, Stack, Text, Tooltip, UnstyledButton } from "@mantine/core";
import { IconX } from "@tabler/icons-react";

import type { TabGroup, TabRecord } from "../store/tabs";

interface TabRailProps {
	groups:     TabGroup[];
	activeId:   string | null;
	onActivate: (id: string) => void;
	onClose:    (id: string) => void;
}

export function TabRail({ groups, activeId, onActivate, onClose }: TabRailProps) {
	return (
		<Box
			style={{
				background: "light-dark(var(--mantine-color-gray-0), var(--mantine-color-dark-7))",
				borderRight: "1px solid var(--mantine-color-gray-3)",
				display: "flex",
				flexDirection: "column",
				minHeight: 0,
				overflowY: "auto",
			}}
		>
			<Stack gap={0} p={4}>
				{groups.map((group, i) => (
					<Box key={group.id}>
						{i > 0 && (
							<Box
								my={4}
								style={{
									height: 1,
									background: "var(--mantine-color-gray-3)",
								}}
							/>
						)}
						<Stack gap={2}>
							{group.tabs.map((tab) => (
								<TabButton
									key={tab.id}
									tab={tab}
									group={group}
									active={tab.id === activeId}
									onActivate={() => onActivate(tab.id)}
									onClose={() => onClose(tab.id)}
								/>
							))}
						</Stack>
					</Box>
				))}
			</Stack>
		</Box>
	);
}

interface TabButtonProps {
	tab:        TabRecord;
	group:      TabGroup;
	active:     boolean;
	onActivate: () => void;
	onClose:    () => void;
}

function TabButton({ tab, group, active, onActivate, onClose }: TabButtonProps) {
	const tooltip = tab.subtitle ? `${tab.title} · ${tab.subtitle}` : tab.title;
	const closeable = group.kind !== "welcome";

	return (
		<Tooltip
			label={tooltip}
			position="right"
			openDelay={600}
			withArrow
		>
			<Box pos="relative" className="tab-rail-item">
				<UnstyledButton
					onClick={onActivate}
					style={{
						display: "flex",
						flexDirection: "column",
						alignItems: "flex-start",
						justifyContent: "center",
						width: "100%",
						minHeight: 44,
						padding: "6px 24px 6px 10px",
						borderRadius: 6,
						borderLeft: active
							? "3px solid var(--mantine-color-brand-6)"
							: "3px solid transparent",
						background: active
							? "light-dark(var(--mantine-color-white), var(--mantine-color-dark-6))"
							: "transparent",
						color: active
							? "var(--mantine-color-brand-7)"
							: "var(--mantine-color-gray-7)",
						transition: "background 80ms ease",
					}}
				>
					<Text size="sm" fw={active ? 600 : 500} lineClamp={1}>
						{tab.title}
					</Text>
					{tab.subtitle && (
						<Text size="xs" c="dimmed" lineClamp={1}>
							{tab.subtitle}
						</Text>
					)}
				</UnstyledButton>
				{closeable && (
					<ActionIcon
						size="xs"
						variant="subtle"
						color="gray"
						aria-label="Tab schließen"
						className="tab-rail-close"
						onClick={(e) => {
							e.stopPropagation();
							onClose();
						}}
						style={{
							position: "absolute",
							top: "50%",
							right: 4,
							transform: "translateY(-50%)",
						}}
					>
						<IconX size={12} />
					</ActionIcon>
				)}
			</Box>
		</Tooltip>
	);
}
