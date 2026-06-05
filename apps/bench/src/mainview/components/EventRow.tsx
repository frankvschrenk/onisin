// src/mainview/components/EventRow.tsx — one tool-call row.

import { useState } from "react";
import { Group, Text, Badge, Collapse, Code } from "@mantine/core";
import type { ToolEvent } from "../types";

const STATUS_COLOR = { ok: "teal", error: "red" } as const;

const TOOL_COLOR: Record<string, string> = {
  "bench.fs.read":          "#61afef",
  "bench.fs.read_many":     "#61afef",
  "bench.fs.write":         "#e5c07b",
  "bench.fs.edit":          "#e5c07b",
  "bench.fs.append":        "#e5c07b",
  "bench.fs.list":          "#56b6c2",
  "bench.fs.tree":          "#56b6c2",
  "bench.search.search":    "#c678dd",
  "bench.exec.exec":        "#e06c75",
  "bench.exec.exec_start":  "#e06c75",
  "bench.exec.exec_read":   "#e06c75",
  "bench.exec.exec_stop":   "#e06c75",
  "bench.git.status":       "#98c379",
  "bench.git.diff":         "#98c379",
  "bench.git.commit":       "#98c379",
  "bench.git.push":         "#98c379",
  "bench.memory.write":     "#d19a66",
  "bench.memory.search":    "#d19a66",
  "bench.memory.list":      "#d19a66",
  "bench.task.start":       "#be5046",
  "bench.task.note":        "#be5046",
  "bench.task.resume":      "#be5046",
  "bench.pg.query":         "#abb2bf",
  "bench.pg.exec":          "#abb2bf",
};

function toolColor(name: string): string {
  return TOOL_COLOR[name] ?? "#abb2bf";
}

function fmtDuration(ms: number): string {
  return ms < 1000 ? `${ms}ms` : `${(ms / 1000).toFixed(1)}s`;
}

function fmtSize(bytes: number): string {
  return bytes < 1024 ? `${bytes}b` : `${(bytes / 1024).toFixed(1)}kb`;
}

function fmtTime(iso: string): string {
  return new Date(iso).toLocaleTimeString("de-DE", {
    hour: "2-digit", minute: "2-digit", second: "2-digit",
    fractionalSecondDigits: 3,
  });
}

export function EventRow({ event }: { event: ToolEvent }) {
  const [open, setOpen] = useState(false);

  return (
    <div
      style={{ borderBottom: "1px solid #1a1a1a", cursor: "pointer" }}
      onClick={() => setOpen((o) => !o)}
    >
      <Group px="md" py={4} gap="sm" wrap="nowrap">
        <Text size="xs" c="dimmed" style={{ minWidth: 90, flexShrink: 0 }}>
          {fmtTime(event.ts)}
        </Text>
        <Text size="xs" fw={600} style={{ color: toolColor(event.tool), minWidth: 200, flexShrink: 0 }}>
          {event.tool}
        </Text>
        <Badge size="xs" color={STATUS_COLOR[event.status]} variant="light" style={{ flexShrink: 0 }}>
          {event.status}
        </Badge>
        <Text size="xs" c="dimmed" style={{ minWidth: 50, flexShrink: 0 }}>{fmtDuration(event.durationMs)}</Text>
        <Text size="xs" c="dimmed" style={{ minWidth: 50, flexShrink: 0 }}>{fmtSize(event.resultSize)}</Text>
        {event.error && (
          <Text size="xs" c="red" style={{ overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>
            {event.error}
          </Text>
        )}
      </Group>
      <Collapse expanded={open}>
        {/* Stop click propagation so clicking inside the expanded panel doesn't
            trigger the row-level toggle and collapse it out from under the user. */}
        <div onClick={(e) => e.stopPropagation()}>
          <Code
            block
            style={{
              margin:     "0 12px 8px",
              fontSize:   11,
              background: "var(--mantine-color-gray-1)",
              color:      "var(--mantine-color-gray-9)",
              border:     "1px solid var(--mantine-color-gray-3)",
              maxHeight:  300,
              overflow:   "auto",
            }}
          >
            {JSON.stringify(event.args, null, 2)}
          </Code>
        </div>
      </Collapse>
    </div>
  );
}
