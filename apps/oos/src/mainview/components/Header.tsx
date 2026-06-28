// Header.tsx — Top app header.
//
// Left: a Mantine Menu opened by the burger icon. The menu pattern
// is portable from the crypto project (AppNavMenu): `Menu.Target`
// holds the trigger button, `Menu.Dropdown` lists the items.
// Sections are introduced with `Menu.Label` separators.
//
// Welcome, Documentation and Settings are all entries that open or
// focus a tab group through the tabs store. Settings used to live
// in a side drawer; it is now a regular group so finetuning, model
// switching and any future panels can stay open in parallel to
// active chat results — same workspace pattern as Docs.
//
// Chat history is now a regular tab group — same shape as Settings
// and Docs. The menu entry calls openChatHistory() on the tabs
// store, which opens the tab on first click and focuses it on
// every subsequent one.
//
// Right side: ModeSwitch (Markdown / source / preview etc.) plus
// the Light/Dark/Auto color scheme toggle. The slot stays a Group
// so further per-page actions (search, notifications bell) can
// drop in without restructuring the layout.

import { ActionIcon, Group, Menu, Text } from "@mantine/core";
import {
	IconActivity,
	IconBook,
	IconCode,
	IconHistory,
	IconHome,
	IconLogout,
	IconMenu2,
	IconSettings,
	IconDatabase,
	IconLanguage,
	IconPlus,
	IconSearch,
	IconTimeline,
} from "@tabler/icons-react";
import { spotlight } from "@mantine/spotlight";

import {
	openActivityList,
	openChatHistory,
	openDev,
	openDocs,
	openNewEvent,
	openPipelineList,
	openSettings,
	openStreamManager,
	openTranslate,
	showWelcome,
} from "../store/tabs";
import { ColorSchemeToggle }  from "oos-theme-ts";
import { ModeSwitch }         from "./ModeSwitch";
import { rpc } from "../rpc";

export function Header({ onLogout }: { onLogout: () => void }) {
	async function handleLogout() {
		await rpc.logout({});
		onLogout();
	}

	return (
		<Group h="100%" px="md" justify="space-between" wrap="nowrap">
			<Menu shadow="md" width={240} position="bottom-start" withArrow>
				<Menu.Target>
					<ActionIcon
						variant="default"
						size="lg"
						aria-label="Menü öffnen"
					>
						<IconMenu2 size={18} />
					</ActionIcon>
				</Menu.Target>

				<Menu.Dropdown>
					<Menu.Item
						leftSection={<IconSearch size={16} />}
						rightSection={
							<Text size="xs" c="dimmed">
								⌘ K
							</Text>
						}
						onClick={() => spotlight.open()}
					>
						Command Palette
					</Menu.Item>
					<Menu.Divider />
					<Menu.Label>Application</Menu.Label>
					<Menu.Item
						leftSection={<IconHome size={16} />}
						onClick={showWelcome}
					>
						Welcome
					</Menu.Item>
					<Menu.Item
						leftSection={<IconHistory size={16} />}
						onClick={openChatHistory}
					>
						Chat-Verlauf
					</Menu.Item>
					<Menu.Item
						leftSection={<IconBook size={16} />}
						onClick={openDocs}
					>
						Documentation
					</Menu.Item>
					<Menu.Item
						leftSection={<IconActivity size={16} />}
						onClick={openActivityList}
					>
						Activity
					</Menu.Item>
					<Menu.Item
						leftSection={<IconDatabase size={16} />}
						onClick={openStreamManager}
					>
						Streams
					</Menu.Item>
					<Menu.Item
						leftSection={<IconTimeline size={16} />}
						onClick={openPipelineList}
					>
						Pipelines
					</Menu.Item>
					<Menu.Item
						leftSection={<IconPlus size={16} />}
						onClick={openNewEvent}
					>
						New Event
					</Menu.Item>
					<Menu.Item
						leftSection={<IconCode size={16} />}
						onClick={openDev}
					>
						Dev
					</Menu.Item>
					<Menu.Item
						leftSection={<IconLanguage size={16} />}
						onClick={openTranslate}
					>
						Translate
					</Menu.Item>
					<Menu.Item
						leftSection={<IconSettings size={16} />}
						rightSection={
							<Text size="xs" c="dimmed">
								⌘ ,
							</Text>
						}
						onClick={() => openSettings()}
					>
						Settings
					</Menu.Item>
					<Menu.Divider />
					<Menu.Item
						leftSection={<IconLogout size={16} />}
						color="red"
						onClick={() => void handleLogout()}
					>
						Abmelden
					</Menu.Item>
				</Menu.Dropdown>
			</Menu>

			<Group gap="sm">
				<ModeSwitch />
				<ColorSchemeToggle />
			</Group>
		</Group>
	);
}
