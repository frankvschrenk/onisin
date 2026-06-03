//! JetStream KV access for service configuration.
//!
//! The backend services keep their config as a single flat JSON object
//! under the key `config` in a per-service bucket (oos-ai / oos-gql /
//! ...), values stored as strings. Mirrors oos-env-ts loadKvConfig /
//! healKvConfig: a missing bucket, missing key, or disabled JetStream
//! all resolve to an empty map rather than an error, so a service
//! always boots on its hardcoded defaults.

use std::collections::HashMap;

use async_nats::jetstream::{self, kv::Config as KvConfig};
use async_nats::Client;

/// Single key under which a service stores its whole config object.
const CONFIG_KEY: &str = "config";
/// History depth matching what oosiam/oos-env-ts created the buckets with.
const HISTORY: i64 = 5;

/// Binds the bucket, creating it on demand. Returns None when JetStream
/// is unreachable so callers degrade to defaults instead of failing.
async fn bind(client: &Client, bucket: &str) -> Option<jetstream::kv::Store> {
    let js = jetstream::new(client.clone());
    if let Ok(store) = js.get_key_value(bucket).await {
        return Some(store);
    }
    js.create_key_value(KvConfig {
        bucket: bucket.to_string(),
        history: HISTORY,
        ..Default::default()
    })
    .await
    .ok()
}

/// Reads `<bucket>/config` into a flat string map. The bool reports
/// whether the key actually existed, so the caller can tell a first
/// boot apart from a populated bucket (and decide whether to heal).
pub async fn load_config_map(client: &Client, bucket: &str) -> (HashMap<String, String>, bool) {
    let Some(store) = bind(client, bucket).await else {
        return (HashMap::new(), false);
    };
    match store.get(CONFIG_KEY).await {
        Ok(Some(bytes)) if !bytes.is_empty() => {
            match serde_json::from_slice::<serde_json::Map<String, serde_json::Value>>(&bytes) {
                Ok(obj) => (obj.iter().map(|(k, v)| (k.clone(), coerce(v))).collect(), true),
                Err(_) => (HashMap::new(), false),
            }
        }
        _ => (HashMap::new(), false),
    }
}

/// Writes a flat string map to `<bucket>/config`. Best-effort: a
/// failure (JetStream off) is swallowed by the caller. Stored as
/// strings — exactly what the oosd KV Designer shows — so a heal stays
/// byte-compatible with what the Bun services wrote.
pub async fn put_config_map(
    client: &Client,
    bucket: &str,
    map: &HashMap<String, String>,
) -> Result<(), async_nats::Error> {
    let store = bind(client, bucket)
        .await
        .ok_or_else(|| async_nats::Error::from("jetstream kv unavailable"))?;
    let bytes = serde_json::to_vec(map)?;
    store.put(CONFIG_KEY, bytes.into()).await?;
    Ok(())
}

/// Coerces a JSON value to the string form resolveKv uses: strings
/// verbatim, numbers/bools via their literal, null to empty.
fn coerce(v: &serde_json::Value) -> String {
    match v {
        serde_json::Value::String(s) => s.clone(),
        serde_json::Value::Null => String::new(),
        other => other.to_string(),
    }
}
