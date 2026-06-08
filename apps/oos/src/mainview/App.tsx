// App.tsx — top-level shell of the oos app.
//
// Three-column main area sandwiched between a thin header and a
// status footer. The split between the chat column and the tab
// workspace is a draggable resizable panel; the tab rail itself
// stays at a fixed 160px because it is designed as a compact
// vertical strip, not a primary surface.
//
//   ┌────────────────────────────────────────────────────────────────┐
//   │  Header (burger menu)                                         │
//   ├──────────────────╫───────┬────────────────────────────────────┤
//   │                  ║       │                                  │
//   │     Chat       ←─║→ Tabs │   Active tab content             │
//   │                  ║       │   (Welcome, doc, settings,       │
//   │   ┌──────────┐   ║       │    or DSL view)                  │
//   │   │ input ▶  │   ║       │                                  │
//   │   └──────────┘   ║       │                                  │
//   ├──────────────────╩───────┴────────────────────────────────────┤
//   │  Footer (model · endpoint health)                             │
//   └────────────────────────────────────────────────────────────────┘
//        the ║ is the resizable handle: chat ⇆ tab workspace
//
// The handle position persists across reloads via useDefaultLayout
// reading from / writing to localStorage. The hook produces a
// `defaultLayout` prop the Group consumes on mount.
//
// Tab state lives in store/tabs.ts. Tabs are organised into groups
// (welcome, docs, results, settings, future activity); group
// displacement rules are encoded on the group itself.
//
// Settings used to be a modal drawer; it is now a parallel tab
// group — same place finetuning lives, same place future
// permission/embedding panels will live. The `mod+,` shortcut and
// the burger-menu entry both call into `openSettings()`.
//
// Chat history is a regular tab group like Settings and Docs.
// Click-to-load and delete actions on rows publish on the
// chat-events bus; this component subscribes once and wires those
// signals back into useChat. Drag-from-row-into-composer is
// handled directly by the composer's drop handler — it doesn't
// need a round-trip through here.

import { useCallback, useEffect, useState } from "react";
import { AppShell } from "@mantine/core";
import { useHotkeys } from "@mantine/hooks";
import { Group, Panel, Separator, useDefaultLayout } from "react-resizable-panels";

import { subscribe as subscribeChatEvent } from "./chat-events";
import { clearClaims }        from "./store/auth";
import { Chat }                from "./components/Chat";
import { Footer }              from "./components/Footer";
import { OosSpotlight }        from "./components/OosSpotlight";
import { AppErrorDrawer }      from "./components/AppErrorDrawer";
import { Header }              from "./components/Header";
import { TabRail }             from "./components/TabRail";
import { TabContent }          from "./components/TabContent";
import { loadEmbedder }        from "./embedder/embedder";
import { useChat }             from "./hooks/useChat";
import { deleteChat }          from "./store/chats";
import { loadResolverIndex }   from "./store/resolver";
import { useAppSettings }      from "./store/settings";
import { LoginScreen }         from "./components/LoginScreen";
import {
	closeTab,
	openSettings,
	setActive,
	useTabs,
} from "./store/tabs";

// `v2` suffix bumps the localStorage key past any leftover entry
// from the brief moment when defaultSize was a bare number — V4
// reads bare numbers as px, which collapsed both panels.
const LAYOUT_ID  = "oos-main-layout-v2";
const PANEL_CHAT = "chat";
const PANEL_TABS = "tabs";

export function App() {
	const [loggedIn, setLoggedIn] = useState(false);
	const handleDone   = useCallback(() => setLoggedIn(true),  []);
	const handleLogout = useCallback(() => {
		clearClaims();
		setLoggedIn(false);
	}, []);

	return (
		<>
			{!loggedIn && <LoginScreen onDone={handleDone} />}
			{loggedIn  && <MainApp onLogout={handleLogout} />}
			<OosSpotlight />
		</>
	);
}

function MainApp({ onLogout }: { onLogout: () => void }) {
	const { groups, activeId } = useTabs();
	const { settings }         = useAppSettings();
	const {
		messages,
		busy,
		send,
		cancel,
		activeChatId,
		newChat,
		loadChatById,
	} = useChat(settings);

	useHotkeys([
		["mod+,", () => openSettings()],
	]);

	const handleDeleteChat = async (id: string) => {
		await deleteChat(id);
		if (id === activeChatId) newChat();
	};

	useEffect(() => {
		return subscribeChatEvent((evt) => {
			if (evt.kind === "load")   { void loadChatById(evt.chatId); return; }
			if (evt.kind === "delete") { void handleDeleteChat(evt.chatId); return; }
		});
		// eslint-disable-next-line react-hooks/exhaustive-deps
	}, [activeChatId]);

	useEffect(() => {
		void loadResolverIndex();
		void loadEmbedder();
	}, []);

	const { defaultLayout, onLayoutChanged } = useDefaultLayout({
		id:       LAYOUT_ID,
		panelIds: [PANEL_CHAT, PANEL_TABS],
		storage:  typeof localStorage !== "undefined" ? localStorage : undefined,
	});

	const activeTab =
		groups.flatMap((g) => g.tabs).find((t) => t.id === activeId) ?? null;

	return (
		<AppShell
			header={{ height: 48 }}
			footer={{ height: 28 }}
			padding={0}
			styles={{
				main: {
					height: "100vh",
					display: "flex",
					flexDirection: "column",
					minHeight: 0,
				},
			}}
		>
			<AppShell.Header>
				<Header onLogout={onLogout} />
			</AppShell.Header>

			<AppShell.Main>
				<Group
					id={LAYOUT_ID}
					orientation="horizontal"
					defaultLayout={defaultLayout}
					onLayoutChanged={onLayoutChanged}
					style={{ flex: 1, minHeight: 0, display: "flex" }}
				>
					<Panel
						id={PANEL_CHAT}
						defaultSize="30%"
						minSize="20%"
						maxSize="60%"
						style={{ overflow: "hidden" }}
					>
						<Chat messages={messages} busy={busy} onSend={send} onCancel={cancel} onClear={newChat} />
					</Panel>

					<Separator
						className="oos-resize-handle"
						style={{
							width: 4,
							background: "light-dark(var(--mantine-color-gray-2), var(--mantine-color-dark-5))",
							cursor: "col-resize",
							transition: "background 120ms ease",
							flexShrink: 0,
						}}
					/>

					<Panel
						id={PANEL_TABS}
						defaultSize="70%"
						minSize="40%"
						style={{ overflow: "hidden" }}
					>
						<div
							style={{
								height: "100%",
								display: "grid",
								gridTemplateColumns: "160px minmax(0, 1fr)",
								minHeight: 0,
							}}
						>
							<TabRail
								groups={groups}
								activeId={activeId}
								onActivate={setActive}
								onClose={closeTab}
							/>
							<TabContent tab={activeTab} />
						</div>
					</Panel>
				</Group>
			</AppShell.Main>

			<Footer />
			<AppErrorDrawer />
		</AppShell>
	);
}
