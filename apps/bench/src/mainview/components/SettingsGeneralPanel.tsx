// src/mainview/components/SettingsGeneralPanel.tsx — general bench settings.
//
// Instance name identifies this bench on NATS: it subscribes to "bench.>"
// (shared) AND "{name}.bench.>" (targeted), so bench-nats can address it
// specifically by prefixing subjects. OTLP port is the local HTTP receiver
// for OTel log records (a change needs an app restart: the receiver binds
// once at boot).

import { useEffect, useState } from "react";
import {
  Stack, Title, Text, TextInput, NumberInput,
  Button, Group, Paper, Divider,
} from "@mantine/core";
import type { BenchSettings } from "../store/settings";

interface Props {
  settings: BenchSettings;
  onSave: (next: BenchSettings) => Promise<void>;
}

export function SettingsGeneralPanel({ settings, onSave }: Props) {
  const [instanceName, setInstanceName] = useState(settings.instanceName);
  const [otlpPort, setOtlpPort] = useState<number>(settings.otlpPort);
  const [savedAt, setSavedAt] = useState<string | null>(null);

  // Re-sync when settings change externally.
  useEffect(() => {
    setInstanceName(settings.instanceName);
    setOtlpPort(settings.otlpPort);
  }, [settings]);

  const submit = async () => {
    await onSave({ ...settings, instanceName: instanceName.trim(), otlpPort });
    setSavedAt(new Date().toLocaleTimeString());
  };

  return (
    <Paper p="lg" radius={0} style={{ height: "100%", overflow: "auto" }}>
      <Stack gap="lg" maw={640}>
        <div>
          <Title order={3}>General</Title>
          <Text size="sm" c="dimmed" mt={4}>
            Identity and local service ports for this bench instance.
          </Text>
        </div>

        <Stack gap="sm">
          <TextInput
            label="Instance name"
            description={
              'Identifies this bench on NATS. Set e.g. "macos" or "linux". ' +
              "bench-nats can then address this instance specifically via " +
              '"{name}.bench.*" subjects, or omit the prefix to reach any ' +
              "available bench."
            }
            placeholder="macos"
            value={instanceName}
            onChange={(e) => setInstanceName(e.currentTarget.value)}
          />
        </Stack>

        <Divider label="Local services" labelPosition="left" />

        <Stack gap="sm">
          <NumberInput
            label="OTLP port"
            description="HTTP port for the local OTel log receiver (oosl → bench). Takes effect after an app restart."
            value={otlpPort}
            onChange={(v) => setOtlpPort(Number(v))}
            min={1024}
            max={65535}
          />
        </Stack>

        <Group justify="flex-end" align="center" gap="md">
          {savedAt && <Text size="xs" c="dimmed">Saved at {savedAt}</Text>}
          <Button onClick={() => void submit()}>Save</Button>
        </Group>
      </Stack>
    </Paper>
  );
}
