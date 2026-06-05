// src/mainview/components/LogRow.tsx — one log record row.
//
// Click anywhere on the row to expand the full message and fields.

import { useState } from "react";
import { Group, Text, Badge, Collapse, Code, Box } from "@mantine/core";
import type { LogRecord } from "../types";

const LEVEL_COLOR: Record<string, string> = {
  error: "red", warn: "orange", info: "blue", debug: "gray",
};

function fmtTime(iso: string): string {
  return new Date(iso).toLocaleTimeString("de-DE", {
    hour: "2-digit", minute: "2-digit", second: "2-digit",
    fractionalSecondDigits: 3,
  });
}

export function LogRow({ record }: { record: LogRecord }) {
  const [open, setOpen] = useState(false);

  const hasDetail = !!record.fields || record.message.length > 80;

  return (
    <div
      style={{ borderBottom: "1px solid #1a1a1a", cursor: hasDetail ? "pointer" : "default" }}
      onClick={() => hasDetail && setOpen((o) => !o)}
    >
      {/* Summary row */}
      <Group px="md" py={4} gap="sm" wrap="nowrap">
        <Text size="xs" c="dimmed" style={{ minWidth: 90, flexShrink: 0 }}>
          {fmtTime(record.ts)}
        </Text>
        <Badge
          size="xs"
          color={LEVEL_COLOR[record.level] ?? "gray"}
          variant="light"
          style={{ minWidth: 50, flexShrink: 0 }}
        >
          {record.level.slice(0, 3).toUpperCase()}
        </Badge>
        <Text size="xs" style={{ color: "#56b6c2", minWidth: 80, flexShrink: 0 }}>
          {record.service}
        </Text>
        <Text size="xs" c="dimmed" style={{ minWidth: 140, flexShrink: 0, fontFamily: "monospace" }}>
          {record.source}
        </Text>
        <Text
          size="xs"
          style={{
            color:        "#abb2bf",
            overflow:     "hidden",
            textOverflow: "ellipsis",
            whiteSpace:   "nowrap",
            flex:         1,
          }}
        >
          {record.message}
        </Text>
        {hasDetail && (
          <Text size="xs" c="dimmed" style={{ flexShrink: 0, userSelect: "none" }}>
            {open ? "▲" : "▼"}
          </Text>
        )}
      </Group>

      {/* Detail panel */}
      <Collapse expanded={open}>
        <Box px="md" pb="sm">
          <Code
            block
            style={{
              fontSize:   11,
              background: "#161616",
              border:     "1px solid #2a2a2a",
              whiteSpace: "pre-wrap",
              wordBreak:  "break-word",
            }}
          >
            {record.message}
          </Code>
          {record.fields && (
            <Code
              block
              mt="xs"
              style={{
                fontSize:   11,
                background: "#161616",
                border:     "1px solid #2a2a2a",
              }}
            >
              {JSON.stringify(record.fields, null, 2)}
            </Code>
          )}
        </Box>
      </Collapse>
    </div>
  );
}
