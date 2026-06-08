//! DomainDef -> LLM-friendly text chunk.
//!
//! The chunk teaches the agent how to read a domain through the
//! `oos_query` tool, which calls oos.cmd.data.query: the field list, the
//! per-field filter operators, the {field, op, value} where shape, the
//! dropdown vocabularies, and the permission matrix. GraphQL is retired
//! — no query strings appear here.
//!
//! Deterministic: same DomainDef -> same string, byte for byte, so the
//! backfill can hash-and-skip unchanged chunks. The section order
//! (header, aliases, fields, filter examples, relations, metas, dropdown
//! mapping, permissions, ai hints) is part of the retrieval contract.

use std::collections::HashSet;

use super::aliases::domain_aliases;
use super::operators::{find_operator, format_example_value, operators_for_type, render_where_entry};
use super::types::{
    AiHintDef, DomainDef, DomainFieldDef, FieldType, MetaDef, PermissionAction, PermissionDef,
    RelationDef, RelationKind,
};

/// Renders the structured chunk embedded into pgvector for LLM retrieval.
pub fn render_llm_chunk(def: &DomainDef) -> String {
    let mut s = String::new();
    write_header(&mut s, def);
    write_aliases(&mut s, def);
    write_fields(&mut s, def);
    write_filter_examples(&mut s, def);
    write_relations(&mut s, def);
    write_metas(&mut s, def);
    write_dropdown_field_mapping(&mut s, def);
    write_permissions(&mut s, def);
    write_ai_hints(&mut s, def);
    s
}

// ─── header ───────────────────────────────────

fn write_header(s: &mut String, def: &DomainDef) {
    s.push_str(&format!("Domain: {}\n", def.name));
    s.push_str(&format!("Source: {}@{}\n", def.source, def.dsn));
}

// ─── aliases ───────────────────────────

/// Emits retrieval-friendly variants on top of the bare name, which
/// already appears in the header — so the name itself is filtered out.
fn write_aliases(s: &mut String, def: &DomainDef) {
    let variants: Vec<String> = domain_aliases(def)
        .into_iter()
        .filter(|a| a != &def.name)
        .collect();
    s.push_str(&format!("Alias: {}\n", variants.join(", ")));
}

// ─── fields ────────────────────────────

fn write_fields(s: &mut String, def: &DomainDef) {
    if def.fields.is_empty() {
        return;
    }
    let descs: Vec<String> = def.fields.iter().map(describe_field).collect();
    s.push_str(&format!("Fields: {}\n", descs.join(" | ")));

    let names: Vec<&str> = def.fields.iter().map(|f| f.name.as_str()).collect();
    s.push_str(&format!(
        "ALLOWED query fields (ONLY these, no others): {}\n",
        names.join(", ")
    ));

    // Read path: oos_query against this domain. Omitting `where` returns
    // every row; the result always carries the full field list above.
    s.push_str(&format!(
        "Read: call oos_query with context_name \"{}\" and an optional where array (omit it for all rows).\n",
        def.name
    ));
}

fn describe_field(f: &DomainFieldDef) -> String {
    let mut attrs: Vec<String> = Vec::new();
    if f.read_only {
        attrs.push("readonly".to_string());
    }
    if f.filterable {
        attrs.push("filterable".to_string());
    }
    if let Some(opt) = &f.options_ref {
        attrs.push(format!("options={opt}"));
    }
    let ty = field_type_str(f.field_type);
    if attrs.is_empty() {
        format!("{} ({})", f.name, ty)
    } else {
        format!("{} ({}, {})", f.name, ty, attrs.join(", "))
    }
}

fn field_type_str(t: FieldType) -> &'static str {
    match t {
        FieldType::Int => "int",
        FieldType::Float => "float",
        FieldType::String => "string",
        FieldType::Text => "text",
        FieldType::Bool => "bool",
        FieldType::Date => "date",
        FieldType::Datetime => "datetime",
    }
}

// ─── filter examples ──────────────────────────

fn write_filter_examples(s: &mut String, def: &DomainDef) {
    let filterable: Vec<&DomainFieldDef> = def.fields.iter().filter(|f| f.filterable).collect();
    if filterable.is_empty() {
        return;
    }
    let names: Vec<&str> = filterable.iter().map(|f| f.name.as_str()).collect();
    s.push_str(&format!("Filterable fields: {}\n", names.join(", ")));
    s.push_str(
        "Filter with a where array; each entry is { \"field\": <name>, \"op\": <op>, \"value\": <value> }. Multiple entries are AND-combined.\n",
    );

    // Allowed operator tokens per filterable field — the runtime rejects
    // any op not listed here for the field's type.
    let by_field: Vec<String> = filterable
        .iter()
        .map(|f| {
            let toks: Vec<&str> = operators_for_type(f.field_type).iter().map(|o| o.token).collect();
            format!("{} [{}]", f.name, toks.join(", "))
        })
        .collect();
    s.push_str(&format!("Allowed operators by field: {}\n", by_field.join(" | ")));

    // Concrete where examples: one typed default per field (its first
    // op), then any author-supplied overrides with their descriptions.
    for f in &filterable {
        if let Some(op) = operators_for_type(f.field_type).first() {
            let entry = render_where_entry(&f.name, op.token, op.sample_value);
            s.push_str(&format!("where example ({} {}): [{}]\n", f.name, op.label, entry));
        }
        for ex in &f.examples {
            let Some(op) = find_operator(f.field_type, ex.op) else {
                continue;
            };
            let value = format_example_value(f.field_type, &ex.value, ex.value_is_string);
            let entry = render_where_entry(&f.name, op.token, &value);
            let header = if ex.description.is_empty() {
                format!("where example ({} {})", f.name, op.label)
            } else {
                format!("where example ({} {}) — {}", f.name, op.label, ex.description)
            };
            s.push_str(&format!("{}: [{}]\n", header, entry));
        }
    }

    // Combined-filter example: two AND-ed entries in one where array.
    if filterable.len() >= 2 {
        if let Some(line) = render_multi_filter_example(&filterable) {
            s.push_str(&line);
        }
    }
}

fn render_multi_filter_example(filterable: &[&DomainFieldDef]) -> Option<String> {
    let a = filterable[0];
    let b = filterable[1];
    let op_a = operators_for_type(a.field_type).into_iter().next()?;
    let op_b = operators_for_type(b.field_type).into_iter().next()?;
    let entry_a = render_where_entry(&a.name, op_a.token, op_a.sample_value);
    let entry_b = render_where_entry(&b.name, op_b.token, op_b.sample_value);
    Some(format!(
        "where example (combined — {} and {}, AND): [{}, {}]\n",
        a.name, b.name, entry_a, entry_b
    ))
}

// ─── relations ─────────────────────────

fn write_relations(s: &mut String, def: &DomainDef) {
    if def.relations.is_empty() {
        return;
    }
    let lines: Vec<String> = def.relations.iter().map(describe_relation).collect();
    s.push_str(&format!("Relations: {}\n", lines.join(" | ")));
}

fn describe_relation(r: &RelationDef) -> String {
    format!(
        "{} ({} {}, {} -> {})",
        r.name,
        relation_kind_str(r.kind),
        r.target,
        r.local_field,
        r.foreign_field
    )
}

fn relation_kind_str(k: RelationKind) -> &'static str {
    match k {
        RelationKind::HasMany => "has_many",
        RelationKind::HasOne => "has_one",
        RelationKind::BelongsTo => "belongs_to",
    }
}

// ─── meta sources ───────────────────────

fn write_metas(s: &mut String, def: &DomainDef) {
    if def.metas.is_empty() {
        return;
    }
    let summary: Vec<String> = def.metas.iter().map(describe_meta_source).collect();
    s.push_str(&format!("Dropdown sources: {}\n", summary.join(" | ")));
}

fn describe_meta_source(m: &MetaDef) -> String {
    format!("{} (from {}.{}, label={})", m.name, m.table, m.value_field, m.label_field)
}

// ─── dropdown field mapping ────────────────────

fn write_dropdown_field_mapping(s: &mut String, def: &DomainDef) {
    let pairs = collect_dropdown_pairs(def);
    if pairs.is_empty() {
        return;
    }
    s.push_str(
        "Dropdown fields (closed vocabularies — values come only from the meta list, never free text):\n",
    );
    for p in &pairs {
        s.push_str(&format!("  - {} -> {}\n", p.field, p.meta));
    }
    s.push_str(
        "Option lists are returned by oos_query when called with withOptions: true, in `options` keyed by meta name.\n",
    );
}

struct DropdownPair {
    field: String,
    meta: String,
}

fn collect_dropdown_pairs(def: &DomainDef) -> Vec<DropdownPair> {
    let meta_names: HashSet<&str> = def.metas.iter().map(|m| m.name.as_str()).collect();
    let mut pairs = Vec::new();
    for f in &def.fields {
        if let Some(opt) = &f.options_ref {
            if meta_names.contains(opt.as_str()) {
                pairs.push(DropdownPair { field: f.name.clone(), meta: opt.clone() });
            }
        }
    }
    pairs
}

// ─── permissions ───────────────────────

fn write_permissions(s: &mut String, def: &DomainDef) {
    if def.permissions.is_empty() {
        return;
    }
    let lines: Vec<String> = def.permissions.iter().map(describe_permission).collect();
    s.push_str(&format!("Permissions: {}\n", lines.join(" | ")));
}

fn describe_permission(p: &PermissionDef) -> String {
    let actions: Vec<&str> = p.actions.iter().map(|a| permission_action_str(*a)).collect();
    format!("{}={}", p.role, actions.join(","))
}

fn permission_action_str(a: PermissionAction) -> &'static str {
    match a {
        PermissionAction::Read => "read",
        PermissionAction::Write => "write",
        PermissionAction::Delete => "delete",
    }
}

// ─── AI hints ────────────────────────

fn write_ai_hints(s: &mut String, def: &DomainDef) {
    let usable: Vec<&AiHintDef> = def
        .ai_hints
        .iter()
        .filter(|h| !collapse_whitespace(&h.body).is_empty())
        .collect();
    if usable.is_empty() {
        return;
    }
    s.push_str("AI hints:\n");
    for h in &usable {
        s.push_str(&format!("  - {}: {}\n", h.name, collapse_whitespace(&h.body)));
    }
}

/// Normalises runs of whitespace to single spaces and trims — authors
/// write multi-line hints for readability; the embedder wants flat text.
fn collapse_whitespace(s: &str) -> String {
    s.split_whitespace().collect::<Vec<_>>().join(" ")
}

#[cfg(test)]
mod tests {
    use super::super::parse_domain;
    use super::render_llm_chunk;

    const NOTE_DOMAIN: &str = r#"
domain note from note@demo {
  permission admin   read, write, delete
  permission manager read, write
  permission user    read

  field id         : int      readonly
  field person_id  : int      readonly filterable {
    example eq 42 "Alle Notizen der Person mit id 42"
  }
  field title      : string   filterable {
    example like "Meeting" "Titel enthält 'Meeting'"
  }
  field body       : text
  field created_at : datetime readonly

  ai "scope"
     "Notizen gehören immer zu einer Person."

  aliases [ "notizen", "anmerkungen" ]
}
"#;

    #[test]
    fn renders_note_chunk_sections() {
        let def = parse_domain(NOTE_DOMAIN).expect("note domain parses");
        let chunk = render_llm_chunk(&def);

        // Header + identity.
        assert!(chunk.starts_with("Domain: note\nSource: note@demo\n"));
        // Author alias expanded into the German phrasing family.
        assert!(chunk.contains("Alias: "));
        assert!(chunk.contains("notizen"));
        // Field listing + the ALLOWED line the LLM is told to obey.
        assert!(chunk.contains("ALLOWED query fields (ONLY these, no others): id, person_id, title, body, created_at"));
        // Read path is oos_query / data.query, not GraphQL.
        assert!(chunk.contains("Read: call oos_query with context_name \"note\""));
        assert!(!chunk.contains("GraphQL"));
        // Filterable fields + per-field op tokens (data.query vocabulary).
        assert!(chunk.contains("Filterable fields: person_id, title"));
        assert!(chunk.contains("Allowed operators by field: person_id [eq, ne, gt, gte, lt, lte] | title [contains, eq, ne]"));
        // Typed-default where example + the author override (bare int value).
        assert!(chunk.contains("where example (person_id equals): [{ \"field\": \"person_id\", \"op\": \"eq\", \"value\": 0 }]"));
        assert!(chunk.contains("Alle Notizen der Person mit id 42"));
        assert!(chunk.contains("\"value\": 42"));
        // String author override quotes the value.
        assert!(chunk.contains("where example (title contains) — Titel enthält 'Meeting'"));
        // Combined where example (two filterable fields present).
        assert!(chunk.contains("where example (combined — person_id and title, AND):"));
        // Permissions matrix.
        assert!(chunk.contains("Permissions: admin=read,write,delete | manager=read,write | user=read"));
        // AI hint, whitespace-collapsed.
        assert!(chunk.contains("AI hints:"));
        assert!(chunk.contains("  - scope: Notizen gehören immer zu einer Person."));
    }
}
