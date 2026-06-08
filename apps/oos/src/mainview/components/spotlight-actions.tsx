// spotlight-actions.ts — Action catalogue for the OOS command palette.
//
// Two tiers of actions combined at open time:
//
//   Static  — menu shortcuts always present (navigate to Welcome,
//             Docs, Settings sub-panels, Activity, Streams, Events).
//             Each carries a leftSection icon so the palette rows
//             look like the demo screenshot (icon + label + description).
//
//   Dynamic — Streams and Mappings. Loaded once per component mount
//             via rpc proxies. The caller passes the proxy so this
//             module stays side-effect-free and testable.

import type { SpotlightActionData } from "@mantine/spotlight";
import {
	IconHome,
	IconBook,
	IconHistory,
	IconActivity,
	IconDatabase,
	IconPlus,
	IconSettings,
	IconNetwork,
	IconAdjustments,
	IconList,
	IconLock,
	IconBug,
} from "@tabler/icons-react";

import {
	openActivityList,
	openChatHistory,
	openDocs,
	openNewEvent,
	openSettings,
	openStreamDetail,
	openStreamManager,
	showWelcome,
} from "../store/tabs";
import { openAppErrorLog } from "./AppErrorDrawer";

// ─── Icon size ────────────────────────────────────────────────────────

const SZ = 18;

// ─── Static actions ──────────────────────────────────────────────────

/** Navigation and Settings actions — always present. */
export const STATIC_ACTIONS: SpotlightActionData[] = [
	// ─ Navigation ──────────────────────────────────────────
	{
		id:          "nav-welcome",
		group:       "Navigation",
		label:       "Welcome",
		description: "Go back to the start screen",
		keywords:    ["home", "start"],
		leftSection: <IconHome size={SZ} />,
		onClick:     showWelcome,
	},
	{
		id:          "nav-docs",
		group:       "Navigation",
		label:       "Documentation",
		description: "Browse the built-in documentation",
		keywords:    ["docs", "help", "hilfe"],
		leftSection: <IconBook size={SZ} />,
		onClick:     openDocs,
	},
	{
		id:          "nav-history",
		group:       "Navigation",
		label:       "Chat history",
		description: "Browse and reload saved conversations",
		keywords:    ["history", "verlauf", "chats"],
		leftSection: <IconHistory size={SZ} />,
		onClick:     openChatHistory,
	},
	{
		id:          "nav-activity",
		group:       "Navigation",
		label:       "Activity",
		description: "Inspect every agent turn and tool call",
		keywords:    ["activity", "telemetry", "turns", "log"],
		leftSection: <IconActivity size={SZ} />,
		onClick:     openActivityList,
	},
	{
		id:          "nav-streams",
		group:       "Navigation",
		label:       "Stream manager",
		description: "Create and manage event streams",
		keywords:    ["streams", "events", "manager"],
		leftSection: <IconDatabase size={SZ} />,
		onClick:     openStreamManager,
	},
	{
		id:          "nav-new-event",
		group:       "Navigation",
		label:       "New event",
		description: "Insert a new event into a stream",
		keywords:    ["new", "event", "insert", "neu"],
		leftSection: <IconPlus size={SZ} />,
		onClick:     openNewEvent,
	},
	// ─ Settings ──────────────────────────────────────────
	{
		id:          "settings-connection",
		group:       "Settings",
		label:       "Connection",
		description: "LLM endpoint, API key, model, NATS URL",
		keywords:    ["connection", "llm", "nats", "backend", "api", "key", "endpoint"],
		leftSection: <IconNetwork size={SZ} />,
		onClick:     () => openSettings("connection"),
	},
	{
		id:          "settings-finetuning",
		group:       "Settings",
		label:       "Finetuning",
		description: "Temperature, max tokens, top hits per model",
		keywords:    ["finetuning", "temperature", "model", "tokens", "tuning"],
		leftSection: <IconAdjustments size={SZ} />,
		onClick:     () => openSettings("finetuning"),
	},
	{
		id:          "settings-logs",
		group:       "Settings",
		label:       "Logs",
		description: "Local log ring-buffer viewer",
		keywords:    ["logs", "debug", "log viewer"],
		leftSection: <IconList size={SZ} />,
		onClick:     () => openSettings("logs"),
	},
	{
		id:          "settings-permissions",
		group:       "Settings",
		label:       "Permissions",
		description: "Current role and domain access rights",
		keywords:    ["permissions", "berechtigungen", "role", "rechte", "acl"],
		leftSection: <IconLock size={SZ} />,
		onClick:     () => openSettings("permissions"),
	},
	// ─ Debug ─────────────────────────────────────────────
	{
		id:          "debug-errors",
		group:       "Debug",
		label:       "Error log",
		description: "Show the recent backend error drawer",
		keywords:    ["errors", "fehler", "debug", "log"],
		leftSection: <IconBug size={SZ} />,
		onClick:     openAppErrorLog,
	},
];

// ─── RPC shapes (minimal) ──────────────────────────────────────────────────

interface StreamRow {
	stream:       string;
	description:  string;
	mapping?:     string;
	source_table?: string;
}

interface MappingRow {
	name:   string;
	title?: string;
}

// ─── Dynamic loaders ───────────────────────────────────────────────────────

/**
 * buildStreamActions fetches all streams and converts each row into
 * a SpotlightActionData that opens the stream_detail tab directly.
 * Never throws — returns [] on any error.
 */
export async function buildStreamActions(
	rpcProxy: { listAllStreams: () => Promise<{ json: string; error?: string }> },
): Promise<SpotlightActionData[]> {
	try {
		const res = await rpcProxy.listAllStreams();
		if (res.error || !res.json) return [];
		const parsed = JSON.parse(res.json) as { streams?: StreamRow[] };
		const rows   = Array.isArray(parsed.streams) ? parsed.streams : [];
		return rows.map((row): SpotlightActionData => ({
			id:          `stream-${row.stream}`,
			group:       "Streams",
			label:       row.stream,
			description: row.description || row.mapping || "",
			keywords:    [
				row.stream.toLowerCase(),
				(row.mapping     ?? "").toLowerCase(),
				(row.description ?? "").toLowerCase(),
			].filter(Boolean),
			leftSection: <IconDatabase size={SZ} />,
			onClick:     () =>
				openStreamDetail({
					mapping:     row.mapping      ?? "unknown",
					sourceTable: row.source_table ?? "",
					streamId:    row.stream,
				}),
		}));
	} catch {
		return [];
	}
}

/**
 * buildMappingActions fetches all event mappings and produces one
 * action per mapping opening the Stream Manager. Never throws.
 */
export async function buildMappingActions(
	rpcProxy: { getEventMappings: () => Promise<{ json: string; error?: string }> },
): Promise<SpotlightActionData[]> {
	try {
		const res = await rpcProxy.getEventMappings();
		if (res.error || !res.json) return [];
		const parsed = JSON.parse(res.json) as { mappings?: MappingRow[] };
		const rows   = Array.isArray(parsed.mappings) ? parsed.mappings : [];
		return rows.map((row): SpotlightActionData => ({
			id:          `mapping-${row.name}`,
			group:       "Mappings",
			label:       row.title ?? row.name,
			description: `Mapping · ${row.name}`,
			keywords:    [row.name.toLowerCase(), (row.title ?? "").toLowerCase()].filter(Boolean),
			leftSection: <IconSettings size={SZ} />,
			onClick:     openStreamManager,
		}));
	} catch {
		return [];
	}
}
