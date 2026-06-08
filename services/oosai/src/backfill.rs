//! Schema backfill: reconcile source tables with their embedding
//! (schema) tables on boot.
//!
//! Global prompts (DSL-free) and domains (parse_domain +
//! render_llm_chunk) are both reconciled here. Views still need the
//! view parser + renderViewChunk port; that side slots in the same way,
//! mechanically identical to the domain path, once oos-dsls grows a
//! view parser.
//!
//! Re-embedding is unconditional (no updated_at short-circuit): a model
//! upgrade would otherwise silently leave stale vectors, and the demo
//! dataset is cheap to re-embed on each boot.

use std::collections::HashSet;
use std::sync::Arc;

use sqlx::{PgPool, Row};

use oos_dsls::domain::{parse_domain, render_llm_chunk};
use oos_dsls::view::{parse_view, render_view_chunk};

use crate::embed::EmbedClient;
use crate::{domain_store, global_store, view_store};

/// Per-kind result counts.
pub struct BackfillCount {
    pub embedded: usize,
    pub pruned: usize,
}

/// Reconciles oos.global_prompt -> oos.oos_global_schema: re-embeds
/// every non-locale prompt and prunes schema rows whose source row is
/// gone. A single bad row is logged and skipped so it can't block the
/// rest of the backfill.
pub async fn run_global(pool: &PgPool, embed: &Arc<EmbedClient>) -> anyhow::Result<BackfillCount> {
    let rows = sqlx::query(
        "SELECT name, text FROM oos.global_prompt WHERE name NOT LIKE 'locale.%' ORDER BY name",
    )
    .fetch_all(pool)
    .await?;

    let mut source_keys: HashSet<String> = HashSet::new();
    let mut embedded = 0;
    for row in &rows {
        let name: String = row.get("name");
        let text: String = row.get("text");
        source_keys.insert(name.clone());
        match embed.embed(&text).await {
            Ok(vec) => match global_store::upsert(pool, &name, &text, &vec).await {
                Ok(()) => embedded += 1,
                Err(e) => eprintln!("[oosai] global upsert failed {name}: {e}"),
            },
            Err(e) => eprintln!("[oosai] global embed failed {name}: {e}"),
        }
    }

    let mut pruned = 0;
    for key in global_store::list_keys(pool).await? {
        if !source_keys.contains(&key) {
            global_store::delete(pool, &key).await?;
            println!("[oosai] pruned global orphan {key}");
            pruned += 1;
        }
    }

    Ok(BackfillCount { embedded, pruned })
}

/// Reconciles oos.domain -> oos.oos_domain_schema: parses every domain
/// source, renders its LLM chunk, re-embeds, and prunes schema rows
/// whose source domain is gone. The primary key is the domain's `name`
/// (context_name), not the row id. An unparseable row is logged and
/// skipped so a broken domain can't block the rest of the backfill.
pub async fn run_domains(pool: &PgPool, embed: &Arc<EmbedClient>) -> anyhow::Result<BackfillCount> {
    let rows = sqlx::query("SELECT id, source FROM oos.domain ORDER BY id")
        .fetch_all(pool)
        .await?;

    let mut source_keys: HashSet<String> = HashSet::new();
    let mut embedded = 0;
    for row in &rows {
        let id: String = row.get("id");
        let source: String = row.get("source");
        let def = match parse_domain(&source) {
            Ok(def) => def,
            Err(e) => {
                eprintln!("[oosai] domain backfill parse failed {id}: {e}");
                continue;
            }
        };
        let chunk = render_llm_chunk(&def);
        source_keys.insert(def.name.clone());
        match embed.embed(&chunk).await {
            Ok(vec) => match domain_store::upsert(pool, &def.name, &chunk, &vec).await {
                Ok(()) => embedded += 1,
                Err(e) => eprintln!("[oosai] domain upsert failed {}: {e}", def.name),
            },
            Err(e) => eprintln!("[oosai] domain embed failed {}: {e}", def.name),
        }
    }

    let mut pruned = 0;
    for key in domain_store::list_keys(pool).await? {
        if !source_keys.contains(&key) {
            domain_store::delete(pool, &key).await?;
            println!("[oosai] pruned domain orphan {key}");
            pruned += 1;
        }
    }

    Ok(BackfillCount { embedded, pruned })
}

/// Renders and re-embeds a single domain source into oos_domain_schema,
/// returning the domain's context_name (its `name`) on success.
///
/// Why a standalone helper next to run_domains rather than reusing the
/// loop: the on-save command handler already holds the edited source and
/// needs exactly the boot path (parse_domain -> render_llm_chunk ->
/// embed -> upsert) so a domain edited in oosd becomes searchable
/// without a restart. run_domains keeps its own loop because it also
/// prunes orphans and deliberately tracks the parsed name *before*
/// embedding, so a transient embed failure can't drop an existing chunk
/// — a distinction a single combined helper would erase.
pub async fn reembed_domain(
    pool: &PgPool,
    embed: &Arc<EmbedClient>,
    source: &str,
) -> anyhow::Result<String> {
    let def = parse_domain(source).map_err(|e| anyhow::anyhow!("parse failed: {e}"))?;
    let chunk = render_llm_chunk(&def);
    let vec = embed
        .embed(&chunk)
        .await
        .map_err(|e| anyhow::anyhow!("embed failed: {e}"))?;
    domain_store::upsert(pool, &def.name, &chunk, &vec).await?;
    Ok(def.name)
}

/// Reconciles oos.view -> oos.oos_view_schema: parses every view source,
/// renders its chunk (header + verbatim source), re-embeds, and prunes
/// element rows whose source view is gone. Primary key is the view's
/// `name`. An unparseable row is logged and skipped. Mechanically
/// identical to run_domains; only the parser, renderer and store differ.
pub async fn run_views(pool: &PgPool, embed: &Arc<EmbedClient>) -> anyhow::Result<BackfillCount> {
    let rows = sqlx::query("SELECT id, source FROM oos.view ORDER BY id")
        .fetch_all(pool)
        .await?;

    let mut source_keys: HashSet<String> = HashSet::new();
    let mut embedded = 0;
    for row in &rows {
        let id: String = row.get("id");
        let source: String = row.get("source");
        let def = match parse_view(&source) {
            Ok(def) => def,
            Err(e) => {
                eprintln!("[oosai] view backfill parse failed {id}: {e}");
                continue;
            }
        };
        let chunk = render_view_chunk(&def, &source);
        source_keys.insert(def.name.clone());
        match embed.embed(&chunk).await {
            Ok(vec) => match view_store::upsert(pool, &def.name, &chunk, &vec).await {
                Ok(()) => embedded += 1,
                Err(e) => eprintln!("[oosai] view upsert failed {}: {e}", def.name),
            },
            Err(e) => eprintln!("[oosai] view embed failed {}: {e}", def.name),
        }
    }

    let mut pruned = 0;
    for key in view_store::list_keys(pool).await? {
        if !source_keys.contains(&key) {
            view_store::delete(pool, &key).await?;
            println!("[oosai] pruned view orphan {key}");
            pruned += 1;
        }
    }

    Ok(BackfillCount { embedded, pruned })
}

/// Renders and re-embeds a single view source into oos_view_schema,
/// returning the view's id (its `name`) on success. The on-save twin of
/// reembed_domain: the view.save command handler holds the edited source
/// and needs the same parse -> render_view_chunk -> embed -> upsert path
/// so a view edited in oosd becomes resolver-visible without a restart.
pub async fn reembed_view(
    pool: &PgPool,
    embed: &Arc<EmbedClient>,
    source: &str,
) -> anyhow::Result<String> {
    let def = parse_view(source).map_err(|e| anyhow::anyhow!("parse failed: {e}"))?;
    let chunk = render_view_chunk(&def, source);
    let vec = embed
        .embed(&chunk)
        .await
        .map_err(|e| anyhow::anyhow!("embed failed: {e}"))?;
    view_store::upsert(pool, &def.name, &chunk, &vec).await?;
    Ok(def.name)
}
