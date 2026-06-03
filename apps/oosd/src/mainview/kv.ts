// kv.ts — JetStream KV operations for the oosd webview.
//
// Ported almost verbatim from the Bun gateway's kv-rpc.ts. nats.ws speaks
// JetStream directly, so the KV admin panel needs no backend command
// subject — the webview is the KV client. Values are stored as JSON.

import { getNats, jsonCodec } from "./nats";

export interface KvBucketInfo {
	name: string;
	keys: number;
}

export interface KvEntry {
	key: string;
	value: unknown;
	revision: number;
}

/** Names and key counts of all KV buckets, sorted by name. */
export async function listKvBuckets(): Promise<KvBucketInfo[]> {
	const nc = await getNats();
	const jsm = await nc.jetstreamManager();
	const streams = await jsm.streams.list().next();
	const buckets: KvBucketInfo[] = [];
	for (const s of streams) {
		if (!s.config.name.startsWith("KV_")) continue;
		buckets.push({ name: s.config.name.slice(3), keys: s.state.messages });
	}
	return buckets.sort((a, b) => a.name.localeCompare(b.name));
}

/** All current (non-deleted) entries in a bucket, sorted by key. */
export async function listKvKeys(bucket: string): Promise<KvEntry[]> {
	const nc = await getNats();
	const kv = await nc.jetstream().views.kv(bucket);
	const jc = jsonCodec();
	const entries: KvEntry[] = [];
	try {
		// keys() returns only live entries and closes the iterator when
		// done — safe on empty buckets, unlike watch().
		const keys = await kv.keys();
		for await (const key of keys) {
			const e = await kv.get(key);
			if (!e || e.operation === "DEL" || e.operation === "PURGE") continue;
			let value: unknown = null;
			try {
				value = jc.decode(e.value);
			} catch {
				value = null;
			}
			entries.push({ key, value, revision: e.revision });
		}
	} catch (err) {
		// Some NATS versions throw "no keys found" on an empty bucket.
		if (!String(err).includes("no keys")) throw err;
	}
	return entries.sort((a, b) => a.key.localeCompare(b.key));
}

/** Write a key. Creates the bucket (history=5) if absent. */
export async function putKvEntry(bucket: string, key: string, value: unknown): Promise<void> {
	const nc = await getNats();
	const kv = await nc.jetstream().views.kv(bucket, { history: 5 });
	await kv.put(key, jsonCodec().encode(value));
}

/** Remove a key from a bucket. */
export async function deleteKvEntry(bucket: string, key: string): Promise<void> {
	const nc = await getNats();
	const kv = await nc.jetstream().views.kv(bucket);
	await kv.delete(key);
}

/** Create an empty bucket (history=5). Idempotent. */
export async function createKvBucket(bucket: string): Promise<void> {
	const nc = await getNats();
	await nc.jetstream().views.kv(bucket, { history: 5 });
}

/** Purge and delete a bucket entirely. */
export async function deleteKvBucket(bucket: string): Promise<void> {
	const nc = await getNats();
	const jsm = await nc.jetstreamManager();
	await jsm.streams.delete(`KV_${bucket}`);
}
