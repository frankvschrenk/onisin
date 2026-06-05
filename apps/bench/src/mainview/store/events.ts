// src/mainview/store/events.ts — in-memory event and log lists.

import { useCallback, useState } from "react";
import type { LogRecord, ToolEvent } from "../types";

export const MAX_EVENTS = 500;

export function useEventStore() {
  const [events, setEvents] = useState<ToolEvent[]>([]);
  const addEvent = useCallback((e: ToolEvent) => {
    setEvents((prev) => [e, ...prev].slice(0, MAX_EVENTS));
  }, []);
  const clear = useCallback(() => setEvents([]), []);
  return { events, addEvent, clear };
}

export function useLogStore() {
  const [logs, setLogs] = useState<LogRecord[]>([]);
  const addLog = useCallback((r: LogRecord) => {
    setLogs((prev) => [r, ...prev].slice(0, MAX_EVENTS));
  }, []);
  const clear = useCallback(() => setLogs([]), []);
  return { logs, addLog, clear };
}
