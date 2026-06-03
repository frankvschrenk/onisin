//! In-memory cache of parsed domains, mirrored from oos.domain.
//!
//! Each row's `source` is parsed once into a DomainDef and cached, so
//! the permissions handler (and, later, the GraphQL schema builder) read
//! parsed defs instead of re-parsing on every request. A row whose
//! source fails to parse is logged and dropped from the cache rather
//! than aborting the whole load — half a domain is worse than none, and
//! the author's next save fires oos.domain.changed and retries that row.
//!
//! The lock is a plain std RwLock: it is only ever held to read or swap
//! the map, never across an await, so it cannot stall the runtime.

use std::collections::BTreeMap;
use std::sync::RwLock;

use oos_dsls::domain::{parse_domain, DomainDef};
use sqlx::{PgPool, Row};

/// Caches parsed domains keyed by their oos.domain row id.
pub struct DomainStore {
    pool: Option<PgPool>,
    /// BTreeMap keeps a deterministic key order; snapshot re-sorts by
    /// domain name to match the Bun store's stable output.
    cache: RwLock<BTreeMap<String, DomainDef>>,
}

impl DomainStore {
    pub fn new(pool: Option<PgPool>) -> Self {
        DomainStore { pool, cache: RwLock::new(BTreeMap::new()) }
    }

    /// Re-scans the whole oos.domain table and rebuilds the cache. Used
    /// at boot and as the fallback when a targeted reload is not enough.
    /// A missing oos schema is not an error — the cache just stays empty.
    pub async fn load_all(&self) -> anyhow::Result<()> {
        let Some(pool) = &self.pool else { return Ok(()) };
        let rows = sqlx::query("SELECT id, source FROM oos.domain")
            .fetch_all(pool)
            .await?;

        let mut next = BTreeMap::new();
        let mut failed = Vec::new();
        for row in rows {
            let id: String = row.get("id");
            let source: String = row.get("source");
            match parse_domain(&source) {
                Ok(def) => {
                    next.insert(id, def);
                }
                Err(e) => failed.push(format!("{id} ({e})")),
            }
        }
        if !failed.is_empty() {
            eprintln!("[oosgql] {} domain(s) failed to parse: {}", failed.len(), failed.join(", "));
        }
        *self.cache.write().unwrap() = next;
        Ok(())
    }

    /// Re-reads a single row and updates the cache. Used by the notify
    /// listener so one domain change does not re-parse the whole table.
    /// A deleted row (or one that no longer parses) is evicted.
    pub async fn load_one(&self, id: &str) -> anyhow::Result<()> {
        let Some(pool) = &self.pool else { return Ok(()) };
        let row = sqlx::query("SELECT id, source FROM oos.domain WHERE id = $1")
            .bind(id)
            .fetch_optional(pool)
            .await?;

        match row {
            None => {
                self.cache.write().unwrap().remove(id);
            }
            Some(row) => {
                let source: String = row.get("source");
                match parse_domain(&source) {
                    Ok(def) => {
                        self.cache.write().unwrap().insert(id.to_string(), def);
                    }
                    Err(e) => {
                        eprintln!("[oosgql] domain {id} failed to parse, dropped: {e}");
                        self.cache.write().unwrap().remove(id);
                    }
                }
            }
        }
        Ok(())
    }

    /// Looks up a single cached domain by its declared name (the same
    /// name used in `domain <name> from ...`). Cloned out so callers
    /// don't hold the read lock across awaits.
    pub fn get(&self, name: &str) -> Option<DomainDef> {
        self.cache.read().unwrap().values().find(|d| d.name == name).cloned()
    }

    /// Snapshot of every cached domain, sorted by name for determinism.
    pub fn snapshot(&self) -> Vec<DomainDef> {
        let mut out: Vec<DomainDef> = self.cache.read().unwrap().values().cloned().collect();
        out.sort_by(|a, b| a.name.cmp(&b.name));
        out
    }
}
