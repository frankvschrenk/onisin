//! Bootstrap and KV-sourced configuration for oosai.
//!
//! Two inputs come from the environment — the NATS URL (needed to reach
//! KV at all) and the bucket name. Everything else is read from the
//! JetStream KV bucket `oos-ai/config` at boot via the shared resolver
//! in oos-svc, falling back to the same demo defaults the Bun service
//! used, then healed back so a fresh or out-of-date bucket becomes fully
//! populated and editable in the oosd Designer — without ever clobbering
//! an operator's edit. Provenance rides along as EnvEntry for env.show.
//!
//! Only the spec list and the typed Config struct are oosai-specific;
//! the load/resolve/heal mechanics live in oos_svc::env.

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
        let nats = resolve_env("natsUrl", &["OOSAI_NATS_URL"], "nats://localhost:4222", false);
        let bucket = resolve_env("kvBucket", &["OOSAI_KV_BUCKET"], "oos-ai", false);
        Bootstrap {
            nats_url: nats.value.clone(),
            bucket: bucket.value.clone(),
            entries: vec![nats, bucket],
        }
    }
}

/// Resolved server configuration, frozen at startup.
#[allow(dead_code)] // host/port/pg_url/llm are consumed by later slices (pgvector store, pipeline runner)
pub struct Config {
    pub host: String,
    pub port: u16,
    pub pg_url: String,
    pub embed: EmbedConfig,
    pub llm: LlmConfig,
    /// Bootstrap + KV entries in declaration order, for env.show.
    pub entries: Vec<EnvEntry>,
}

pub struct EmbedConfig {
    /// OpenAI-compatible base URL; `/v1` appended by the client if absent.
    pub base_url: String,
    pub api_key: String,
    pub model: String,
}

#[allow(dead_code)] // consumed by the pipeline-runner slice
pub struct LlmConfig {
    pub base_url: String,
    pub api_key: String,
    pub model: String,
    pub timeout_ms: u64,
}

/// Field specs: (kv key, default, secret). Declaration order is the
/// order env.show reports them in.
const SPECS: &[Spec] = &[
    ("host", "localhost", false),
    ("port", "4100", false),
    ("pgUrl", "postgres://postgres:demo@localhost:5432/onisin", true),
    ("embed.baseUrl", "http://localhost:11434/v1", false),
    ("embed.apiKey", "", true),
    ("embed.model", "bge-m3:latest", false),
    ("llm.baseUrl", "http://localhost:11434", false),
    ("llm.apiKey", "", true),
    ("llm.model", "gemma4:26b", false),
    ("llm.timeoutMs", "600000", false),
];

impl Config {
    /// Reads `oos-ai/config` from KV, resolves every field against the
    /// demo defaults, heals the bucket, and carries provenance through.
    /// NATS must already be connected.
    pub async fn load(client: &Client, boot: &Bootstrap) -> Self {
        let resolved = resolve_kv_specs(client, &boot.bucket, &boot.entries, SPECS).await;
        Config {
            host: resolved.get("host"),
            port: resolved.get("port").parse().unwrap_or(4100),
            pg_url: resolved.get("pgUrl"),
            embed: EmbedConfig {
                base_url: resolved.get("embed.baseUrl"),
                api_key: resolved.get("embed.apiKey"),
                model: resolved.get("embed.model"),
            },
            llm: LlmConfig {
                base_url: resolved.get("llm.baseUrl"),
                api_key: resolved.get("llm.apiKey"),
                model: resolved.get("llm.model"),
                timeout_ms: resolved.get("llm.timeoutMs").parse().unwrap_or(600_000),
            },
            entries: resolved.entries,
        }
    }
}
