// nats.ts — NATS-over-WebSocket client for the ooso webview (Pfad C).
//
// The operator console talks to the bus directly: it subscribes to status.*
// for heartbeats, requests oos.cmd.<service>.env.show on demand, and publishes
// its own status.ooso heartbeat. The ws URL is reconfigurable at runtime
// because pointing ooso at a different bus (local vs a remote ws listener) is
// the whole point of an operator console.

import { connect, StringCodec, type NatsConnection } from "nats.ws";

// The dev nats-server's websocket listener (server.conf websocket{ port:4223 }).
// A webview cannot reach the tcp nats:// endpoint, so this is the ws default;
// setWsUrl swaps it and recycles the connection.
const DEFAULT_WS_URL = "ws://localhost:4223";
const REQUEST_TIMEOUT_MS = 3_000;

const sc = StringCodec();

let wsUrl = DEFAULT_WS_URL;
let conn: NatsConnection | null = null;
let connecting: Promise<NatsConnection> | null = null;

/** Returns the currently configured ws bus URL. */
export function getWsUrl(): string {
	return wsUrl;
}

/**
 * setWsUrl points ooso at a different ws bus. It drops the current connection
 * so the next getNats() redials against the new URL; callers that hold a
 * status.* subscription must re-establish it (App re-runs its NATS effect when
 * the URL changes).
 */
export async function setWsUrl(url: string): Promise<void> {
	if (url === wsUrl) return;
	wsUrl = url;
	const stale = conn;
	conn = null;
	connecting = null;
	if (stale) {
		try { await stale.close(); } catch { /* already gone */ }
	}
}

/**
 * getNats returns the shared connection, opening it on first use. The connect
 * promise is cached so concurrent first callers share one dial. nats.ws owns
 * reconnection (retry forever) so a nats-server blip on kickstart heals itself.
 */
export async function getNats(): Promise<NatsConnection> {
	if (conn) return conn;
	if (!connecting) {
		const url = wsUrl;
		connecting = connect({
			servers: url,
			reconnect: true,
			maxReconnectAttempts: -1,
			// Wait through retries on the very first dial instead of rejecting, so
			// an ooso started before the bus is up connects once it appears (the
			// old Electrobun ooso achieved this with its own retry loop).
			waitOnFirstConnect: true,
			timeout: 3_000,
		})
			.then((nc) => {
				// A setWsUrl() during the dial wins: discard this connection.
				if (url !== wsUrl) { void nc.close(); throw new Error("ws url changed during connect"); }
				conn = nc;
				connecting = null;
				void nc.closed().then(() => { if (conn === nc) conn = null; });
				return nc;
			})
			.catch((err) => {
				connecting = null;
				throw err;
			});
	}
	return connecting;
}

/** Request-reply with JSON in/out (StringCodec over JSON, wire-compatible with
 *  the Rust service handlers). Used for oos.cmd.<service>.env.show. */
export async function natsRequest<T = unknown>(subject: string, payload: unknown = {}): Promise<T> {
	const nc = await getNats();
	const msg = await nc.request(subject, sc.encode(JSON.stringify(payload)), { timeout: REQUEST_TIMEOUT_MS });
	return JSON.parse(sc.decode(msg.data)) as T;
}

/** Fire-and-forget publish (JSON). Used for ooso's own status.ooso heartbeat. */
export async function natsPublish(subject: string, payload: unknown): Promise<void> {
	const nc = await getNats();
	nc.publish(subject, sc.encode(JSON.stringify(payload)));
}

/**
 * subscribeStatus subscribes to the status.* wildcard and calls onMessage for
 * every successfully parsed payload. Returns an unsubscribe handle. Malformed
 * payloads are ignored rather than throwing out of the async iterator.
 */
export async function subscribeStatus<T = unknown>(
	onMessage: (value: T) => void,
): Promise<() => void> {
	const nc = await getNats();
	const sub = nc.subscribe("status.*");
	void (async () => {
		for await (const msg of sub) {
			try {
				onMessage(JSON.parse(sc.decode(msg.data)) as T);
			} catch {
				// malformed payload — ignore
			}
		}
	})();
	return () => { sub.unsubscribe(); };
}
