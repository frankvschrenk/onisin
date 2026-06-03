//! NATS request-reply handlers for oosgql.
//!
//! Wire-compatible subjects with the Bun original:
//!
//!   oos.cmd.gql.view        {name}            -> {name, source} | {error}
//!   oos.cmd.gql.domain      {name}            -> {name, source} | {error}
//!   oos.cmd.gql.query       {query, ...}      -> {error: not yet migrated}
//!   oos.cmd.gql.mutation    {query, role}     -> {error: not yet migrated}
//!   oos.cmd.gql.permissions {role}            -> {permissions: [{domain, actions}]}
//!
//! view, domain and permissions are real. view/domain are pure source
//! passthroughs (SELECT id, source FROM oos.view|oos.domain WHERE id =
//! $1). permissions reads the parsed DomainDefs from the DomainStore —
//! it needs the domain DSL parser but not a GraphQL engine. query and
//! mutation do need a dynamic GraphQL engine, not ported yet; rather
//! than fake them they reply with an explicit not-yet-migrated error so
//! a caller fails loudly instead of silently getting wrong data.
//!
//! Each subject runs in its own task and each message in a further
//! spawned task, so a slow DB round-trip never blocks the subscription
//! loop or the other subjects — same lesson as the old bench dispatcher.

use std::sync::Arc;

use async_nats::{Client, Subscriber};
use futures::StreamExt;
use serde::Deserialize;
use serde_json::json;
use sqlx::{PgPool, Row};

use crate::domain_store::DomainStore;

#[derive(Deserialize)]
struct NameReq {
    name: String,
}

#[derive(Deserialize)]
struct RoleReq {
    role: String,
}

/// Subscribes to every oos.cmd.gql.* subject and spawns its serving
/// task. Returns once the subscriptions are live; the tasks run until
/// the process exits. `pool` is None when Postgres was unreachable at
/// boot, in which case view/domain reply with a database-unavailable
/// error.
pub async fn serve(client: Client, pool: Option<PgPool>, store: Arc<DomainStore>) -> anyhow::Result<()> {
    let view = client.subscribe("oos.cmd.gql.view").await?;
    let domain = client.subscribe("oos.cmd.gql.domain").await?;
    let query = client.subscribe("oos.cmd.gql.query").await?;
    let mutation = client.subscribe("oos.cmd.gql.mutation").await?;
    let permissions = client.subscribe("oos.cmd.gql.permissions").await?;
    let data_query = client.subscribe("oos.cmd.data.query").await?;
    let data_mutate = client.subscribe("oos.cmd.data.mutate").await?;

    tokio::spawn(handle_source(client.clone(), pool.clone(), view, "oos.view", "view"));
    tokio::spawn(handle_source(client.clone(), pool.clone(), domain, "oos.domain", "domain"));
    tokio::spawn(handle_permissions(client.clone(), store.clone(), permissions));
    tokio::spawn(handle_data_query(client.clone(), pool.clone(), store.clone(), data_query));
    tokio::spawn(handle_data_mutate(client.clone(), pool.clone(), store, data_mutate));
    tokio::spawn(handle_unmigrated(client.clone(), query, "oos.cmd.gql.query"));
    tokio::spawn(handle_unmigrated(client.clone(), mutation, "oos.cmd.gql.mutation"));

    println!(
        "[oosgql] listening on oos.cmd.data.{{query,mutate}} + oos.cmd.gql.{{view,domain,permissions}} (real); oos.cmd.gql.{{query,mutation}} superseded by data.* (not migrated)"
    );
    Ok(())
}

/// Serves oos.cmd.data.query — the schema-bound row/options query that
/// replaces GraphQL reads. Validation and SQL live in `crate::data`.
async fn handle_data_query(
    client: Client,
    pool: Option<PgPool>,
    store: Arc<DomainStore>,
    mut sub: Subscriber,
) {
    while let Some(msg) = sub.next().await {
        let Some(reply) = msg.reply.clone() else { continue };
        let client = client.clone();
        let pool = pool.clone();
        let store = store.clone();
        tokio::spawn(async move {
            let payload = crate::data::query(&store, pool.as_ref(), &msg.payload).await;
            reply_json(&client, reply, &payload).await;
        });
    }
}

/// Serves oos.cmd.data.mutate — the permission-gated insert/update/delete
/// that replaces GraphQL mutations.
async fn handle_data_mutate(
    client: Client,
    pool: Option<PgPool>,
    store: Arc<DomainStore>,
    mut sub: Subscriber,
) {
    while let Some(msg) = sub.next().await {
        let Some(reply) = msg.reply.clone() else { continue };
        let client = client.clone();
        let pool = pool.clone();
        let store = store.clone();
        tokio::spawn(async move {
            let payload = crate::data::mutate(&store, pool.as_ref(), &msg.payload).await;
            reply_json(&client, reply, &payload).await;
        });
    }
}

/// Serves a source-passthrough subject. `table` is a trusted literal
/// (oos.view / oos.domain); the user-supplied name is always bound, so
/// interpolating the table name carries no injection risk.
async fn handle_source(
    client: Client,
    pool: Option<PgPool>,
    mut sub: Subscriber,
    table: &'static str,
    label: &'static str,
) {
    while let Some(msg) = sub.next().await {
        let Some(reply) = msg.reply.clone() else { continue };
        let client = client.clone();
        let pool = pool.clone();
        tokio::spawn(async move {
            let payload = match serde_json::from_slice::<NameReq>(&msg.payload) {
                Ok(req) if is_valid_name(&req.name) => match &pool {
                    Some(p) => match fetch_source(p, table, &req.name).await {
                        Ok(Some((id, source))) => json!({ "name": id, "source": source }),
                        Ok(None) => json!({ "error": format!("{label} \"{}\" not found", req.name) }),
                        Err(e) => json!({ "error": e.to_string() }),
                    },
                    None => json!({ "error": "database unavailable" }),
                },
                Ok(_) => json!({ "error": format!("invalid {label} name") }),
                Err(e) => json!({ "error": format!("bad request: {e}") }),
            };
            reply_json(&client, reply, &payload).await;
        });
    }
}

/// Serves a subject whose real implementation is gated on the DSL parser
/// and GraphQL engine. Replies with a clear not-yet-migrated error.
async fn handle_unmigrated(client: Client, mut sub: Subscriber, subject: &'static str) {
    while let Some(msg) = sub.next().await {
        let Some(reply) = msg.reply.clone() else { continue };
        let client = client.clone();
        tokio::spawn(async move {
            let payload = json!({
                "error": format!(
                    "{subject} is not yet migrated to oosgql-rs — needs the domain DSL parser and dynamic GraphQL engine"
                )
            });
            reply_json(&client, reply, &payload).await;
        });
    }
}

/// Serves oos.cmd.gql.permissions: for the given role, returns every
/// domain with the actions that role is granted on it. Reads parsed
/// DomainDefs from the store — no DB hit, no GraphQL engine. A domain
/// the role has no entry on yields an empty action list, matching the
/// Bun handler so the Settings → Permissions tab renders unchanged.
async fn handle_permissions(client: Client, store: Arc<DomainStore>, mut sub: Subscriber) {
    while let Some(msg) = sub.next().await {
        let Some(reply) = msg.reply.clone() else { continue };
        let client = client.clone();
        let store = store.clone();
        tokio::spawn(async move {
            let payload = match serde_json::from_slice::<RoleReq>(&msg.payload) {
                Ok(req) if !req.role.trim().is_empty() => {
                    let perms: Vec<_> = store
                        .snapshot()
                        .into_iter()
                        .map(|d| {
                            // Enum -> lowercase string array via serde, so the
                            // wire shape stays ["read","write",...] as before.
                            let actions = d
                                .permissions
                                .iter()
                                .find(|p| p.role == req.role)
                                .map(|p| serde_json::to_value(&p.actions).unwrap_or_else(|_| json!([])))
                                .unwrap_or_else(|| json!([]));
                            json!({ "domain": d.name, "actions": actions })
                        })
                        .collect();
                    json!({ "permissions": perms })
                }
                Ok(_) => json!({ "error": "role required" }),
                Err(e) => json!({ "error": format!("bad request: {e}") }),
            };
            reply_json(&client, reply, &payload).await;
        });
    }
}

/// Reads one (id, source) row from a source table by primary key.
async fn fetch_source(
    pool: &PgPool,
    table: &str,
    name: &str,
) -> anyhow::Result<Option<(String, String)>> {
    let sql = format!("SELECT id, source FROM {table} WHERE id = $1");
    let row = sqlx::query(&sql).bind(name).fetch_optional(pool).await?;
    Ok(row.map(|r| (r.get::<String, _>("id"), r.get::<String, _>("source"))))
}

/// A name is a primary key in oos.view / oos.domain: non-empty and
/// bounded so a pathological payload can't reach the database.
fn is_valid_name(name: &str) -> bool {
    !name.is_empty() && name.len() <= 200
}

/// Encodes `payload` as JSON and publishes it to the reply subject.
async fn reply_json(client: &Client, reply: async_nats::Subject, payload: &serde_json::Value) {
    match serde_json::to_vec(payload) {
        Ok(bytes) => {
            let _ = client.publish(reply, bytes.into()).await;
        }
        Err(e) => eprintln!("[oosgql] reply encode failed: {e}"),
    }
}
