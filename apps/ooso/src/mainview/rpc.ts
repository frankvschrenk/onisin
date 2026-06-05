// rpc.ts — the webview seam. In the old Electrobun ooso this proxied to the
// bun process; in Pfad C it talks to the bus directly. Method names are kept
// (rpc.getServiceEnv) so ServiceTable barely changes. env.show is a NATS
// request; the heartbeat table is fed by the status.* subscription in App.tsx,
// not through here.

import { natsRequest } from "./nats";

/** Wire payload for oos.cmd.<service>.env.show (one entry per resolved var). */
export interface EnvShowReply {
	service: string;
	entries: Array<{ key: string; value: string; source: string }>;
}

/** Discriminated result so the caller can render an error inline without a try. */
export type EnvResult =
	| { ok: true; reply: EnvShowReply }
	| { ok: false; error: string };

export const rpc = {
	/**
	 * getServiceEnv requests oos.cmd.<service>.env.show and validates the shape.
	 * A non-responding service (usually one that isn't running) surfaces as
	 * { ok: false } with the transport error message, which the panel shows.
	 */
	async getServiceEnv({ service }: { service: string }): Promise<EnvResult> {
		try {
			const reply = await natsRequest<EnvShowReply>(`oos.cmd.${service}.env.show`);
			if (!reply || !Array.isArray(reply.entries)) {
				return { ok: false, error: `malformed reply on oos.cmd.${service}.env.show` };
			}
			return { ok: true, reply };
		} catch (err) {
			return { ok: false, error: err instanceof Error ? err.message : String(err) };
		}
	},
};
