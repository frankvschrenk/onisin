// Sidebar.tsx — left navigation for oosd.
//
// Static-width nav, no collapse. The pattern is the Mantine "navbar"
// recipe: a plain <nav> with a CSS module (Sidebar.module.css), one
// section per group, separated by border-bottom on every section that
// is not the last. The top section has flex:1 so the bottom section
// (Settings / Demo) sticks to the bottom of the column. Active state
// is driven by the `kind` prop from App.tsx; clicks call onChange.
//
// Collapse was dropped deliberately: at the sidebar's natural width
// the icons + labels take well under 5 % of the window, so the
// hide-to-rail interaction does not earn its complexity.

import { UnstyledButton } from "@mantine/core";
import {
	IconBraces,
	IconCalendarEvent,
	IconDatabase,
	IconLayout,
	IconList,
	IconMap2,
	IconPlayerPlay,
	IconServer,
	IconSettings,
	IconUsers,
	type Icon,
} from "@tabler/icons-react";
import type { Kind } from "../types";
import classes from "./Sidebar.module.css";

interface NavItem {
	id:    Kind;
	label: string;
	icon:  Icon;
}

const NAV_ITEMS: NavItem[] = [
	{ id: "domain",        label: "Domain",        icon: IconDatabase      },
	{ id: "view",          label: "View",          icon: IconLayout        },
	{ id: "events",        label: "Events",        icon: IconCalendarEvent },
	{ id: "event-types",   label: "Event Types",   icon: IconList          },
	{ id: "mapping-types", label: "Mapping Types", icon: IconMap2          },
	{ id: "grammar",       label: "Grammar",       icon: IconBraces        },
	{ id: "kv-store",      label: "KV Store",      icon: IconServer        },
	{ id: "iam",           label: "Users",         icon: IconUsers         },
];

const BOTTOM_ITEMS: NavItem[] = [
	{ id: "settings", label: "Settings", icon: IconSettings   },
	{ id: "demo",     label: "Demo",     icon: IconPlayerPlay },
];

export function Sidebar({
	kind,
	onChange,
}: {
	kind:     Kind;
	onChange: (k: Kind) => void;
}) {
	return (
		<nav className={classes.navbar}>
			<div className={`${classes.section} ${classes.sectionGrow}`}>
				<div className={classes.mainLinks}>
					{NAV_ITEMS.map((item) => (
						<SidebarItem
							key={item.id}
							item={item}
							active={kind === item.id}
							onClick={() => onChange(item.id)}
						/>
					))}
				</div>
			</div>

			<div className={classes.section}>
				<div className={classes.mainLinks}>
					{BOTTOM_ITEMS.map((item) => (
						<SidebarItem
							key={item.id}
							item={item}
							active={kind === item.id}
							onClick={() => onChange(item.id)}
						/>
					))}
				</div>
			</div>
		</nav>
	);
}

function SidebarItem({
	item,
	active,
	onClick,
}: {
	item:    NavItem;
	active:  boolean;
	onClick: () => void;
}) {
	const Icon = item.icon;
	const className = active
		? `${classes.mainLink} ${classes.mainLinkActive}`
		: classes.mainLink;
	return (
		<UnstyledButton className={className} onClick={onClick}>
			<Icon size={18} stroke={1.5} className={classes.mainLinkIcon} />
			<span>{item.label}</span>
		</UnstyledButton>
	);
}
