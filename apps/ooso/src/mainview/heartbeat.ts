// heartbeat.ts — ooso publishes its own status.ooso heartbeat.
//
// A console that does not show itself is a console you cannot trust, so ooso
// ticks on status.ooso every 5s just like every other Onisin process. The
// payload matches the HeartbeatPayload other services publish; the immutable
// fields come from the native identity, ts/startedAt from the webview clock.

import { natsPublish } from "./nats";
import type { Identity } from "./identity";
import type { HeartbeatPayload } from "./state";

const SERVICE = "ooso";
const INTERVAL_MS = 5_000;

/**
 * startHeartbeat begins publishing status.ooso every 5s and returns a stop
 * handle. The first tick fires immediately so ooso's own row appears at once.
 * Publish errors (bus draining / reconnecting) are swallowed.
 */
export function startHeartbeat(id: Identity): () => void {
	const startedAt = new Date().toISOString();
	const tick = (): void => {
		const payload: HeartbeatPayload = {
			nodeId: id.nodeId,
			service: SERVICE,
			version: id.version,
			pid: id.pid,
			host: id.host,
			startedAt,
			ts: new Date().toISOString(),
		};
		void natsPublish(`status.${SERVICE}`, payload).catch(() => { /* draining — ignore */ });
	};
	tick();
	const handle = setInterval(tick, INTERVAL_MS);
	return () => clearInterval(handle);
}
