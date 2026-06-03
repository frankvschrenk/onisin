// App.tsx — top-level oosd designer shell.
//
// Layout:
//
//   ┌──────┬──────────────────┬──────────────────────────┐
//   │      │ Assistant  │   Editor / Content              │
//   │ Nav  │ (Chat-     │                                 │
//   │      │  Panel)    │                                 │
//   └──────┴────────────┴──────────────────────────┘
//     fixed         ↑ resizable splitter:
//     width          @gfazioli/mantine-split-pane
//
// Nav is a static-width left rail (see Sidebar.tsx + Sidebar.module.css).
// Chat panel sits between Nav and Content; user drags the splitter to
// give the chat more or less room. mantine-split-pane has no collapse
// API and the chat is too central to need one anyway.

import { useEffect, useRef, useState } from "react";
import { AppShell, Box } from "@mantine/core";
import { Split } from "@gfazioli/mantine-split-pane";
import "@gfazioli/mantine-split-pane/styles.css";

import type { Kind } from "./types";

import { Sidebar }            from "./components/Sidebar";
import { Footer }             from "./components/Footer";
import { IdList }             from "./components/IdList";
import { EditorPane }         from "./components/EditorPane";
import { NewItemDialog }      from "./components/NewItemDialog";
import { DemoPanel }          from "./components/DemoPanel";
import { EventsPanel }        from "./components/EventsPanel";
import { EventTypesPanel }    from "./components/EventTypesPanel";
import { MappingTypesPanel }  from "./components/MappingTypesPanel";
import { GrammarPanel }       from "./components/GrammarPanel";
import { KvStorePanel }       from "./components/KvStorePanel";
import { IamPanel }           from "./components/IamPanel";
import { SettingsPanel }      from "./components/SettingsPanel";
import { ChatPanel }          from "./components/ChatPanel";
import type { SubmitArgs as NewSubmitArgs } from "./components/NewItemDialog";
import { stubSource }         from "./components/templates";
import {
	rpc,
	setPreviewWindowChangedHandler,
	setOpenDslInEditorHandler,
} from "./rpc";

const PREVIEW_DEBOUNCE_MS = 300;
const LAYOUT_KEY          = "oosd-layout-v1";
const NAV_WIDTH           = 200;
const CHAT_WIDTH_DEFAULT  = 380;

type KindState = {
	ids:      string[];
	selected: string | null;
	source:   string;
	dirty:    boolean;
};

const emptyKindState: KindState = { ids: [], selected: null, source: "", dirty: false };

// Chat width is persisted in pixels under a fresh key. The previous
// react-resizable-panels build stored a percentage under `-chat`; that
// key is intentionally ignored here so an old value cannot be misread
// as pixels and force the pane to a few-px-wide sliver on first load.
function loadChatWidth(): number {
	try {
		const raw = localStorage.getItem(`${LAYOUT_KEY}-chat-px`);
		const n   = Number(raw);
		return Number.isFinite(n) && n > 0 ? n : CHAT_WIDTH_DEFAULT;
	} catch { return CHAT_WIDTH_DEFAULT; }
}
function saveChatWidth(px: number) {
	try { localStorage.setItem(`${LAYOUT_KEY}-chat-px`, String(Math.round(px))); } catch { /* */ }
}

// Kinds that fill the full content panel (no IdList / EditorPane split).
const FULL_PANEL_KINDS = new Set<Kind>([
	"demo", "events", "event-types", "mapping-types", "grammar", "kv-store", "iam", "settings",
]);

export function App() {
	const [kind,          setKind]          = useState<Kind>("domain");
	const [domainState,   setDomainState]   = useState<KindState>(emptyKindState);
	const [viewState,     setViewState]     = useState<KindState>(emptyKindState);
	const [saving,        setSaving]        = useState(false);
	const [previewOpen,   setPreviewOpen]   = useState(false);
	const [newDialogOpen, setNewDialogOpen] = useState(false);

	const active = kind === "domain" ? domainState : viewState;
	// setActive replaces the whole state — use updateActive for async
	// handlers to avoid stale-closure overwrites of concurrent updates.
	const setActive = (next: KindState) => {
		if (kind === "domain") setDomainState(next);
		else                   setViewState(next);
	};
	const updateActive = (fn: (prev: KindState) => KindState) => {
		if (kind === "domain") setDomainState(fn);
		else                   setViewState(fn);
	};

	// ── On mount ──────────────────────────────────────────────
	useEffect(() => {
		void Promise.all([refreshDomain(), refreshView()]);
		// eslint-disable-next-line react-hooks/exhaustive-deps
	}, []);

	// ── RPC handlers ──────────────────────────────────────────
	useEffect(() => {
		setPreviewWindowChangedHandler(({ open }) => setPreviewOpen(open));
		setOpenDslInEditorHandler(({ kind: k, id, source }) => {
			if (k === "domain") {
				setKind("domain");
				setDomainState((p) => ({ ...p, selected: id, source, dirty: true }));
			} else {
				setKind("view");
				setViewState((p) => ({ ...p, selected: id, source, dirty: true }));
			}
		});
	}, []);

	// ── Preview push ──────────────────────────────────────────
	const pushTimer = useRef<ReturnType<typeof setTimeout> | null>(null);
	useEffect(() => {
		if (kind !== "view") return;
		if (pushTimer.current) clearTimeout(pushTimer.current);
		pushTimer.current = setTimeout(() => {
			rpc.pushPreviewSource({ source: active.source, viewId: active.selected }).catch(() => {});
		}, PREVIEW_DEBOUNCE_MS);
		return () => { if (pushTimer.current) clearTimeout(pushTimer.current); };
	}, [kind, active.source, active.selected]);

	async function openPreview()  { await rpc.openPreview({});  }
	async function closePreview() { await rpc.closePreview({}); }

	// ── Data ops ────────────────────────────────────────────────
	async function refreshDomain() {
		const res = await rpc.listDomain({});
		if (!res.error) setDomainState((s) => ({ ...s, ids: res.ids ?? [] }));
	}
	async function refreshView() {
		const res = await rpc.listView({});
		if (!res.error) setViewState((s) => ({ ...s, ids: res.ids ?? [] }));
	}
	async function refreshActive() {
		return kind === "domain" ? refreshDomain() : refreshView();
	}
	async function selectId(id: string) {
		updateActive((p) => ({ ...p, selected: id, source: "", dirty: false }));
		const res = kind === "domain" ? await rpc.loadDomain({ id }) : await rpc.loadView({ id });
		updateActive((p) => ({
			...p,
			selected: id,
			source: res.source ?? (res.error ? `<!-- error: ${res.error} -->` : ""),
			dirty: false,
		}));
	}
	function onSourceChange(v: string) { updateActive((p) => ({ ...p, source: v, dirty: true })); }
	async function onSave() {
		if (!active.selected) return;
		setSaving(true);
		try {
			const res = kind === "domain"
				? await rpc.saveDomain({ id: active.selected, source: active.source })
				: await rpc.saveView({ id: active.selected, source: active.source });
			if (res.ok) updateActive((p) => ({ ...p, dirty: false }));
		} finally { setSaving(false); }
	}
	async function onSubmitNew({ id, template }: NewSubmitArgs) {
		const source = template === "copy" && active.source ? active.source : stubSource(kind, id);
		const res = kind === "domain"
			? await rpc.insertDomain({ id, source })
			: await rpc.insertView({ id, source });
		if (!res.ok) return res;
		await refreshActive();
		updateActive((p) => ({ ...p, selected: id, source, dirty: false }));
		return res;
	}
	async function onDelete(id: string) {
		const res = kind === "domain"
			? await rpc.deleteDomain({ id })
			: await rpc.deleteView({ id });
		if (!res.ok) return res;
		await refreshActive();
		updateActive((p) => p.selected === id ? { ...p, selected: null, source: "", dirty: false } : p);
		return res;
	}

	// ── Render ──────────────────────────────────────────────────
	const isFullPanel = FULL_PANEL_KINDS.has(kind);

	const contentPane = isFullPanel ? (
		<Box style={{ height: "100%", overflow: kind === "settings" ? "auto" : "hidden" }}>
			{kind === "demo"          && <DemoPanel         dsn="" disabled={false} />}
			{kind === "events"        && <EventsPanel       disabled={false} />}
			{kind === "event-types"   && <EventTypesPanel   disabled={false} />}
			{kind === "mapping-types" && <MappingTypesPanel disabled={false} />}
			{kind === "grammar"       && <GrammarPanel      disabled={false} />}
			{kind === "kv-store"      && <KvStorePanel      disabled={false} />}
			{kind === "iam"           && <IamPanel          disabled={false} />}
			{kind === "settings"      && <SettingsPanel />}
		</Box>
	) : (
		<Box style={{ height: "100%", display: "grid", gridTemplateColumns: "280px 1fr" }}>
			<Box style={{ borderRight: "1px solid var(--mantine-color-default-border)", overflow: "hidden" }}>
				<IdList
					kind={kind}
					ids={active.ids}
					selected={active.selected}
					onSelect={selectId}
					onRefresh={refreshActive}
					onNew={() => setNewDialogOpen(true)}
					onDelete={onDelete}
					disabled={false}
				/>
			</Box>
			<Box style={{ overflow: "hidden" }}>
				<EditorPane
					kind={kind}
					selected={active.selected}
					source={active.source}
					dirty={active.dirty}
					saving={saving}
					onSourceChange={onSourceChange}
					onSave={onSave}
					previewOpen={previewOpen}
					onOpenPreview={openPreview}
					onClosePreview={closePreview}
				/>
			</Box>
		</Box>
	);

	return (
		<AppShell navbar={{ width: NAV_WIDTH, breakpoint: 0 }} footer={{ height: 28 }} padding={0}>

			{/* ── Sidebar ── */}
			<AppShell.Navbar style={{ overflow: "hidden" }}>
				<Sidebar kind={kind} onChange={setKind} />
			</AppShell.Navbar>

			{/* ── Main: chat (left) | content (right) via mantine-split-pane ── */}
			<AppShell.Main style={{ display: "flex", flexDirection: "column", height: "100vh" }}>
				<Split
					orientation="vertical"
					autoResizers
					style={{ flex: 1, minHeight: 0 }}
				>
					<Split.Pane
						initialWidth={loadChatWidth()}
						minWidth={250}
						style={{ overflow: "hidden" }}
						onResizeEnd={(size) => saveChatWidth(size.width)}
					>
						<ChatPanel
							activeViewId={active.selected ?? undefined}
							activeKind={kind}
						/>
					</Split.Pane>
					<Split.Pane grow minWidth={300} style={{ overflow: "hidden" }}>
						{contentPane}
					</Split.Pane>
				</Split>
			</AppShell.Main>

			<Footer />

			<NewItemDialog
				opened={newDialogOpen}
				onClose={() => setNewDialogOpen(false)}
				onSubmit={onSubmitNew}
				kind={kind}
				existingIds={active.ids}
				currentSelection={active.selected}
			/>
		</AppShell>
	);
}
