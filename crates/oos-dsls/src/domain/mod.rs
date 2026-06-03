//! domain-dsl: parse `.domain` sources into a runtime `DomainDef`.
//!
//! Public surface is intentionally just the types and `parse_domain`;
//! the lexer and parser internals stay private so the crate is free to
//! change how it tokenizes without breaking consumers.

mod ddl;
mod lexer;
mod parser;
mod types;

pub use ddl::domain_to_ddl;
pub use parser::parse_domain;
pub use types::*;
