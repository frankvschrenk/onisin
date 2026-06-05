// state.ts — in-memory map of every Onisin process that has published a
// heartbeat since this ooso instance started. In the Tauri (Pfad C)
// architecture the webview owns this state directly; in the old Electrobun
// build it lived in the bun process and the renderer pulled snapshots. The
// shape and the time-based transitions are unchanged.
//
// State transitions are time-based:
//   live   = last tick <= STALE_MS
//   stale  = STALE_MS < last tick <= GONE_MS  (a few ticks missed)
//   gone   = last tick > GONE_MS              (process probably crashed)
//
// Rows are never deleted — a gone process is more interesting than a silent
// one, and the operator should see who came and went during the session.

/** Payload published on subject status.<service> every 5s. Defined here
 *  (rather than imported from the retired oos-heartbeat-ts package) because
 *  ooso is the only consumer left and the shape is tiny. */
export interface HeartbeatPayload {
	/** Stable node id from the shell's get_identity, or "" if a publisher omits it. */
	nodeId: string;
	/** Short service identifier, e.g. "oosai" or "ooso". */
	service: string;
	/** Semantic version string, e.g. "0.7.0". */
	version: string;
	/** OS process id of the publishing service. */
	pid: number;
	/** Hostname of the machine running the process. */
	host: string;
	/** ISO timestamp when the process started. */
	startedAt: string;
	/** ISO timestamp of this heartbeat tick. */
	ts: string;
}

/** Threshold in ms below which a service is considered live. */
const STALE_MS = 15_000;

/** Threshold in ms above which a service is considered gone. */
const GONE_MS = 60_000;

/** Public derived status for the UI. */
export type ServiceStatus = "live" | "stale" | "gone";

/** One row in the operator table. */
export interface ServiceRow {
	/** Stable key used to keep React renders cheap. */
	key: string;
	/** Most recent heartbeat payload from this process. */
	last: HeartbeatPayload;
	/** Derived status at the moment of read. */
	status: ServiceStatus;
	/** Total heartbeats received since this process appeared. */
	ticks: number;
	/** ISO timestamp of the first heartbeat received from this process. */
	firstSeen: string;
}

const rows = new Map<string, ServiceRow>();

// nodeId is the preferred discriminator because it survives restarts of the
// same binary; the host+pid fallback covers a publisher whose identity is not
// yet initialised when it sends its first heartbeat.
function keyFor(hb: HeartbeatPayload): string {
	return hb.nodeId !== "" ? hb.nodeId : `${hb.service}:${hb.host}:${hb.pid}`;
}

/** ingest is called once per received heartbeat. */
export function ingest(hb: HeartbeatPayload): void {
	const key = keyFor(hb);
	const prev = rows.get(key);
	rows.set(key, {
		key,
		last: hb,
		status: "live",
		ticks: (prev?.ticks ?? 0) + 1,
		firstSeen: prev?.firstSeen ?? hb.ts,
	});
}

/**
 * snapshot returns every row with a freshly computed status field. Sorting is
 * stable: live first, then by service+host so two instances on the same
 * machine stay adjacent.
 */
export function snapshot(now: number = Date.now()): ServiceRow[] {
	const list: ServiceRow[] = [];
	for (const r of rows.values()) {
		const age = now - new Date(r.last.ts).getTime();
		const status: ServiceStatus = age <= STALE_MS ? "live" : age <= GONE_MS ? "stale" : "gone";
		list.push({ ...r, status });
	}
	list.sort((a, b) => {
		const rank = { live: 0, stale: 1, gone: 2 } as const;
		if (rank[a.status] !== rank[b.status]) return rank[a.status] - rank[b.status];
		if (a.last.service !== b.last.service) return a.last.service.localeCompare(b.last.service);
		return a.last.host.localeCompare(b.last.host);
	});
	return list;
}
