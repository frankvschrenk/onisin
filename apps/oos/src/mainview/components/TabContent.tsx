// TabContent.tsx — Right-most column showing the active tab.
//
// Dispatches by the active tab's `payload.kind`. Welcome and the
// individual doc topics each have their own renderer; graphql_result
// tabs go through ViewRenderer, which itself falls back to
// ResultTable when no view DSL is in play. dsl_detail tabs go
// through DetailRenderer for single-record edits.
//
// Adding a real domain view later means: extend TabPayload, add a
// case here, point at the right renderer. The structure does not
// change.

import { Box, Center, Stack, Text } from "@mantine/core";

import type { TabRecord } from "../store/tabs";
import { ActivityDetailPanel }       from "./ActivityDetailPanel";
import { ActivityListPanel }         from "./ActivityListPanel";
import { ChatHistoryPanel }          from "./ChatHistoryPanel";
import { StreamManagerPanel }        from "./StreamManagerPanel";
import { NewEventPanel }            from "./NewEventPanel";
import { TranslatePanel }           from "./TranslatePanel";
import { StreamDetailPanel }        from "./StreamDetailPanel";
import { PipelineListPanel }        from "./PipelineListPanel";
import { PipelineRunPanel }         from "./PipelineRunPanel";
import { DetailRenderer }            from "./DetailRenderer";
import { DocsPanel }                 from "./DocsPanel";
import { EventResultPanel }          from "./EventResultPanel";
import { AskResultPanel }            from "./AskResultPanel";
import { SettingsConnectionPanel }   from "./SettingsConnectionPanel";
import { SettingsFinetuningPanel }   from "./SettingsFinetuningPanel";
import { LogsContent }               from "./LogsContent";
import { SettingsPermissionsPanel }  from "./SettingsPermissionsPanel";
import { ViewRenderer }              from "./ViewRenderer";
import { DevPanel }                  from "./DevPanel";
import { WelcomePanel }              from "./WelcomePanel";

interface TabContentProps {
	tab: TabRecord | null;
}

export function TabContent({ tab }: TabContentProps) {
	if (!tab) {
		return (
			<Center style={{ height: "100%" }}>
				<Text c="dimmed">Kein Tab geöffnet.</Text>
			</Center>
		);
	}

	switch (tab.payload.kind) {
		case "welcome":
			return <WelcomePanel />;
		case "doc":
			return <DocsPanel topicId={tab.payload.topicId} />;
		case "graphql_result":
			return (
				<ViewRenderer
					contextName={tab.payload.contextName}
					query={tab.payload.query}
					data={tab.payload.data}
					viewName={tab.payload.viewName}
				/>
			);
		case "event_result":
			return (
				<EventResultPanel
					mapping={tab.payload.mapping}
					streamId={tab.payload.streamId}
					question={tab.payload.question}
					answer={tab.payload.answer}
					hits={tab.payload.hits}
					model={tab.payload.model}
				/>
			);
		case "ask_result":
			return (
				<AskResultPanel
					question={tab.payload.question}
					answer={tab.payload.answer}
					model={tab.payload.model}
				/>
			);
		case "dsl_detail":
			return (
				<DetailRenderer
					tabId={tab.id}
					contextName={tab.payload.contextName}
					viewName={tab.payload.viewName}
					data={tab.payload.data}
					id={tab.payload.id}
				/>
			);
		case "settings_connection":
			return <SettingsConnectionPanel />;
		case "settings_finetuning":
			return <SettingsFinetuningPanel />;
		case "settings_logs":
			return <LogsContent />;
		case "settings_permissions":
			return <SettingsPermissionsPanel />;
		case "chat_history":
			return <ChatHistoryPanel />;
		case "activity_list":
			return <ActivityListPanel />;
		case "activity_detail":
			return <ActivityDetailPanel turnId={tab.payload.turnId} />;
		case "stream_manager":
			return <StreamManagerPanel />;
		case "new_event":
			return <NewEventPanel />;
		case "translate":
			// key={tab.id} keeps the two MDXEditor instances from being
			// reused if React ever diffs this against a sibling panel.
			return <TranslatePanel key={tab.id} />;
		case "stream_detail":
			return (
				<StreamDetailPanel
					mapping={tab.payload.mapping}
					sourceTable={tab.payload.sourceTable}
					streamId={tab.payload.streamId}
				/>
			);
		case "dev":
			return <DevPanel />;
		case "pipeline_list":
			return <PipelineListPanel />;
		case "pipeline_run":
			// `key={tab.id}` forces a fresh mount per pipeline tab. Without it
			// React reuses the component instance across sibling pipeline tabs,
			// so startedRef, the runId subscriptions, and the MDXEditor's
			// initial-markdown prop all leak from one tab into the next.
			return (
				<PipelineRunPanel
					key={tab.id}
					tabId={tab.id}
					pipelineName={tab.payload.pipelineName}
					initialStatus={tab.payload.status}
					initialOutput={tab.payload.output ?? ""}
					initialError={tab.payload.error}
					initialRunId={tab.payload.runId}
				/>
			);
		case "person_list":
			return <MockPersonList />;
		case "person_detail":
			return <MockPersonDetail />;
		case "note_list":
			return <MockNoteList />;
	}
}

// ─── Mock bodies (until the DSL renderer takes over) ─────────────────
//
// Kept around because the keyboard-shortcut menu still opens these
// tabs by hand for offline demoing. Once every demo path goes
// through the chat → tab_open pipeline, this whole block can move
// out.

function MockPersonList() {
	const rows = [
		{ id: 1, firstname: "Anna", lastname: "Meier",    age: 42, city: "Berlin"   },
		{ id: 2, firstname: "Bea",  lastname: "Schulz",   age: 31, city: "München"  },
		{ id: 3, firstname: "Carl", lastname: "Weber",    age: 55, city: "Hamburg"  },
		{ id: 4, firstname: "Dora", lastname: "Krause",   age: 28, city: "Köln"     },
		{ id: 5, firstname: "Eva",  lastname: "Hoffmann", age: 47, city: "Stuttgart"},
	];
	return (
		<Box p="lg" style={{ overflow: "auto", height: "100%" }}>
			<table
				style={{
					width: "100%",
					borderCollapse: "collapse",
					fontSize: 14,
				}}
			>
				<thead>
					<tr style={{ textAlign: "left", color: "var(--mantine-color-gray-7)" }}>
						<th style={cellStyle}>Vorname</th>
						<th style={cellStyle}>Nachname</th>
						<th style={cellStyle}>Alter</th>
						<th style={cellStyle}>Stadt</th>
					</tr>
				</thead>
				<tbody>
					{rows.map((r) => (
						<tr key={r.id} style={{ borderTop: "1px solid light-dark(var(--mantine-color-gray-2), var(--mantine-color-dark-5))" }}>
							<td style={cellStyle}>{r.firstname}</td>
							<td style={cellStyle}>{r.lastname}</td>
							<td style={cellStyle}>{r.age}</td>
							<td style={cellStyle}>{r.city}</td>
						</tr>
					))}
				</tbody>
			</table>
		</Box>
	);
}

function MockPersonDetail() {
	const fields: Array<[string, string]> = [
		["Vorname",  "Anna"],
		["Nachname", "Meier"],
		["Alter",    "42"],
		["Stadt",    "Berlin"],
		["E-Mail",   "anna.meier@example.com"],
		["Sprache",  "Deutsch"],
		["Aktiv",    "ja"],
	];
	return (
		<Box p="lg" style={{ overflow: "auto", height: "100%" }}>
			<Stack gap="xs" maw={520}>
				{fields.map(([label, value]) => (
					<Box key={label} style={{ display: "flex", gap: 16 }}>
						<Text fw={500} w={140} c="gray.7">
							{label}
						</Text>
						<Text>{value}</Text>
					</Box>
				))}
			</Stack>
		</Box>
	);
}

function MockNoteList() {
	const rows = [
		{ id: 1, title: "Kickoff-Meeting",   updated: "2026-04-29" },
		{ id: 2, title: "Quarterly Review",  updated: "2026-04-22" },
		{ id: 3, title: "Architektur-Notiz", updated: "2026-04-15" },
	];
	return (
		<Box p="lg" style={{ overflow: "auto", height: "100%" }}>
			<Stack gap="xs">
				{rows.map((r) => (
					<Box
						key={r.id}
						p="sm"
						style={{
							display: "flex",
							justifyContent: "space-between",
							border: "1px solid var(--mantine-color-gray-3)",
							borderRadius: 6,
						}}
					>
						<Text fw={500}>{r.title}</Text>
						<Text size="xs" c="dimmed">
							{r.updated}
						</Text>
					</Box>
				))}
			</Stack>
		</Box>
	);
}

const cellStyle: React.CSSProperties = {
	padding: "8px 12px",
	verticalAlign: "top",
};
