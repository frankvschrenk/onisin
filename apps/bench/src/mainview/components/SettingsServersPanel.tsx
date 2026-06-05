// src/mainview/components/SettingsServersPanel.tsx — NATS server list.
//
// Add/remove named NATS servers. Each entry has a name (e.g. "macos-local")
// and a URL. Saving reconnects the dispatcher to the new set immediately.

import { useState } from "react";
import {
  Stack, Title, Text, TextInput, Button, Group,
  ActionIcon, Table, Badge, Tooltip, Paper,
} from "@mantine/core";
import { IconPlus, IconTrash } from "@tabler/icons-react";
import type { BenchSettings, NatsServer } from "../store/settings";

interface Props {
  settings: BenchSettings;
  onSave: (next: BenchSettings) => Promise<void>;
  statuses: Map<string, boolean>;
}

export function SettingsServersPanel({ settings, onSave, statuses }: Props) {
  const [name, setName] = useState("");
  const [url, setUrl] = useState("nats://localhost:4222");
  const [savedAt, setSavedAt] = useState<string | null>(null);

  const add = async () => {
    const trimName = name.trim();
    const trimUrl = url.trim();
    if (!trimName || !trimUrl) return;
    const servers: NatsServer[] = [
      ...settings.servers.filter((s) => s.name !== trimName),
      { name: trimName, url: trimUrl },
    ];
    await onSave({ ...settings, servers });
    setName("");
    setUrl("nats://localhost:4222");
    setSavedAt(new Date().toLocaleTimeString());
  };

  const remove = async (serverName: string) => {
    const servers = settings.servers.filter((s) => s.name !== serverName);
    await onSave({ ...settings, servers });
    setSavedAt(new Date().toLocaleTimeString());
  };

  return (
    <Paper p="lg" radius={0} style={{ height: "100%", overflow: "auto" }}>
      <Stack gap="lg" maw={640}>
        <div>
          <Title order={3}>NATS Servers</Title>
          <Text size="sm" c="dimmed" mt={4}>
            Named NATS servers bench can connect to. bench-nats uses the server
            name to route tool calls.
          </Text>
        </div>

        {settings.servers.length > 0 && (
          <Table withRowBorders={false} verticalSpacing="xs">
            <Table.Thead>
              <Table.Tr>
                <Table.Th>Name</Table.Th>
                <Table.Th>URL</Table.Th>
                <Table.Th>Status</Table.Th>
                <Table.Th />
              </Table.Tr>
            </Table.Thead>
            <Table.Tbody>
              {settings.servers.map((s) => (
                <Table.Tr key={s.name}>
                  <Table.Td>
                    <Text size="sm" fw={500}>{s.name}</Text>
                  </Table.Td>
                  <Table.Td>
                    <Text size="sm" c="dimmed">{s.url}</Text>
                  </Table.Td>
                  <Table.Td>
                    <Badge
                      size="xs"
                      color={statuses.get(s.name) ? "teal" : "red"}
                      variant="dot"
                    >
                      {statuses.get(s.name) ? "connected" : "disconnected"}
                    </Badge>
                  </Table.Td>
                  <Table.Td>
                    <Tooltip label="Remove">
                      <ActionIcon
                        size="sm"
                        variant="subtle"
                        color="red"
                        onClick={() => void remove(s.name)}
                      >
                        <IconTrash size={14} />
                      </ActionIcon>
                    </Tooltip>
                  </Table.Td>
                </Table.Tr>
              ))}
            </Table.Tbody>
          </Table>
        )}

        <Stack gap="sm">
          <Text size="sm" fw={500}>Add server</Text>
          <Group align="flex-end" gap="sm">
            <TextInput
              label="Name"
              placeholder="macos-local"
              value={name}
              onChange={(e) => setName(e.currentTarget.value)}
              style={{ flex: "0 0 160px" }}
            />
            <TextInput
              label="URL"
              placeholder="nats://localhost:4222"
              value={url}
              onChange={(e) => setUrl(e.currentTarget.value)}
              style={{ flex: 1 }}
            />
            <Button
              leftSection={<IconPlus size={14} />}
              onClick={() => void add()}
              disabled={!name.trim() || !url.trim()}
            >
              Add
            </Button>
          </Group>
        </Stack>

        {savedAt && (
          <Text size="xs" c="dimmed">Saved at {savedAt}</Text>
        )}
      </Stack>
    </Paper>
  );
}
