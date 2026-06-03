// static.tsx — Mantine renderers for non-bound widgets: button, link,
// icon, richtext.

import { Anchor, Button, Stack, Text, Title } from "@mantine/core";
import type { ReactElement } from "react";
import type { ButtonDef, IconDef, LinkDef, RichTextDef } from "oos-dsls-ts/types";
import {
	IconUser,
	IconUsers,
	IconSettings,
	IconHome,
	IconSearch,
	IconPlus,
	IconEdit,
	IconTrash,
	IconCheck,
	IconX,
	IconAlertCircle,
	IconInfoCircle,
	IconMail,
	IconPhone,
	IconMapPin,
	IconBriefcase,
	IconCalendar,
	IconClock,
	IconStar,
	IconHeart,
	IconLock,
	IconKey,
	IconFile,
	IconFolder,
	IconDatabase,
	IconChartBar,
	IconDashboard,
	IconLogout,
	IconLogin,
	IconRefresh,
	IconDownload,
	IconUpload,
	IconEye,
	IconEyeOff,
	IconBell,
	IconTag,
	IconLink,
	IconPhoto,
	IconBuilding,
	IconWorld,
	IconMessage,
	IconShield,
} from "@tabler/icons-react";
import type { FC } from "react";

// ─── Icon name → Tabler component ───────────────────────────────────────
//
// Accepts Material Design icon names (used in the DSL) and maps them
// to the closest Tabler equivalent. Unknown names fall back to
// IconUser so the layout is never broken by a missing icon.

type TablerIcon = FC<{ size?: number; stroke?: number }>;

const ICON_MAP: Record<string, TablerIcon> = {
	// People
	account:          IconUser,
	person:           IconUser,
	people:           IconUsers,
	group:            IconUsers,
	// Actions
	search:           IconSearch,
	add:              IconPlus,
	edit:             IconEdit,
	delete:           IconTrash,
	check:            IconCheck,
	close:            IconX,
	refresh:          IconRefresh,
	download:         IconDownload,
	upload:           IconUpload,
	logout:           IconLogout,
	login:            IconLogin,
	// Navigation
	home:             IconHome,
	settings:         IconSettings,
	dashboard:        IconDashboard,
	// Communication
	email:            IconMail,
	phone:            IconPhone,
	message:          IconMessage,
	notifications:    IconBell,
	// Content
	file:             IconFile,
	folder:           IconFolder,
	photo:            IconPhoto,
	image:            IconPhoto,
	link:             IconLink,
	tag:              IconTag,
	// Location & time
	location:         IconMapPin,
	place:            IconMapPin,
	map:              IconMapPin,
	calendar:         IconCalendar,
	schedule:         IconClock,
	// Business
	work:             IconBriefcase,
	business:         IconBuilding,
	company:          IconBuilding,
	// Data
	database:         IconDatabase,
	chart:            IconChartBar,
	bar_chart:        IconChartBar,
	// Status
	warning:          IconAlertCircle,
	error:            IconAlertCircle,
	info:             IconInfoCircle,
	star:             IconStar,
	favorite:         IconHeart,
	// Security
	security:         IconShield,
	lock:             IconLock,
	key:              IconKey,
	visibility:       IconEye,
	visibility_off:   IconEyeOff,
	// Misc
	public:           IconWorld,
	language:         IconWorld,
};

export function ButtonRenderer({
	def,
	onAction,
}: {
	def:      ButtonDef;
	onAction?: (actionRef: string | undefined) => void;
}): ReactElement {
	return (
		<Button onClick={() => onAction?.(def.actionRef)} variant="light">
			{def.caption}
		</Button>
	);
}

export function LinkRenderer({ def }: { def: LinkDef }): ReactElement {
	return (
		<Anchor href={def.href} target="_blank" rel="noreferrer">
			{def.caption}
		</Anchor>
	);
}

export function IconRenderer({ def }: { def: IconDef }): ReactElement {
	const size = def.size ?? 24;
	const name = def.name?.toLowerCase().replace(/[\s-]/g, "_") ?? "";
	const Icon: TablerIcon = ICON_MAP[name] ?? IconUser;
	return <Icon size={size} stroke={1.5} />;
}

export function RichTextRenderer({ def }: { def: RichTextDef }): ReactElement {
	return (
		<Stack gap={4}>
			{def.spans.map((span, i) => {
				switch (span.style) {
					case "heading":
						return <Title key={i} order={3}>{span.text}</Title>;
					case "subheading":
						return <Title key={i} order={4}>{span.text}</Title>;
					case "bold":
						return <Text key={i} fw={700}>{span.text}</Text>;
					case "italic":
						return <Text key={i} fs="italic">{span.text}</Text>;
					case "mono":
						return <Text key={i} ff="monospace">{span.text}</Text>;
					case "plain":
					default:
					return <Text key={i}>{span.text}</Text>;
				}
			})}
		</Stack>
	);
}
