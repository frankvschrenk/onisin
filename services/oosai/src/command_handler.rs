//! NATS request-reply handler for the oosd designer's admin commands.
//!
//! Faithful port of the Bun command-handler's oosd-facing surface:
//! CRUD over oos.domain / oos.view (DSL source rows), the
//! event_type_grammar library, and event_mappings.event_types. The
//! Tauri webview will talk these subjects directly over NATS-over-WS
//! once oosd is migrated, replacing the old Electrobun RPC gateway.
//!
//! It also serves the oos agent's read-only RAG index subjects
//! (oos.cmd.global, oos.cmd.domains): they share this handler's pool,
//! queue group and per-message dispatch, so a second subscription loop
//! would buy nothing. oos.cmd.views and oos.cmd.search are deliberately
//! not here yet — views needs the view parser (deferred) and search
//! needs the embed client (the next slice).
//!
//! Why a queue group: without `oosai-cmd`, NATS fans every request out
//! to *all* subscribers on the subject. For a write handler that means
//! each subscriber runs the INSERT and only one reply reaches the
//! caller — duplicate writes. The queue makes NATS pick exactly one
//! subscriber per request, even with several oosai processes (or a
//! stale + fresh subscriber inside one `--hot` process).
//!
//! DSL boundary: `domain.save` now persists the source row *and*
//! re-embeds its RAG chunk on the spot (parse_domain + render_llm_chunk
//! are ported), so an edit in oosd is searchable via oos.cmd.search
//! without a restart. `view.save` still only persists the source — its
//! chunk re-embed waits on parseView + renderViewChunk, which need a
//! view parser in oos-dsls (not ported yet).

use async_nats::{Client, Subscriber};
use futures::StreamExt;
use serde_json::{json, Value};
use sqlx::{PgPool, Row};

use std::sync::Arc;

use anyhow::{anyhow, Result};

use crate::embed::EmbedClient;
use crate::{backfill, domain_index, global_store, view_index};

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
    "oos.cmd.event_streams.list",
    "oos.cmd.event.refresh",
    // RAG index subjects for the oos agent's system prompt (read-only).
    "oos.cmd.global",
    "oos.cmd.domains",
    "oos.cmd.views",
];

/// Subscribes to all command subjects in the queue group and spawns a
/// serving task per subject. Returns once the subscriptions are live;
/// the tasks run until the process exits.
pub async fn serve(client: Client, pool: PgPool, embed: Arc<EmbedClient>) -> Result<()> {
    for &subject in SUBJECTS {
        let sub = client
            .queue_subscribe(subject.to_string(), QUEUE.to_string())
            .await?;
        tokio::spawn(run_subject(client.clone(), pool.clone(), embed.clone(), sub));
    }
    println!("[oosai] listening on oos.cmd.{{domain,view,event_type_grammar,event_mappings}}.* + event.refresh + global/domains/views (queue {QUEUE})");
    Ok(())
}

/// One subscription loop. Each message is dispatched in its own task so
/// a slow query never stalls the subscription or the sibling subjects
/// (same lesson as the old bench `for await` dispatcher).
async fn run_subject(client: Client, pool: PgPool, embed: Arc<EmbedClient>, mut sub: Subscriber) {
    while let Some(msg) = sub.next().await {
        let Some(reply) = msg.reply.clone() else { continue };
        let client = client.clone();
        let pool = pool.clone();
        let embed = embed.clone();
        tokio::spawn(async move {
            let value = match handle(&pool, &embed, msg.subject.as_str(), &msg.payload).await {
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
async fn handle(
    pool: &PgPool,
    embed: &Arc<EmbedClient>,
    subject: &str,
    payload: &[u8],
) -> Result<Value> {
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
            let source = str_field(&body, "source")?;
            save_source(pool, "oos.domain", id, source).await?;
            // Re-embed the RAG chunk so an edit in oosd is searchable via
            // oos.cmd.search without an oosai restart. Best-effort and
            // decoupled from the save's success: a source that doesn't
            // parse yet (saved mid-edit) still persists; its chunk just
            // stays stale until the next save that parses. We never fail
            // the save on a render/embed error — the source row is the
            // editor's source of truth, the chunk is a derived cache.
            match backfill::reembed_domain(pool, embed, source).await {
                Ok(name) => {
                    println!("[oosai/cmd] domain.save {id}: source stored + chunk re-embedded ({name})")
                }
                Err(e) => {
                    eprintln!("[oosai/cmd] domain.save {id}: source stored, chunk re-embed skipped: {e}")
                }
            }
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
            let source = str_field(&body, "source")?;
            save_source(pool, "oos.view", id, source).await?;
            // Re-embed the view chunk so an edit in oosd is resolver-
            // visible (oos.cmd.views) and embedded without a restart.
            // Best-effort, same contract as domain.save: a source that
            // doesn't parse yet still persists, its chunk stays stale
            // until the next save that parses; we never fail the save.
            match backfill::reembed_view(pool, embed, source).await {
                Ok(name) => {
                    println!("[oosai/cmd] view.save {id}: source stored + chunk re-embedded ({name})")
                }
                Err(e) => {
                    eprintln!("[oosai/cmd] view.save {id}: source stored, chunk re-embed skipped: {e}")
                }
            }
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

        // ── Event streams ────────────────────────
        // The oos Ask panel's StreamPicker lists streams for the
        // active mapping; `mapping` is the mapping *name* (empty = all).
        // mapping_name is joined back so the manager panel can show it
        // without a second round-trip.
        "oos.cmd.event_streams.list" => {
            let mapping = body.get("mapping").and_then(Value::as_str).unwrap_or("");
            let limit = body.get("limit").and_then(Value::as_i64).unwrap_or(100);
            let rows = sqlx::query(
                "SELECT s.stream, s.description, s.event_mapping_id::int8 AS event_mapping_id,
                        m.name AS mapping_name, s.tag
                 FROM public.event_streams s
                 LEFT JOIN public.event_mappings m ON m.id = s.event_mapping_id
                 WHERE $1 = '' OR m.name = $1
                 ORDER BY s.stream
                 LIMIT $2",
            )
            .bind(mapping)
            .bind(limit)
            .fetch_all(pool)
            .await?;
            let mut streams = Vec::with_capacity(rows.len());
            for r in &rows {
                let mapping_name = r.try_get::<Option<String>, _>("mapping_name")?;
                streams.push(json!({
                    "stream":           r.try_get::<String, _>("stream")?,
                    "description":      r.try_get::<String, _>("description")?,
                    "event_mapping_id": r.try_get::<Option<i64>, _>("event_mapping_id")?,
                    "mapping_name":     mapping_name.clone(),
                    "mapping":          mapping_name,
                    "tag":              r.try_get::<Option<String>, _>("tag")?,
                }));
            }
            json!({ "streams": streams })
        }

        // ── RAG index (oos agent system prompt) ───────────────────
        // Read-only. global returns every standing-instruction chunk;
        // domains returns the rich one-entry-per-domain catalogue the
        // resolver/prompt need (replaces the lean {ids} of domain.list).
        "oos.cmd.global" => json!({ "prompts": global_store::list_chunks(pool).await? }),
        "oos.cmd.domains" => json!({ "domains": domain_index::load_domain_index(pool).await? }),
        "oos.cmd.views" => json!({ "views": view_index::load_view_index(pool).await? }),

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
