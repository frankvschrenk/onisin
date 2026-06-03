//! Tier-A demo seeds: the internal oos schema, the public demo schema + data,
//! and the bundled .domain/.view sources. SQL and DSL are compile-time embedded
//! via include_str! (the files live under src-tauri/seed/, next to the binary
//! that embeds them), so there is one binary and no runtime asset path to
//! resolve — consistent with how the grammar sources are embedded.
//!
//! Every step is idempotent (the SQL uses CREATE ... IF NOT EXISTS / OR REPLACE,
//! the DSL rows use ON CONFLICT upserts), so a seed may be re-run safely.
//!
//! The police/support event seeds are intentionally absent: they drive oosai's
//! event-insert REST API, which the NATS-only oosai-rs does not yet serve.

use sqlx::postgres::PgPoolOptions;
use sqlx::PgPool;

use crate::error::CmdError;

const INTERNAL_SQL: &str = include_str!("../seed/internal.sql");
const DEMO_SCHEMA_SQL: &str = include_str!("../seed/demo-schema.sql");
const DEMO_DATA_SQL: &str = include_str!("../seed/demo-data.sql");

/// Bundled .domain sources, upserted into oos.domain by id (the file stem).
const DOMAINS: &[(&str, &str)] = &[
    ("note", include_str!("../seed/dsl/note.domain")),
    ("person", include_str!("../seed/dsl/person.domain")),
];

/// Bundled .view sources, upserted into oos.view by id (the file stem).
const VIEWS: &[(&str, &str)] = &[
    ("note_detail", include_str!("../seed/dsl/note_detail.view")),
    ("note_list", include_str!("../seed/dsl/note_list.view")),
    ("person_detail", include_str!("../seed/dsl/person_detail.view")),
    ("person_list", include_str!("../seed/dsl/person_list.view")),
];

/// Open a short-lived single-connection pool to the target DSN.
async fn connect(dsn: &str) -> Result<PgPool, CmdError> {
    if dsn.is_empty() {
        return Err(CmdError::Msg("no DSN".into()));
    }
    Ok(PgPoolOptions::new().max_connections(1).connect(dsn).await?)
}

/// Apply the internal oos schema (tables, indices, triggers). Idempotent.
pub async fn run_internal(dsn: &str) -> Result<(), CmdError> {
    let pool = connect(dsn).await?;
    sqlx::raw_sql(INTERNAL_SQL).execute(&pool).await?;
    pool.close().await;
    Ok(())
}

/// Apply the public demo schema + data, then upsert the bundled .domain/.view
/// sources into oos.domain / oos.view.
///
/// The DSL rows are written directly via sqlx rather than through oosai's
/// domain/view save subjects: the only side effect those add is chunk
/// embedding, which is part of the deferred renderer work, and the seed only
/// needs the source rows the oosd panels read back.
pub async fn run_demo(dsn: &str) -> Result<(), CmdError> {
    let pool = connect(dsn).await?;

    sqlx::raw_sql(DEMO_SCHEMA_SQL).execute(&pool).await?;
    sqlx::raw_sql(DEMO_DATA_SQL).execute(&pool).await?;

    for (id, source) in DOMAINS {
        sqlx::query(
            "INSERT INTO oos.domain (id, source) VALUES ($1, $2) \
             ON CONFLICT (id) DO UPDATE SET source = EXCLUDED.source, updated_at = now()",
        )
        .bind(id)
        .bind(source)
        .execute(&pool)
        .await?;
    }
    for (id, source) in VIEWS {
        sqlx::query(
            "INSERT INTO oos.view (id, source) VALUES ($1, $2) \
             ON CONFLICT (id) DO UPDATE SET source = EXCLUDED.source, updated_at = now()",
        )
        .bind(id)
        .bind(source)
        .execute(&pool)
        .await?;
    }

    pool.close().await;
    Ok(())
}

// ── Pipeline seed ──────────────────────────────────────────

const PIPELINE_SCHEMA_SQL: &str = include_str!("../seed/demo-schema-pipeline.sql");

/// 10 detailed + 100 generated demo documents, emitted as JSON from the old
/// pipeline-documents.ts so the data stays byte-identical (the 100 mass cases
/// are generated there, including de-DE number formatting).
const DOCS_JSON: &str = include_str!("../seed/pipeline-documents.json");

/// The three demo pipelines, one per architecture pattern. Kept as editable
/// .pipeline sources next to the binary, embedded at compile time.
const PIPELINES: &[(&str, &str)] = &[
    ("fall-analyse-db", include_str!("../seed/pipelines/fall-analyse-db.pipeline")),
    ("fraud-detection-mass", include_str!("../seed/pipelines/fraud-detection-mass.pipeline")),
    ("einzel-fall-kontext", include_str!("../seed/pipelines/einzel-fall-kontext.pipeline")),
];

/// One pipeline demo document. Mirrors the PipelineDocument interface; s3_key is
/// present only for quelle == "s3" rows.
#[derive(serde::Deserialize)]
struct Doc {
    fall_nr: String,
    kategorie: String,
    titel: String,
    inhalt: String,
    quelle: String,
    #[serde(default)]
    s3_key: Option<String>,
}

/// Install the pipeline demo: schema, the 110 documents, and the 3 demo
/// pipelines, then best-effort upload the s3-sourced documents to RustFS.
///
/// Per-document vector embedding (the old oos.cmd.embed step) is intentionally
/// omitted: the thin shell carries no NATS client. Embeddings are backfilled by
/// oosai or wired from the webview seam in a follow-up, so the semantic
/// pipeline step has no vectors until then.
pub async fn run_pipeline(dsn: &str) -> Result<(), CmdError> {
    let pool = connect(dsn).await?;

    // 1. Schema — pipeline_documents + pipelines, idempotent.
    sqlx::raw_sql(PIPELINE_SCHEMA_SQL).execute(&pool).await?;

    // 2. Documents — upsert by fall_nr.
    let docs: Vec<Doc> = serde_json::from_str(DOCS_JSON)
        .map_err(|e| CmdError::Msg(format!("pipeline-documents.json: {e}")))?;
    for d in &docs {
        sqlx::query(
            "INSERT INTO public.pipeline_documents \
             (fall_nr, kategorie, titel, inhalt, quelle, s3_key) \
             VALUES ($1, $2, $3, $4, $5, $6) \
             ON CONFLICT (fall_nr) DO UPDATE SET \
             kategorie = EXCLUDED.kategorie, titel = EXCLUDED.titel, \
             inhalt = EXCLUDED.inhalt, quelle = EXCLUDED.quelle, s3_key = EXCLUDED.s3_key",
        )
        .bind(&d.fall_nr)
        .bind(&d.kategorie)
        .bind(&d.titel)
        .bind(&d.inhalt)
        .bind(&d.quelle)
        .bind(&d.s3_key)
        .execute(&pool)
        .await?;
    }

    // 3. Demo pipelines — upsert by name.
    for (name, source) in PIPELINES {
        sqlx::query(
            "INSERT INTO public.pipelines (name, source) VALUES ($1, $2) \
             ON CONFLICT (name) DO UPDATE SET source = EXCLUDED.source, geaendert_am = now()",
        )
        .bind(name)
        .bind(source)
        .execute(&pool)
        .await?;
    }

    pool.close().await;

    // 4. S3 upload (best-effort) — an unreachable RustFS must not fail the seed.
    upload_s3_docs(&docs).await;

    Ok(())
}

/// Upload the quelle == "s3" documents to the RustFS/MinIO bucket. Best-effort:
/// every failure is logged and swallowed so the seed still reports success.
async fn upload_s3_docs(docs: &[Doc]) {
    let s3_docs: Vec<&Doc> = docs
        .iter()
        .filter(|d| d.quelle == "s3" && d.s3_key.is_some())
        .collect();
    if s3_docs.is_empty() {
        return;
    }

    // RustFS endpoint + credentials, overridable via env (defaults match dev).
    let base = std::env::var("RUSTFS_URL").unwrap_or_else(|_| "http://localhost:9000".into());
    let access = std::env::var("RUSTFS_ACCESS_KEY").unwrap_or_else(|_| "minioadmin".into());
    let secret = std::env::var("RUSTFS_SECRET_KEY").unwrap_or_else(|_| "minioadmin123".into());
    let bucket = "schaeden";
    let client = reqwest::Client::new();

    // Ensure the bucket exists (PUT, tolerate 200 created / 409 exists).
    let date = httpdate::fmt_http_date(std::time::SystemTime::now());
    let auth = s3_auth("PUT", bucket, "", &date, &access, &secret);
    match client
        .put(format!("{base}/{bucket}"))
        .header("Date", &date)
        .header("Authorization", &auth)
        .header("Content-Length", "0")
        .send()
        .await
    {
        Ok(res) if matches!(res.status().as_u16(), 200 | 409) => {}
        Ok(res) => eprintln!("[seed] S3 bucket create: HTTP {}", res.status().as_u16()),
        Err(e) => {
            eprintln!("[seed] S3 bucket check failed: {e} — S3 upload skipped");
            return;
        }
    }

    for d in s3_docs {
        let key = d.s3_key.as_deref().unwrap_or_default();
        let rel = key.strip_prefix("schaeden/").unwrap_or(key);
        let date = httpdate::fmt_http_date(std::time::SystemTime::now());
        let auth = s3_auth("PUT", bucket, key, &date, &access, &secret);
        let res = client
            .put(format!("{base}/{bucket}/{rel}"))
            .header("Date", &date)
            .header("Authorization", &auth)
            .header("Content-Type", "text/plain; charset=utf-8")
            .body(d.inhalt.clone().into_bytes())
            .send()
            .await;
        match res {
            Ok(r) if r.status().is_success() => {}
            Ok(r) => eprintln!("[seed] S3 upload {key}: HTTP {}", r.status().as_u16()),
            Err(e) => eprintln!("[seed] S3 upload {key}: {e}"),
        }
    }
}

/// AWS Signature V2 Authorization header for a RustFS/MinIO PUT — a faithful
/// port of the old seeder's s3AuthHeader: the string to sign hardcodes the
/// text/plain content-type and strips the leading "schaeden/" from the key for
/// the canonical resource. Seeder-only, not production auth.
fn s3_auth(method: &str, bucket: &str, key: &str, date: &str, access: &str, secret: &str) -> String {
    use base64::Engine;
    use hmac::{Hmac, Mac};
    use sha1::Sha1;

    let rel = key.strip_prefix("schaeden/").unwrap_or(key);
    let resource = format!("/{bucket}/{rel}");
    let to_sign = format!("{method}\n\ntext/plain\n{date}\n{resource}");

    let mut mac = Hmac::<Sha1>::new_from_slice(secret.as_bytes()).expect("hmac accepts any key length");
    mac.update(to_sign.as_bytes());
    let sig = mac.finalize().into_bytes();
    let b64 = base64::engine::general_purpose::STANDARD.encode(sig);
    format!("AWS {access}:{b64}")
}
