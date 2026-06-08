//! domain-dsl: parse `.domain` sources into a runtime `DomainDef`.
//!
//! Public surface is intentionally just the types and `parse_domain`;
//! the lexer and parser internals stay private so the crate is free to
//! change how it tokenizes without breaking consumers.

mod aliases;
mod ddl;
mod lexer;
mod llm_chunk;
mod operators;
mod parser;
mod types;

pub use aliases::domain_aliases;
pub use ddl::domain_to_ddl;
pub use llm_chunk::render_llm_chunk;
pub use parser::parse_domain;
pub use types::*;
