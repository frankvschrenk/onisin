// src/mainview/components/SettingsDatabasePanel.tsx — PostgreSQL DSN config.
//
// bench needs two pieces to talk to PostgreSQL:
//   1. DSN — host, port, credentials; no dbname segment (per-machine half).
//   2. App database name — typically "onisin". Used by pg.query/pg.exec/pg.reset;
//      memory.* and task.* always use the hard-wired "bench" DB regardless.
// The panel previews both resulting URLs so the user sees where each tool group
// will connect.

import { useEffect, useState } from "react";
import {
  Stack, Title, Text, TextInput, Button, Group, Paper,
  Code, Alert, Divider,
} from "@mantine/core";
import { IconDatabase, IconInfoCircle } from "@tabler/icons-react";
import type { BenchSettings } from "../store/settings";

interface Props {
  settings: BenchSettings;
  onSave: (next: BenchSettings) => Promise<void>;
}

// withDbname appends or replaces the dbname segment of a libpq DSN. Mirrors
// rewrite_database() on the Rust side; a tiny duplicate so the panel can render
// a live preview without a round-trip.
function withDbname(dsn: string, db: string): string {
  if (!dsn || !db) return "";
  try {
    const [base, query] = dsn.split("?") as [string, string | undefined];
    const schemeEnd = base.indexOf("://") + 3;
    if (schemeEnd < 3) return "";
    const after = base.slice(schemeEnd);
    const slashIdx = after.indexOf("/");
    const head = slashIdx === -1
      ? base + "/" + db
      : base.slice(0, schemeEnd) + after.slice(0, slashIdx + 1) + db;
    return query !== undefined ? `${head}?${query}` : head;
  } catch {
    return "";
  }
}

export function SettingsDatabasePanel({ settings, onSave }: Props) {
  const [dsn, setDsn] = useState(settings.dsn);
  const [appDatabase, setAppDatabase] = useState(settings.appDatabase);
  const [savedAt, setSavedAt] = useState<string | null>(null);

  useEffect(() => {
    setDsn(settings.dsn);
    setAppDatabase(settings.appDatabase);
  }, [settings]);

  const submit = async () => {
    await onSave({
      ...settings,
      dsn: dsn.trim(),
      appDatabase: appDatabase.trim(),
    });
    setSavedAt(new Date().toLocaleTimeString());
  };

  const benchUrl = withDbname(dsn.trim(), "bench");
  const appUrl = withDbname(dsn.trim(), appDatabase.trim());

  return (
    <Paper p="lg" radius={0} style={{ height: "100%", overflow: "auto" }}>
      <Stack gap="lg" maw={720}>
        <div>
          <Title order={3}>Database</Title>
          <Text size="sm" c="dimmed" mt={4}>
            PostgreSQL connection used by the <Code>pg.*</Code>,{" "}
            <Code>memory.*</Code> and <Code>task.*</Code> tools.
          </Text>
        </div>

        <Alert color="gray" variant="light" icon={<IconInfoCircle size={16} />}>
          <Text size="sm">
            The DSN is host-only — no database name. bench appends{" "}
            <Code>/bench</Code> for its own working memory, and{" "}
            <Code>/{appDatabase || "<app-database>"}</Code> for the{" "}
            <Code>pg.*</Code> tools.
          </Text>
        </Alert>

        <Stack gap="sm">
          <TextInput
            label="DSN"
            description="libpq-style connection string without a dbname segment."
            placeholder="postgres://postgres:demo@localhost:5432?sslmode=disable"
            value={dsn}
            onChange={(e) => setDsn(e.currentTarget.value)}
            leftSection={<IconDatabase size={16} />}
            styles={{ input: { fontFamily: "ui-monospace, monospace", fontSize: 13 } }}
          />
          <TextInput
            label="Application database"
            description="Database name the pg.* tools operate on. Typically 'onisin'."
            placeholder="onisin"
            value={appDatabase}
            onChange={(e) => setAppDatabase(e.currentTarget.value)}
            styles={{ input: { fontFamily: "ui-monospace, monospace", fontSize: 13 } }}
          />
        </Stack>

        <Divider label="Resolved URLs" labelPosition="left" />

        <Stack gap={4}>
          <Text size="xs" c="dimmed">
            memory + task → <Code>{benchUrl || "— (set DSN)"}</Code>
          </Text>
          <Text size="xs" c="dimmed">
            pg.* → <Code>{appUrl || "— (set DSN and application database)"}</Code>
          </Text>
        </Stack>

        <Group justify="flex-end" align="center" gap="md">
          {savedAt && <Text size="xs" c="dimmed">Saved at {savedAt}</Text>}
          <Button onClick={() => void submit()}>Save</Button>
        </Group>
      </Stack>
    </Paper>
  );
}
