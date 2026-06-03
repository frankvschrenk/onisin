//! oos.domain.changed listener: keeps the DomainStore fresh.
//!
//! When oosd saves a domain, oosai publishes the domain id on
//! oos.domain.changed. We reload just that row so the permissions
//! handler (and, later, the GraphQL schema) reflect the edit without a
//! full table re-parse. Payload is the bare domain id as a string.

use std::sync::Arc;

use async_nats::{Client, Subscriber};
use futures::StreamExt;

use crate::domain_store::DomainStore;

/// Subscribes to oos.domain.changed and spawns the reload loop. Returns
/// once the subscription is live.
pub async fn start(client: Client, store: Arc<DomainStore>) -> anyhow::Result<()> {
    let sub = client.subscribe("oos.domain.changed").await?;
    tokio::spawn(run(sub, store));
    println!("[oosgql] subscribed to oos.domain.changed");
    Ok(())
}

async fn run(mut sub: Subscriber, store: Arc<DomainStore>) {
    while let Some(msg) = sub.next().await {
        let id = String::from_utf8_lossy(&msg.payload).trim().to_string();
        if id.is_empty() {
            continue;
        }
        if let Err(e) = store.load_one(&id).await {
            eprintln!("[oosgql] reload of domain {id} failed: {e}");
        }
    }
}
