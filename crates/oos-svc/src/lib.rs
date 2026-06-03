//! oos-svc — shared service plumbing for the Onisin backend.
//!
//! Everything in here is the "good citizen" boilerplate every headless
//! service repeats: resolve config from a JetStream KV bucket, open a
//! pgvector-capable pool, tick a heartbeat, and answer env.show. It was
//! extracted from oosai once oosgql became the second consumer — so the
//! contents are what is *demonstrably* shared by two services, not a
//! speculative grab-bag. Service-specific pieces (typed Config structs,
//! the embedding client, GraphQL handlers) stay in their own crates.

pub mod db;
pub mod env;
pub mod env_show;
pub mod heartbeat;
pub mod kv;
