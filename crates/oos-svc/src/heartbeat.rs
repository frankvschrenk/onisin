//! Heartbeat publisher.
//!
//! Every Onisin process ticks `status.<service>` so the ooso operator
//! console knows it is alive. Wire-compatible with oos-heartbeat-ts:
//! payload {nodeId, service, version, pid, host, startedAt, ts}, 5s
//! interval, first tick immediate.
//!
//! nodeId is the stable Ed25519 id from oos-node-id-ts; that package is
//! not ported yet, so callers pass "" for now and ooso falls back to
//! grouping by host+pid — exactly the documented degradation.

use std::time::Duration;

use async_nats::Client;
use serde_json::json;

/// Spawns the heartbeat loop. Runs until the process exits; the first
/// tick fires immediately so the dashboard reflects the service right
/// away.
pub fn start(client: Client, service: &str, version: &str, node_id: &str) {
    let subject = format!("status.{service}");
    let service = service.to_string();
    let version = version.to_string();
    let node_id = node_id.to_string();
    let host = gethostname::gethostname().to_string_lossy().into_owned();
    let started_at = chrono::Utc::now().to_rfc3339();

    tokio::spawn(async move {
        let mut ticker = tokio::time::interval(Duration::from_secs(5));
        loop {
            ticker.tick().await;
            let payload = json!({
                "nodeId": node_id,
                "service": service,
                "version": version,
                "pid": std::process::id(),
                "host": host,
                "startedAt": started_at,
                "ts": chrono::Utc::now().to_rfc3339(),
            });
            if let Ok(bytes) = serde_json::to_vec(&payload) {
                let _ = client.publish(subject.clone(), bytes.into()).await;
            }
        }
    });
}
