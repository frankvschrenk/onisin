//! DomainDef → CREATE TABLE DDL.
//!
//! Ported verbatim from the old oosai buildDomainDdl so a table created via the
//! new Rust path is identical to what the Bun toolchain produced: an idempotent
//! CREATE TABLE IF NOT EXISTS in `public`, with a serial "id" primary key that
//! is synthesised when the domain does not declare one. The type map matches
//! the old dslTypeToSql one-to-one.

use super::types::{DomainDef, FieldType};

/// Postgres column type for a DSL field type (matches old dslTypeToSql).
fn type_to_sql(t: FieldType) -> &'static str {
    match t {
        FieldType::String => "varchar(255)",
        FieldType::Text => "text",
        FieldType::Int => "integer",
        FieldType::Float => "numeric",
        FieldType::Bool => "boolean",
        FieldType::Date => "date",
        FieldType::Datetime => "timestamptz",
    }
}

/// Build a `CREATE TABLE IF NOT EXISTS public."<table>"` for the domain's
/// source table. The "id" column is always the serial primary key — hoisted to
/// the front whether the domain declared it or it had to be synthesised —
/// matching the old generator's column ordering.
pub fn domain_to_ddl(def: &DomainDef) -> String {
    let table = if def.source.is_empty() { &def.name } else { &def.source };

    let mut cols: Vec<String> = Vec::new();
    let mut has_pk = false;
    for f in &def.fields {
        if f.name == "id" {
            cols.insert(0, "\"id\" serial PRIMARY KEY".to_string());
            has_pk = true;
        } else {
            cols.push(format!("\"{}\" {}", f.name, type_to_sql(f.field_type)));
        }
    }
    if !has_pk {
        cols.insert(0, "\"id\" serial PRIMARY KEY".to_string());
    }

    let body = cols
        .iter()
        .map(|c| format!("  {c}"))
        .collect::<Vec<_>>()
        .join(",\n");
    format!("CREATE TABLE IF NOT EXISTS public.\"{table}\" (\n{body}\n);")
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::domain::parse_domain;

    #[test]
    fn synthesises_id_and_maps_types() {
        let def = parse_domain(
            "domain note from note@demo {\n  field title : string\n  field body : text\n}",
        )
        .unwrap();
        let ddl = domain_to_ddl(&def);
        assert!(ddl.starts_with("CREATE TABLE IF NOT EXISTS public.\"note\" ("));
        assert!(ddl.contains("\"id\" serial PRIMARY KEY"));
        assert!(ddl.contains("\"title\" varchar(255)"));
        assert!(ddl.contains("\"body\" text"));
    }

    #[test]
    fn declared_id_is_not_duplicated_and_leads() {
        let def = parse_domain(
            "domain person from person@demo {\n  field name : string\n  field id : int\n}",
        )
        .unwrap();
        let ddl = domain_to_ddl(&def);
        assert_eq!(ddl.matches("\"id\"").count(), 1);
        // id column is hoisted ahead of name regardless of declaration order.
        let id_at = ddl.find("\"id\"").unwrap();
        let name_at = ddl.find("\"name\"").unwrap();
        assert!(id_at < name_at);
    }
}
