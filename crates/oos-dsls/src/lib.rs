//! oos-dsls — Rust ports of the onisin DSL parsers.
//!
//! The Bun side (oos-dsls-ts) used Langium to generate parsers for the
//! domain, view and event-schema DSLs. This crate re-implements them as
//! small hand-written lexers + recursive-descent parsers: no Langium,
//! no codegen, no generated AST to keep in sync — just readable code
//! that emits the same runtime defs the rest of the system consumes.
//!
//! Ported so far: the domain-dsl (the blocker for oosgql's query /
//! mutation / permissions subjects) and the view-dsl (the agent's view
//! index + view-chunk RAG). The event-schema DSL lands with the event
//! subsystem.

pub mod domain;
pub mod view;
