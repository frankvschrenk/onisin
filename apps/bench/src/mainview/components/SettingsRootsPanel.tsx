// src/mainview/components/SettingsRootsPanel.tsx — allowed filesystem roots.
//
// Add/remove the directories bench is allowed to access. On save the backend
// rebuilds its RootRegistry immediately (part of the reconnect cycle).

import { useState } from "react";
import {
  Stack, Title, Text, TextInput, Button, Group,
  ActionIcon, Table, Tooltip, Paper,
} from "@mantine/core";
import { IconPlus, IconTrash } from "@tabler/icons-react";
import type { BenchSettings } from "../store/settings";

interface Props {
  settings: BenchSettings;
  onSave: (next: BenchSettings) => Promise<void>;
}

export function SettingsRootsPanel({ settings, onSave }: Props) {
  const [path, setPath] = useState("");
  const [savedAt, setSavedAt] = useState<string | null>(null);

  const add = async () => {
    const trimPath = path.trim();
    if (!trimPath) return;
    if (settings.roots.some((r) => r.path === trimPath)) return;
    await onSave({ ...settings, roots: [...settings.roots, { path: trimPath }] });
    setPath("");
    setSavedAt(new Date().toLocaleTimeString());
  };

  const remove = async (p: string) => {
    await onSave({ ...settings, roots: settings.roots.filter((r) => r.path !== p) });
    setSavedAt(new Date().toLocaleTimeString());
  };

  return (
    <Paper p="lg" radius={0} style={{ height: "100%", overflow: "auto" }}>
      <Stack gap="lg" maw={640}>
        <div>
          <Title order={3}>Allowed Roots</Title>
          <Text size="sm" c="dimmed" mt={4}>
            Directories bench may read and write. Paths outside these roots are
            rejected. Add a directory to grant access on the fly.
          </Text>
        </div>

        {settings.roots.length > 0 && (
          <Table withRowBorders={false} verticalSpacing="xs">
            <Table.Thead>
              <Table.Tr>
                <Table.Th>Path</Table.Th>
                <Table.Th />
              </Table.Tr>
            </Table.Thead>
            <Table.Tbody>
              {settings.roots.map((r) => (
                <Table.Tr key={r.path}>
                  <Table.Td>
                    <Text size="sm" ff="monospace">{r.path}</Text>
                  </Table.Td>
                  <Table.Td>
                    <Tooltip label="Remove">
                      <ActionIcon
                        size="sm"
                        variant="subtle"
                        color="red"
                        onClick={() => void remove(r.path)}
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
          <Text size="sm" fw={500}>Add directory</Text>
          <Group align="flex-end" gap="sm">
            <TextInput
              label="Path"
              placeholder="/Users/frank/repro/onisin"
              value={path}
              onChange={(e) => setPath(e.currentTarget.value)}
              style={{ flex: 1 }}
            />
            <Button
              leftSection={<IconPlus size={14} />}
              onClick={() => void add()}
              disabled={!path.trim()}
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
