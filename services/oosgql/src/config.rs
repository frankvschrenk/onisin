//! Bootstrap and KV-sourced configuration for oosgql.
//!
//! Same shape as oosai: only natsUrl + kvBucket come from the
//! environment; host/port/pgUrl are read from the JetStream KV bucket
//! `oos-gql/config` via the shared resolver in oos_svc::env, falling
//! back to the demo defaults the Bun service used and healing the
//! bucket so it is immediately editable in the oosd KV Designer.
//!
//! pgUrl is marked secret so env.show masks it on the wire; it sits in
//! plaintext in KV for now (field-level encryption is a later layer).

use async_nats::Client;
use oos_svc::env::{resolve_env, resolve_kv_specs, EnvEntry, Spec};

/// The two inputs that cannot come from KV.
pub struct Bootstrap {
    /// NATS URL used to reach the broker and the KV store.
    pub nats_url: String,
    /// KV bucket holding this service's config object.
    pub bucket: String,
    /// Provenance entries for natsUrl + kvBucket, fed into env.show.
    pub entries: Vec<EnvEntry>,
}

impl Bootstrap {
    /// Resolves the env-sourced inputs. Must run before NATS connects.
    pub fn from_env() -> Self {
        let nats = resolve_env("natsUrl", &["OOSGQL_NATS_URL"], "nats://localhost:4222", false);
        let bucket = resolve_env("kvBucket", &["OOSGQL_KV_BUCKET"], "oos-gql", false);
        Bootstrap {
            nats_url: nats.value.clone(),
            bucket: bucket.value.clone(),
            entries: vec![nats, bucket],
        }
    }
}

/// Resolved server configuration, frozen at startup.
#[allow(dead_code)] // host/port serve the HTTP health probe, added in a later slice
pub struct Config {
    pub host: String,
    pub port: u16,
    pub pg_url: String,
    /// Bootstrap + KV entries in declaration order, for env.show.
    pub entries: Vec<EnvEntry>,
}

/// Field specs: (kv key, default, secret). Mirrors the Bun config.ts
/// defaults so a heal stays byte-compatible with existing buckets.
const SPECS: &[Spec] = &[
    ("host", "localhost", false),
    ("port", "4000", false),
    ("pgUrl", "postgres://postgres:demo@localhost:5432/onisin", true),
];

impl Config {
    /// Reads `oos-gql/config` from KV, resolves every field against the
    /// demo defaults, heals the bucket, and carries provenance through.
    /// NATS must already be connected.
    pub async fn load(client: &Client, boot: &Bootstrap) -> Self {
        let resolved = resolve_kv_specs(client, &boot.bucket, &boot.entries, SPECS).await;
        Config {
            host: resolved.get("host"),
            port: resolved.get("port").parse().unwrap_or(4000),
            pg_url: resolved.get("pgUrl"),
            entries: resolved.entries,
        }
    }
}
