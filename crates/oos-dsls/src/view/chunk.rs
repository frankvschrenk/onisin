//! ViewDef -> LLM-friendly text chunk.
//!
//! Companion to the domain `render_llm_chunk`, but deliberately thinner:
//! where a domain chunk is fully re-rendered from the DomainDef, a view
//! chunk wraps a one-line identity header around the *verbatim* DSL
//! source. The source is more compact and more author-faithful than
//! anything we could re-render from the minimal ViewDef, and the LLM
//! benefits from both — the header gives retrieval cues (name, title,
//! target domains, default flag), the source carries the exact layout
//! and field bindings.
//!
//! The byte layout is part of the embedding contract: re-embedding the
//! same source must produce the same chunk, so the header format is
//! stable and matches the Bun renderViewChunk exactly.

use super::types::ViewDef;

/// Builds the chunk text embedded into oos_view_schema:
///
///   View: <name> "<title>" over <domain>[(alias)][, ...] [(default)]
///
///   <verbatim DSL source>
pub fn render_view_chunk(def: &ViewDef, source: &str) -> String {
    format!("{}\n\n{}", render_view_header(def), source)
}

/// The one-line identity header. Aliases are inlined only when they
/// differ from the domain name, so single-domain views keep the compact
/// `over person` form while multi-domain views render every participant.
fn render_view_header(def: &ViewDef) -> String {
    let flags = if def.default { " (default)" } else { "" };
    let over_list = def
        .domains
        .iter()
        .map(|d| {
            if d.alias == d.name {
                d.name.clone()
            } else {
                format!("{}({})", d.name, d.alias)
            }
        })
        .collect::<Vec<_>>()
        .join(", ");
    format!("View: {} \"{}\" over {}{}", def.name, def.title, over_list, flags)
}

#[cfg(test)]
mod tests {
    use super::super::parser::parse_view;
    use super::*;

    #[test]
    fn header_then_verbatim_source() {
        let source = "view person_list \"Personen\" over person {\n  table -> person.rows { column person.id \"ID\" }\n}\n";
        let def = parse_view(source).unwrap();
        let chunk = render_view_chunk(&def, source);
        assert!(chunk.starts_with("View: person_list \"Personen\" over person\n\n"));
        // The source is embedded verbatim after the blank line.
        assert!(chunk.contains("table -> person.rows"));
    }

    #[test]
    fn multi_domain_and_default_in_header() {
        let source = "default view m \"M\" over person(p), address(a) { }";
        let def = parse_view(source).unwrap();
        let header = render_view_chunk(&def, source);
        assert!(header.starts_with("View: m \"M\" over person(p), address(a) (default)"));
    }
}
