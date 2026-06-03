//! NATS request-reply handler for the oosd designer's admin commands.
//!
//! Faithful port of the Bun command-handler's oosd-facing surface:
//! CRUD over oos.domain / oos.view (DSL source rows), the
//! event_type_grammar library, and event_mappings.event_types. The
//! Tauri webview will talk these subjects directly over NATS-over-WS
//! once oosd is migrated, replacing the old Electrobun RPC gateway.
//!
//! Why a queue group: without `oosai-cmd`, NATS fans every request out
//! to *all* subscribers on the subject. For a write handler that means
//! each subscriber runs the INSERT and only one reply reaches the
//! caller — duplicate writes. The queue makes NATS pick exactly one
//! subscriber per request, even with several oosai processes (or a
//! stale + fresh subscriber inside one `--hot` process).
//!
//! DSL boundary (Path A, same line as oosgql): the `*.save` handlers
//! persist the source row for real, but the chunk re-embed is deferred
//! — it needs renderLLMChunk (domain) / parseView+renderViewChunk
//! (view), neither of which is ported to oos-dsls yet. The editor's
//! Save therefore works end-to-end; only the RAG schema row lags until
//! the renderer slice lands and the domain/view backfill re-embeds.

use async_nats::{Client, Subscriber};
use futures::StreamExt;
use serde_json::{json, Value};
use sqlx::{PgPool, Row};

use anyhow::{anyhow, Result};

/// Shared queue group for every command subscription.
const QUEUE: &str = "oosai-cmd";

/// The oosd-facing command subjects. Explicit list rather than an
/// `oos.cmd.>` wildcard so we don't shadow the embedding subjects
/// (served in nats_embed) or oos-only subjects.
const SUBJECTS: &[&str] = &[
    "oos.cmd.domain.list",
    "oos.cmd.domain.load",
    "oos.cmd.domain.save",
    "oos.cmd.domain.delete",
    "oos.cmd.view.list",
    "oos.cmd.view.load",
    "oos.cmd.view.save",
    "oos.cmd.view.delete",
    "oos.cmd.event_type_grammar.list",
    "oos.cmd.event_type_grammar.load",
    "oos.cmd.event_type_grammar.save",
    "oos.cmd.event_type_grammar.save_tags",
    "oos.cmd.event_type_grammar.insert",
    "oos.cmd.event_type_grammar.delete",
    "oos.cmd.event_mappings.list",
    "oos.cmd.event_mappings.set_types",
    "oos.cmd.event.refresh",
];

/// Subscribes to all command subjects in the queue group and spawns a
/// serving task per subject. Returns once the subscriptions are live;
/// the tasks run until the process exits.
pub async fn serve(client: Client, pool: PgPool) -> Result<()> {
    for &subject in SUBJECTS {
        let sub = client
            .queue_subscribe(subject.to_string(), QUEUE.to_string())
            .await?;
        tokio::spawn(run_subject(client.clone(), pool.clone(), sub));
    }
    println!("[oosai] listening on oos.cmd.{{domain,view,event_type_grammar,event_mappings}}.* + event.refresh (queue {QUEUE})");
    Ok(())
}

/// One subscription loop. Each message is dispatched in its own task so
/// a slow query never stalls the subscription or the sibling subjects
/// (same lesson as the old bench `for await` dispatcher).
async fn run_subject(client: Client, pool: PgPool, mut sub: Subscriber) {
    while let Some(msg) = sub.next().await {
        let Some(reply) = msg.reply.clone() else { continue };
        let client = client.clone();
        let pool = pool.clone();
        tokio::spawn(async move {
            let value = match handle(&pool, msg.subject.as_str(), &msg.payload).await {
                Ok(v) => v,
                // Logical + transport errors come back in-band as
                // {ok:false,error}; the old clients tolerate this even
                // on list/load shapes ({ids}/{source} just read undefined).
                Err(e) => json!({ "ok": false, "error": e.to_string() }),
            };
            if let Ok(bytes) = serde_json::to_vec(&value) {
                let _ = client.publish(reply, bytes.into()).await;
            }
        });
    }
}

/// Routes one command to its SQL and returns the reply payload. The
/// reply shapes mirror the Bun handler exactly so the existing oosd
/// frontend types stay valid.
async fn handle(pool: &PgPool, subject: &str, payload: &[u8]) -> Result<Value> {
    let body: Value = if payload.is_empty() {
        json!({})
    } else {
        serde_json::from_slice(payload)?
    };

    Ok(match subject {
        // ── Domain ────────────────────────────────────────────────
        "oos.cmd.domain.list" => json!({ "ids": list_ids(pool, "oos.domain").await? }),
        "oos.cmd.domain.load" => {
            json!({ "source": load_source(pool, "oos.domain", str_field(&body, "id")?).await? })
        }
        "oos.cmd.domain.save" => {
            let id = str_field(&body, "id")?;
            save_source(pool, "oos.domain", id, str_field(&body, "source")?).await?;
            // Path A: source persisted; chunk re-embed waits on the
            // domain renderer (renderLLMChunk) port.
            println!("[oosai/cmd] domain.save {id}: source stored, chunk embed deferred (renderer not ported)");
            json!({ "ok": true })
        }
        "oos.cmd.domain.delete" => {
            let id = str_field(&body, "id")?;
            delete_source(pool, "oos.domain", id).await?;
            delete_chunk(pool, "oos.oos_domain_schema", "context_name", id).await?;
            json!({ "ok": true })
        }

        // ── View ──────────────────────────────────────────────────
        "oos.cmd.view.list" => json!({ "ids": list_ids(pool, "oos.view").await? }),
        "oos.cmd.view.load" => {
            json!({ "source": load_source(pool, "oos.view", str_field(&body, "id")?).await? })
        }
        "oos.cmd.view.save" => {
            let id = str_field(&body, "id")?;
            save_source(pool, "oos.view", id, str_field(&body, "source")?).await?;
            println!("[oosai/cmd] view.save {id}: source stored, chunk embed deferred (view parser not ported)");
            json!({ "ok": true })
        }
        "oos.cmd.view.delete" => {
            let id = str_field(&body, "id")?;
            delete_source(pool, "oos.view", id).await?;
            delete_chunk(pool, "oos.oos_view_schema", "id", id).await?;
            json!({ "ok": true })
        }

        // ── Event type grammar (mapping-independent library) ──────
        "oos.cmd.event_type_grammar.list" => {
            let rows = sqlx::query(
                "SELECT id::int8 AS id, name, source, tags::text AS tags, created_at::text AS created_at
                 FROM public.event_type_grammar ORDER BY name",
            )
            .fetch_all(pool)
            .await?;
            let mut types = Vec::with_capacity(rows.len());
            for r in &rows {
                types.push(json!({
                    "id":         r.try_get::<i64, _>("id")?,
                    "name":       r.try_get::<String, _>("name")?,
                    "source":     r.try_get::<Option<String>, _>("source")?,
                    "tags":       json_text_or_empty(r.try_get::<Option<String>, _>("tags")?),
                    "created_at": r.try_get::<String, _>("created_at")?,
                }));
            }
            json!({ "types": types })
        }
        "oos.cmd.event_type_grammar.load" => {
            let name = str_field(&body, "name")?;
            let row = sqlx::query(
                "SELECT source, tags::text AS tags FROM public.event_type_grammar WHERE name = $1",
            )
            .bind(name)
            .fetch_optional(pool)
            .await?;
            match row {
                Some(r) => json!({
                    "source": r.try_get::<Option<String>, _>("source")?,
                    "tags":   json_text_or_empty(r.try_get::<Option<String>, _>("tags")?),
                }),
                None => json!({ "source": Value::Null, "tags": [] }),
            }
        }
        "oos.cmd.event_type_grammar.save" => {
            sqlx::query("UPDATE public.event_type_grammar SET source = $1 WHERE name = $2")
                .bind(str_field(&body, "source")?)
                .bind(str_field(&body, "name")?)
                .execute(pool)
                .await?;
            json!({ "ok": true })
        }
        "oos.cmd.event_type_grammar.save_tags" => {
            let tags = serde_json::to_string(body.get("tags").unwrap_or(&json!([])))?;
            sqlx::query("UPDATE public.event_type_grammar SET tags = $1::jsonb WHERE name = $2")
                .bind(tags)
                .bind(str_field(&body, "name")?)
                .execute(pool)
                .await?;
            json!({ "ok": true })
        }
        "oos.cmd.event_type_grammar.insert" => {
            sqlx::query("INSERT INTO public.event_type_grammar (name, source) VALUES ($1, '')")
                .bind(str_field(&body, "name")?)
                .execute(pool)
                .await?;
            json!({ "ok": true })
        }
        "oos.cmd.event_type_grammar.delete" => {
            sqlx::query("DELETE FROM public.event_type_grammar WHERE name = $1")
                .bind(str_field(&body, "name")?)
                .execute(pool)
                .await?;
            json!({ "ok": true })
        }

        // ── Event mappings ────────────────────────────────────────
        "oos.cmd.event_mappings.list" => {
            let rows = sqlx::query(
                "SELECT id::int8 AS id, name, source_schema, source_table, source_text_field,
                        source_id_field, notify_channel, target_schema, target_table,
                        enabled, event_types::text AS event_types
                 FROM public.event_mappings ORDER BY name",
            )
            .fetch_all(pool)
            .await?;
            let mut mappings = Vec::with_capacity(rows.len());
            for r in &rows {
                mappings.push(json!({
                    "id":                r.try_get::<i64, _>("id")?,
                    "name":              r.try_get::<Option<String>, _>("name")?,
                    "source_schema":     r.try_get::<Option<String>, _>("source_schema")?,
                    "source_table":      r.try_get::<Option<String>, _>("source_table")?,
                    "source_text_field": r.try_get::<Option<String>, _>("source_text_field")?,
                    "source_id_field":   r.try_get::<Option<String>, _>("source_id_field")?,
                    "notify_channel":    r.try_get::<Option<String>, _>("notify_channel")?,
                    "target_schema":     r.try_get::<Option<String>, _>("target_schema")?,
                    "target_table":      r.try_get::<Option<String>, _>("target_table")?,
                    "enabled":           r.try_get::<bool, _>("enabled")?,
                    "event_types":       json_text_or_empty(r.try_get::<Option<String>, _>("event_types")?),
                }));
            }
            json!({ "mappings": mappings })
        }
        "oos.cmd.event_mappings.set_types" => {
            let event_types = serde_json::to_string(body.get("eventTypes").unwrap_or(&json!([])))?;
            let mapping_id = body
                .get("mappingId")
                .and_then(Value::as_i64)
                .ok_or_else(|| anyhow!("missing or non-numeric field: mappingId"))?;
            sqlx::query("UPDATE public.event_mappings SET event_types = $1::jsonb WHERE id = $2")
                .bind(event_types)
                .bind(mapping_id)
                .execute(pool)
                .await?;
            json!({ "ok": true })
        }

        // ── Event refresh ─────────────────────────────────────────
        // oosd fires this after admin DDL so the listener re-subscribes
        // to new channels. The event listener isn't ported yet, so this
        // is an honest no-op ack — oosd treats refresh as best-effort.
        "oos.cmd.event.refresh" => {
            println!("[oosai/cmd] event.refresh: listener not ported yet — no-op ack");
            json!({ "ok": true })
        }

        other => return Err(anyhow!("unhandled command subject: {other}")),
    })
}

// ─── Generic source-row helpers (oos.domain / oos.view share shape) ───

/// Lists the `id` column of a source table, alphabetically. The table
/// name is a trusted compile-time literal from the match above — never
/// caller input — so interpolating it is safe.
async fn list_ids(pool: &PgPool, table: &str) -> Result<Vec<String>> {
    let rows = sqlx::query(&format!("SELECT id FROM {table} ORDER BY id"))
        .fetch_all(pool)
        .await?;
    let mut ids = Vec::with_capacity(rows.len());
    for r in &rows {
        ids.push(r.try_get::<String, _>("id")?);
    }
    Ok(ids)
}

/// Returns the source text for one id, or JSON null when absent.
async fn load_source(pool: &PgPool, table: &str, id: &str) -> Result<Value> {
    let row = sqlx::query(&format!("SELECT source FROM {table} WHERE id = $1"))
        .bind(id)
        .fetch_optional(pool)
        .await?;
    Ok(match row {
        Some(r) => Value::String(r.try_get::<String, _>("source")?),
        None => Value::Null,
    })
}

/// Upserts a source row, bumping updated_at on conflict — matching the
/// Bun handler's INSERT ... ON CONFLICT (id) DO UPDATE.
async fn save_source(pool: &PgPool, table: &str, id: &str, source: &str) -> Result<()> {
    sqlx::query(&format!(
        "INSERT INTO {table} (id, source) VALUES ($1, $2)
         ON CONFLICT (id) DO UPDATE SET source = EXCLUDED.source, updated_at = now()"
    ))
    .bind(id)
    .bind(source)
    .execute(pool)
    .await?;
    Ok(())
}

/// Deletes a source row by id.
async fn delete_source(pool: &PgPool, table: &str, id: &str) -> Result<()> {
    sqlx::query(&format!("DELETE FROM {table} WHERE id = $1"))
        .bind(id)
        .execute(pool)
        .await?;
    Ok(())
}

/// Deletes the matching schema (embedding) row. Idempotent: a missing
/// row is fine, which is what we want after the source is already gone.
async fn delete_chunk(pool: &PgPool, table: &str, key_col: &str, key: &str) -> Result<()> {
    sqlx::query(&format!("DELETE FROM {table} WHERE {key_col} = $1"))
        .bind(key)
        .execute(pool)
        .await?;
    Ok(())
}

// ─── Small JSON helpers ───────────────────────────────────────────────

/// Extracts a required string field from the request body.
fn str_field<'a>(body: &'a Value, key: &str) -> Result<&'a str> {
    body.get(key)
        .and_then(Value::as_str)
        .ok_or_else(|| anyhow!("missing or non-string field: {key}"))
}

/// Parses a jsonb-as-text column into a JSON value, falling back to an
/// empty array. jsonb is read as `::text` and parsed here because our
/// sqlx build omits the `json` feature (the same text-roundtrip pattern
/// oosgql uses for its runtime-typed columns).
fn json_text_or_empty(text: Option<String>) -> Value {
    match text {
        Some(s) => serde_json::from_str(&s).unwrap_or_else(|_| json!([])),
        None => json!([]),
    }
}
