//! view-dsl: parse `.view` sources into a minimal runtime `ViewDef`.
//!
//! Public surface is the types, `parse_view`, and `render_view_chunk` —
//! exactly what the backend (oosai's view index + view-chunk backfill)
//! needs. The lexer and parser internals stay private.

mod chunk;
mod lexer;
mod parser;
mod types;

pub use chunk::render_view_chunk;
pub use parser::parse_view;
pub use types::*;
