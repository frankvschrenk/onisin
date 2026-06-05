// src/mainview/App.tsx — bench desktop app root.
//
// Layout: left nav-rail (Tools | Logs | Settings) + right content.
// Settings group has four sub-panels: General, Database, Servers, Roots.
//
// Transport is Tauri (see rpc.ts): ready/clear/save_settings commands and the
// tool-event/log-record/server-status events.

import { useEffect, useRef, useState, type ReactNode } from "react";
import {
  MantineProvider, Group, Text, Badge, ActionIcon,
  Tooltip, NavLink, Stack, Divider,
} from "@mantine/core";
import { theme } from "oos-theme-ts";
import {
  IconTools, IconFileText, IconSettings,
  IconServer, IconFolder, IconDatabase, IconTrash,
  IconPlayerPause, IconPlayerPlay,
} from "@tabler/icons-react";
import "@mantine/core/styles.css";

import { rpc, onToolEvent, onLogRecord, onServerStatus } from "./rpc";
import { useEventStore, useLogStore } from "./store/events";
import { notifySettingsChange, useSettings } from "./store/settings";
import { EventRow } from "./components/EventRow";
import { LogRow } from "./components/LogRow";
import { SettingsGeneralPanel } from "./components/SettingsGeneralPanel";
import { SettingsServersPanel } from "./components/SettingsServersPanel";
import { SettingsRootsPanel } from "./components/SettingsRootsPanel";
import { SettingsDatabasePanel } from "./components/SettingsDatabasePanel";
import type { LogRecord, ServerStatus, ToolEvent } from "./types";

type NavItem =
  | "tools"
  | "logs"
  | "settings-general"
  | "settings-database"
  | "settings-servers"
  | "settings-roots";

export function App() {
  const { events, addEvent, clear: clearEvents } = useEventStore();
  const { logs, addLog, clear: clearLogs } = useLogStore();
  const { settings, save } = useSettings();

  const [nav, setNav] = useState<NavItem>("tools");
  const [paused, setPaused] = useState(false);
  const pausedRef = useRef(false);

  const [statuses, setStatuses] = useState<Map<string, boolean>>(new Map());

  // Bootstrap: call ready() once on mount to seed settings and server statuses.
  useEffect(() => {
    rpc.ready().then(({ settings: s, serverStatuses: ss }) => {
      notifySettingsChange(s);
      const m = new Map<string, boolean>();
      for (const st of ss) m.set(st.name, st.connected);
      setStatuses(m);
    });
  }, []);

  useEffect(() => {
    const unsubTool = onToolEvent((event: ToolEvent) => {
      if (!pausedRef.current) addEvent(event);
    });
    const unsubLog = onLogRecord((record: LogRecord) => {
      if (!pausedRef.current) addLog(record);
    });
    const unsubStatus = onServerStatus(({ name, connected }: ServerStatus) => {
      setStatuses((prev) => new Map(prev).set(name, connected));
    });
    return () => {
      unsubTool();
      unsubLog();
      unsubStatus();
    };
  }, [addEvent, addLog]);

  const handlePause = () => {
    pausedRef.current = !pausedRef.current;
    setPaused(pausedRef.current);
  };

  const handleClear = () => {
    clearEvents();
    clearLogs();
    void rpc.clear();
  };

  // ── Sidebar ────────────────────────────────────────

  const isSettings = nav.startsWith("settings");

  const sidebar = (
    <div style={{
      width: 200,
      background: "var(--mantine-color-default)",
      borderRight: "1px solid var(--mantine-color-default-border)",
      display: "flex",
      flexDirection: "column",
      height: "100%",
    }}>
      <Text fw={700} size="sm" c="dimmed" px="md" pt="md" pb="xs">bench</Text>

      <Stack gap={2} px="xs">
        <NavLink
          label="Tools"
          leftSection={<IconTools size={16} />}
          active={nav === "tools"}
          onClick={() => setNav("tools")}
          rightSection={events.length > 0 ? <Badge size="xs" color="gray" variant="light">{events.length}</Badge> : null}
        />
        <NavLink
          label="Logs"
          leftSection={<IconFileText size={16} />}
          active={nav === "logs"}
          onClick={() => setNav("logs")}
          rightSection={
            logs.length > 0
              ? <Badge size="xs" color={logs.some((l) => l.level === "error") ? "red" : "gray"} variant="light">{logs.length}</Badge>
              : null
          }
        />
      </Stack>

      <Divider my="xs" mx="xs" />

      <Stack gap={2} px="xs">
        <NavLink
          label="Settings"
          leftSection={<IconSettings size={16} />}
          active={isSettings}
          defaultOpened
          childrenOffset={12}
        >
          <NavLink
            label="General"
            leftSection={<IconSettings size={14} />}
            active={nav === "settings-general"}
            onClick={() => setNav("settings-general")}
          />
          <NavLink
            label="Database"
            leftSection={<IconDatabase size={14} />}
            active={nav === "settings-database"}
            onClick={() => setNav("settings-database")}
          />
          <NavLink
            label="Servers"
            leftSection={<IconServer size={14} />}
            active={nav === "settings-servers"}
            onClick={() => setNav("settings-servers")}
          />
          <NavLink
            label="Roots"
            leftSection={<IconFolder size={14} />}
            active={nav === "settings-roots"}
            onClick={() => setNav("settings-roots")}
          />
        </NavLink>
      </Stack>

      {/* Spacer + instance name + connection badges */}
      <div style={{ flex: 1 }} />
      <Stack gap={4} px="md" pb="md">
        {settings.instanceName && (
          <Badge size="xs" color="violet" variant="light" style={{ alignSelf: "flex-start" }}>
            {settings.instanceName}
          </Badge>
        )}
        {settings.servers.map((s) => (
          <Badge
            key={s.name}
            size="xs"
            color={statuses.get(s.name) ? "teal" : "red"}
            variant="dot"
            style={{ alignSelf: "flex-start" }}
          >
            {s.name}
          </Badge>
        ))}
        {settings.servers.length === 0 && (
          <Text size="xs" c="dimmed">no servers configured</Text>
        )}
      </Stack>
    </div>
  );

  // ── Toolbar ────────────────────────────────────────

  const toolbar = (
    <Group
      px="md" py="xs" justify="space-between"
      style={{
        borderBottom: "1px solid var(--mantine-color-default-border)",
        background: "var(--mantine-color-default)",
        flexShrink: 0,
      }}
    >
      <Text size="xs" c="dimmed">
        {nav === "tools" ? `${events.length} events` : nav === "logs" ? `${logs.length} logs` : ""}
      </Text>
      {(nav === "tools" || nav === "logs") && (
        <Group gap="xs">
          <Tooltip label={paused ? "Resume" : "Pause"}>
            <ActionIcon size="sm" variant="subtle" color="gray" onClick={handlePause}>
              {paused ? <IconPlayerPlay size={14} /> : <IconPlayerPause size={14} />}
            </ActionIcon>
          </Tooltip>
          <Tooltip label="Clear">
            <ActionIcon size="sm" variant="subtle" color="red" onClick={handleClear}>
              <IconTrash size={14} />
            </ActionIcon>
          </Tooltip>
        </Group>
      )}
    </Group>
  );

  // ── Content ────────────────────────────────────────

  // Computed as an expression (not a nested component) so React diffs the
  // subtree across re-renders instead of unmounting it. As a nested function
  // declaration, every App re-render produced a new component identity, which
  // React treats as a different type and remounts — wiping descendant useState
  // (notably EventRow's open flag). Since every incoming event re-renders App,
  // expanded detail panels would collapse on the next tool call.
  let content: ReactNode;
  if (nav === "settings-general") {
    content = <SettingsGeneralPanel settings={settings} onSave={save} />;
  } else if (nav === "settings-database") {
    content = <SettingsDatabasePanel settings={settings} onSave={save} />;
  } else if (nav === "settings-servers") {
    content = <SettingsServersPanel settings={settings} onSave={save} statuses={statuses} />;
  } else if (nav === "settings-roots") {
    content = <SettingsRootsPanel settings={settings} onSave={save} />;
  } else if (nav === "tools") {
    content = events.length === 0
      ? <Empty text="Waiting for tool calls…" />
      : <>{events.map((e) => <EventRow key={e.id} event={e} />)}</>;
  } else {
    content = logs.length === 0
      ? <Empty text={`Waiting for logs on OTLP :${settings.otlpPort}…`} />
      : <>{logs.map((l, i) => <LogRow key={`${l.ts}-${i}`} record={l} />)}</>;
  }

  return (
    <MantineProvider theme={theme} forceColorScheme="light">
      <div style={{
        height: "100vh",
        display: "flex",
        background: "var(--mantine-color-body)",
      }}>
        {sidebar}
        <div style={{ flex: 1, display: "flex", flexDirection: "column", minWidth: 0 }}>
          {toolbar}
          <div style={{ flex: 1, overflowY: "auto" }}>
            {content}
          </div>
        </div>
      </div>
    </MantineProvider>
  );
}

function Empty({ text }: { text: string }) {
  return (
    <div style={{ display: "flex", alignItems: "center", justifyContent: "center", height: "100%", color: "var(--mantine-color-dimmed)", fontSize: 13 }}>
      {text}
    </div>
  );
}
