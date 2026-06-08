//! Bootstrap configuration for oosagent.
//!
//! The agent is unusual among the services: its LLM connection (base
//! url, key, model) and tuning arrive in every turn payload from the
//! webview (AgentSettings/AgentTuning), so there is nothing model-
//! related to read from KV. Only natsUrl is needed to reach the bus;
//! kvBucket is carried purely so env.show reports the same provenance
//! shape as the sibling services.

use oos_svc::env::{resolve_env, EnvEntry};

/// The env-sourced inputs the agent needs before NATS connects.
pub struct Bootstrap {
    /// NATS URL used to reach the broker.
    pub nats_url: String,
    /// Provenance entries (natsUrl + kvBucket), fed into env.show.
    pub entries: Vec<EnvEntry>,
}

impl Bootstrap {
    /// Resolves the env-sourced inputs. Must run before NATS connects.
    pub fn from_env() -> Self {
        let nats = resolve_env("natsUrl", &["OOSAGENT_NATS_URL"], "nats://localhost:4222", false);
        let bucket = resolve_env("kvBucket", &["OOSAGENT_KV_BUCKET"], "oos-agent", false);
        Bootstrap {
            nats_url: nats.value.clone(),
            entries: vec![nats, bucket],
        }
    }
}
