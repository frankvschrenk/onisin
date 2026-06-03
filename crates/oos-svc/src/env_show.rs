//! Serves oos.cmd.<service>.env.show with config provenance.
//!
//! The ooso operator console expands a service row to call this and
//! display where each value came from (kv / env / default / runtime).
//! Wire-compatible with oos-env-ts startEnvShow: reply is
//! {service, entries:[{key, value, source}]}; secret values are masked
//! to "***" here, never sent in clear.

use async_nats::{Client, Subscriber};
use futures::StreamExt;
use serde_json::json;

use crate::env::EnvEntry;

/// Subscribes on oos.cmd.<service>.env.show and spawns the reply loop.
/// Returns once the subscription is live.
pub async fn serve(client: Client, service: &str, entries: Vec<EnvEntry>) -> anyhow::Result<()> {
    let subject = format!("oos.cmd.{service}.env.show");
    let sub = client.subscribe(subject).await?;
    let service = service.to_string();
    tokio::spawn(handle(client, sub, service, entries));
    Ok(())
}

async fn handle(client: Client, mut sub: Subscriber, service: String, entries: Vec<EnvEntry>) {
    while let Some(msg) = sub.next().await {
        let Some(reply) = msg.reply.clone() else { continue };
        let masked: Vec<_> = entries
            .iter()
            .map(|e| json!({ "key": e.key, "value": mask(e), "source": e.source }))
            .collect();
        let payload = json!({ "service": service, "entries": masked });
        if let Ok(bytes) = serde_json::to_vec(&payload) {
            let _ = client.publish(reply, bytes.into()).await;
        }
    }
}

/// Masks a secret entry's value on the wire. Empty stays empty so an
/// operator can still tell a configured key from a blank one.
fn mask(e: &EnvEntry) -> String {
    if e.secret && !e.value.is_empty() {
        "***".to_string()
    } else {
        e.value.clone()
    }
}
