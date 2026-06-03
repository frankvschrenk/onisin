//! Service configuration primitives shared by every Onisin backend.
//!
//! Each service keeps its whole config as one flat JSON object under
//! `<bucket>/config` in JetStream KV, values stored as strings — exactly
//! what the oosd KV Designer edits. The duplicated part across services
//! is the resolve-against-defaults-then-heal dance; that lives here as
//! `resolve_kv_specs`. What stays per-service is only the spec list and
//! the typed Config struct each one maps the resolved values into.
//!
//! Provenance rides along: every resolved value carries an `EnvEntry`
//! recording whether it came from env, KV, a default, or runtime, so the
//! ooso operator console can show it via env.show.

use std::collections::HashMap;

use async_nats::Client;

use crate::kv;

/// One resolved configuration value plus where it came from. `source`
/// is "env:<NAME>", "kv:<bucket>", "default", or "runtime"; secret values
/// are masked on the wire by env_show, never here — the service itself
/// still needs the real value.
#[derive(Clone)]
pub struct EnvEntry {
    pub key: String,
    pub value: String,
    pub source: String,
    pub secret: bool,
}

impl EnvEntry {
    /// Constructs an entry for a value produced at runtime (e.g. a probed
    /// embedding dimension) rather than read from env or KV.
    pub fn runtime(key: &str, value: String) -> Self {
        EnvEntry { key: key.to_string(), value, source: "runtime".to_string(), secret: false }
    }
}

/// One KV-backed field: (key, default, secret). Declaration order in a
/// service's spec list is the order env.show reports the fields in.
pub type Spec = (&'static str, &'static str, bool);

/// The resolved configuration: entries for env.show plus a value lookup
/// the service maps into its own typed struct.
pub struct ResolvedConfig {
    /// Bootstrap + KV entries in declaration order, for env.show.
    pub entries: Vec<EnvEntry>,
    values: HashMap<String, String>,
}

impl ResolvedConfig {
    /// Resolved value for `key`, or the empty string when absent. Absence
    /// only happens if a service reads a key it never declared in specs.
    pub fn get(&self, key: &str) -> String {
        self.values.get(key).cloned().unwrap_or_default()
    }
}

/// First non-empty env var in `names`, else `fallback`. Records the
/// winning source. Mirrors oos-env-ts resolve.
pub fn resolve_env(key: &str, names: &[&str], fallback: &str, secret: bool) -> EnvEntry {
    for name in names {
        if let Ok(v) = std::env::var(name) {
            if !v.is_empty() {
                return EnvEntry { key: key.into(), value: v, source: format!("env:{name}"), secret };
            }
        }
    }
    EnvEntry { key: key.into(), value: fallback.into(), source: "default".into(), secret }
}

/// KV value when present (empty string counts as set), else fallback.
/// Mirrors oos-env-ts resolveKv.
fn resolve_kv(map: &HashMap<String, String>, bucket: &str, key: &str, fallback: &str, secret: bool) -> EnvEntry {
    match map.get(key) {
        Some(v) => EnvEntry { key: key.into(), value: v.clone(), source: format!("kv:{bucket}"), secret },
        None => EnvEntry { key: key.into(), value: fallback.into(), source: "default".into(), secret },
    }
}

/// Reads `<bucket>/config`, resolves every spec against its default,
/// seeds/heals the bucket, and returns the entries plus a value lookup.
/// `bootstrap` entries (natsUrl, kvBucket) are prepended so env.show
/// reports them first. NATS must already be connected.
///
/// Healing only writes when the bucket was absent or a declared key was
/// missing, so it never bumps the revision on a fully-populated bucket
/// and never clobbers an operator's edit. A write failure (JetStream
/// off) is logged and swallowed — the service runs on resolved defaults.
pub async fn resolve_kv_specs(
    client: &Client,
    bucket: &str,
    bootstrap: &[EnvEntry],
    specs: &[Spec],
) -> ResolvedConfig {
    let (map, existed) = kv::load_config_map(client, bucket).await;

    let kv_entries: Vec<EnvEntry> = specs
        .iter()
        .map(|(key, default, secret)| resolve_kv(&map, bucket, key, default, *secret))
        .collect();

    let missing = kv_entries.iter().any(|e| !map.contains_key(&e.key));
    if !existed || missing {
        let blob: HashMap<String, String> =
            kv_entries.iter().map(|e| (e.key.clone(), e.value.clone())).collect();
        if let Err(e) = kv::put_config_map(client, bucket, &blob).await {
            eprintln!("[oos-svc] kv seed/heal skipped: {e}");
        }
    }

    let mut entries = bootstrap.to_vec();
    entries.extend(kv_entries.iter().cloned());
    let values = kv_entries.into_iter().map(|e| (e.key, e.value)).collect();

    ResolvedConfig { entries, values }
}
