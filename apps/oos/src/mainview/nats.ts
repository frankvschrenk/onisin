// nats.ts — NATS-over-WebSocket client for the oosd webview.
//
// Replaces the Bun gateway's nats-rpc.ts. In the Tauri architecture the
// webview speaks to the bus directly (Pfad C): Request-Reply for command
// subjects, JetStream for KV. The Bun process that used to proxy these
// calls is gone, so this module is the single owner of the connection.

import {
	connect,
	JSONCodec,
	StringCodec,
	type NatsConnection,
} from "nats.ws";

// Hardcoded for now: the websocket listener added to the dev nats-server
// (server.conf websocket{ port:4223 }). settings.natsUrl is still a tcp
// nats:// URL used by the headless services, not reachable from a webview,
// so the ws endpoint is configured separately until settings carry it.
const WS_URL = "ws://localhost:4223";
const REQUEST_TIMEOUT_MS = 10_000;

const sc = StringCodec();
const jc = JSONCodec();

let conn: NatsConnection | null = null;
let connecting: Promise<NatsConnection> | null = null;

/**
 * getNats returns the shared connection, opening it on first use. The
 * connect promise is cached so concurrent first callers share one dial
 * instead of racing several sockets open.
 */
export async function getNats(): Promise<NatsConnection> {
	if (conn) return conn;
	if (!connecting) {
		connecting = connect({
			servers: WS_URL,
			// reconnect keeps the bus alive across nats-server restarts
			// (the ws listener blips ~1-2s on a kickstart); -1 = retry forever.
			reconnect: true,
			maxReconnectAttempts: -1,
			timeout: 3_000,
		})
			.then((nc) => {
				conn = nc;
				connecting = null;
				// Reset on close so the next getNats redials instead of
				// handing out a dead connection.
				void nc.closed().then(() => {
					conn = null;
				});
				return nc;
			})
			.catch((err) => {
				connecting = null;
				throw err;
			});
	}
	return connecting;
}

/**
 * natsRequest sends a JSON Request-Reply and parses the JSON reply. The
 * wire format matches the old Bun gateway (StringCodec over JSON), so the
 * Rust command handlers reply identically whether the caller is Bun or
 * the webview.
 */
export async function natsRequest<T = unknown>(
	subject: string,
	payload: unknown = {},
	timeoutMs: number = REQUEST_TIMEOUT_MS,
): Promise<T> {
	const nc = await getNats();
	const msg = await nc.request(subject, sc.encode(JSON.stringify(payload)), {
		timeout: timeoutMs,
	});
	return JSON.parse(sc.decode(msg.data)) as T;
}

/** JSON codec shared with the KV module (JetStream stores JSON values). */
export function jsonCodec() {
	return jc;
}

/**
 * pingBus resolves when the websocket bus is reachable. Replaces the old
 * raw-TCP probe to :4222, which a webview cannot perform; the meaningful
 * health signal in Pfad C is whether the ws transport itself is up.
 */
export async function pingBus(): Promise<void> {
	const nc = await getNats();
	await nc.flush();
}
