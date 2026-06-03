//! Safe, schema-bound query/mutate over domains — the replacement for
//! the GraphQL layer.
//!
//! Why not GraphQL: the only capabilities the system actually used were
//! (a) fetch rows of a domain with filters, (b) fetch a record together
//! with its dropdown option lists in one round-trip, (c) insert/update/
//! delete a record under a role gate. None of that needs a dynamic
//! GraphQL schema or a query-execution engine — it needs a validator and
//! a SQL builder. So a request is a small typed object, every part of it
//! is checked against the DomainDef (field declared? filterable? operator
//! allowed for the type? role permitted?), and it compiles to bound SQL.
//! Nothing the caller sends reaches SQL unbound, and only declared
//! fields/operators/tables are ever interpolated.
//!
//! Two operations:
//!   oos.cmd.data.query  { domain, fields?, where[], order?, limit?, withOptions? }
//!                       -> { rows: [..], options?: { <meta>: [{value,label}] } }
//!   oos.cmd.data.mutate { domain, op, role, set?, id? }
//!                       -> { record } | { error }
//!
//! Values are bound as text and cast server-side (`$1::bigint`, etc.),
//! and result columns are read back as text (`(col)::text AS col`) then
//! re-typed from the DomainDef. That sidesteps sqlx's need to know every
//! column's exact Postgres type at compile time for a schema only known
//! at runtime, and keeps the layer free of a date/time decode dependency.

use oos_dsls::domain::{DomainDef, DomainFieldDef, FieldType, PermissionAction};
use serde::Deserialize;
use serde_json::{json, Map, Value};
use sqlx::{PgPool, Row};

use crate::domain_store::DomainStore;

#[derive(Deserialize)]
struct QueryReq {
    domain: String,
    #[serde(default)]
    fields: Option<Vec<String>>,
    #[serde(default, rename = "where")]
    conditions: Vec<WhereCond>,
    #[serde(default)]
    order: Vec<OrderTerm>,
    #[serde(default)]
    limit: Option<i64>,
    #[serde(default, rename = "withOptions")]
    with_options: bool,
}

#[derive(Deserialize)]
struct WhereCond {
    field: String,
    op: String,
    value: Value,
}

#[derive(Deserialize)]
struct OrderTerm {
    field: String,
    #[serde(default)]
    dir: Option<String>,
}

#[derive(Deserialize)]
struct MutateReq {
    domain: String,
    op: String,
    role: String,
    #[serde(default)]
    set: Map<String, Value>,
    #[serde(default)]
    id: Option<Value>,
}

// ─── public entry points ─────────────────────────────────────────────

/// Handles oos.cmd.data.query. Returns the reply payload (never errors
/// out of band — problems come back as `{ error }` so the caller always
/// gets a reply).
pub async fn query(store: &DomainStore, pool: Option<&PgPool>, payload: &[u8]) -> Value {
    let req: QueryReq = match serde_json::from_slice(payload) {
        Ok(r) => r,
        Err(e) => return json!({ "error": format!("bad request: {e}") }),
    };
    let Some(domain) = store.get(&req.domain) else {
        return json!({ "error": format!("unknown domain \"{}\"", req.domain) });
    };
    let Some(pool) = pool else { return json!({ "error": "database unavailable" }) };

    let (sql, binds) = match build_select(&domain, &req) {
        Ok(x) => x,
        Err(e) => return json!({ "error": e }),
    };
    let rows = match run(pool, &sql, &binds).await {
        Ok(r) => r,
        Err(e) => return json!({ "error": e.to_string() }),
    };

    let selected = selected_fields(&domain, &req.fields);
    let out: Vec<Value> = rows.iter().map(|r| row_to_json(r, &selected)).collect();
    let mut resp = json!({ "rows": out });
    if req.with_options {
        let opts = load_options(&domain, pool).await;
        resp.as_object_mut().unwrap().insert("options".into(), Value::Object(opts));
    }
    resp
}

/// Handles oos.cmd.data.mutate. Permission-gated; `write` covers insert
/// and update, `delete` covers delete — same fail-closed rule as the Bun
/// assertActionAllowed.
pub async fn mutate(store: &DomainStore, pool: Option<&PgPool>, payload: &[u8]) -> Value {
    let req: MutateReq = match serde_json::from_slice(payload) {
        Ok(r) => r,
        Err(e) => return json!({ "error": format!("bad request: {e}") }),
    };
    let Some(domain) = store.get(&req.domain) else {
        return json!({ "error": format!("unknown domain \"{}\"", req.domain) });
    };
    let action = match req.op.as_str() {
        "insert" | "update" => "write",
        "delete" => "delete",
        other => return json!({ "error": format!("unknown op '{other}'") }),
    };
    if req.role.trim().is_empty() {
        return json!({ "error": "role required" });
    }
    if !is_action_allowed(&domain, &req.role, action) {
        return json!({ "error": format!("role \"{}\" not allowed to {} {}", req.role, action, domain.name) });
    }
    let Some(pool) = pool else { return json!({ "error": "database unavailable" }) };

    let (sql, binds) = match build_mutate(&domain, &req.op, &req.set, req.id.as_ref()) {
        Ok(x) => x,
        Err(e) => return json!({ "error": e }),
    };
    let all = selected_fields(&domain, &None);
    match run_one(pool, &sql, &binds).await {
        Ok(Some(r)) => json!({ "record": row_to_json(&r, &all) }),
        Ok(None) => json!({ "record": Value::Null }),
        Err(e) => json!({ "error": e.to_string() }),
    }
}

// ─── SQL builders (pure, unit-tested) ────────────────────────────────

fn build_select(domain: &DomainDef, req: &QueryReq) -> Result<(String, Vec<Option<String>>), String> {
    let cols = column_names(domain, &req.fields)?;
    let select_list = cols.iter().map(|c| format!("({c})::text AS {c}")).collect::<Vec<_>>().join(", ");
    let mut sql = format!("SELECT {select_list} FROM {}", domain.source);
    let mut binds: Vec<Option<String>> = Vec::new();

    if !req.conditions.is_empty() {
        let mut parts = Vec::new();
        let mut i = 1;
        for c in &req.conditions {
            let field = find_field(domain, &c.field)
                .ok_or_else(|| format!("unknown field '{}'", c.field))?;
            if !(field.filterable || field.name == "id") {
                return Err(format!("field '{}' is not filterable", c.field));
            }
            if !allowed_ops(field.field_type).contains(&c.op.as_str()) {
                return Err(format!("operator '{}' not allowed on '{}'", c.op, c.field));
            }
            if c.op == "contains" {
                parts.push(format!("{} ILIKE ${i}", field.name));
                binds.push(coerce_bind(&c.value, true));
            } else {
                let sqlop = op_sql(&c.op).expect("op already validated");
                parts.push(format!("{} {sqlop} ${i}::{}", field.name, pg_cast(field.field_type)));
                binds.push(coerce_bind(&c.value, false));
            }
            i += 1;
        }
        sql.push_str(&format!(" WHERE {}", parts.join(" AND ")));
    }

    if !req.order.is_empty() {
        let mut terms = Vec::new();
        for o in &req.order {
            if find_field(domain, &o.field).is_none() {
                return Err(format!("unknown order field '{}'", o.field));
            }
            let dir = match o.dir.as_deref() {
                None | Some("asc") | Some("ASC") => "ASC",
                Some("desc") | Some("DESC") => "DESC",
                Some(d) => return Err(format!("invalid order direction '{d}'")),
            };
            terms.push(format!("{} {dir}", o.field));
        }
        sql.push_str(&format!(" ORDER BY {}", terms.join(", ")));
    }

    if let Some(n) = req.limit {
        if n < 0 {
            return Err("limit must be >= 0".into());
        }
        sql.push_str(&format!(" LIMIT {n}"));
    }

    Ok((sql, binds))
}

fn build_mutate(
    domain: &DomainDef,
    op: &str,
    set: &Map<String, Value>,
    id: Option<&Value>,
) -> Result<(String, Vec<Option<String>>), String> {
    let returning = domain
        .fields
        .iter()
        .map(|f| format!("({})::text AS {}", f.name, f.name))
        .collect::<Vec<_>>()
        .join(", ");

    match op {
        "insert" => {
            let (cols, placeholders, binds) = settable_assignments(domain, set, 1);
            if cols.is_empty() {
                return Err("no settable field supplied".into());
            }
            let sql = format!(
                "INSERT INTO {} ({}) VALUES ({}) RETURNING {returning}",
                domain.source,
                cols.join(", "),
                placeholders.join(", "),
            );
            Ok((sql, binds))
        }
        "update" => {
            let id = id.ok_or("id required for update")?;
            let (cols, placeholders, mut binds) = settable_assignments(domain, set, 1);
            if cols.is_empty() {
                return Err("no settable field supplied".into());
            }
            let set_clause = cols
                .iter()
                .zip(&placeholders)
                .map(|(c, p)| format!("{c} = {p}"))
                .collect::<Vec<_>>()
                .join(", ");
            let i = binds.len() + 1;
            binds.push(coerce_bind(id, false));
            let sql = format!(
                "UPDATE {} SET {set_clause} WHERE id = ${i}::{} RETURNING {returning}",
                domain.source,
                id_cast(domain),
            );
            Ok((sql, binds))
        }
        "delete" => {
            let id = id.ok_or("id required for delete")?;
            let sql = format!(
                "DELETE FROM {} WHERE id = $1::{} RETURNING {returning}",
                domain.source,
                id_cast(domain),
            );
            Ok((sql, vec![coerce_bind(id, false)]))
        }
        other => Err(format!("unknown op '{other}'")),
    }
}

/// Builds the settable column list, placeholders and binds for an insert
/// or update. readonly fields and `id` are never settable — server-side
/// enforcement regardless of what the caller sends.
fn settable_assignments(
    domain: &DomainDef,
    set: &Map<String, Value>,
    start: usize,
) -> (Vec<String>, Vec<String>, Vec<Option<String>>) {
    let mut cols = Vec::new();
    let mut placeholders = Vec::new();
    let mut binds = Vec::new();
    let mut i = start;
    for f in &domain.fields {
        if f.read_only || f.name == "id" {
            continue;
        }
        if let Some(v) = set.get(&f.name) {
            cols.push(f.name.clone());
            placeholders.push(format!("${i}::{}", pg_cast(f.field_type)));
            binds.push(coerce_bind(v, false));
            i += 1;
        }
    }
    (cols, placeholders, binds)
}

// ─── validation + mapping helpers ────────────────────────────────────

fn find_field<'a>(domain: &'a DomainDef, name: &str) -> Option<&'a DomainFieldDef> {
    domain.fields.iter().find(|f| f.name == name)
}

/// Resolves the column list for a query: the requested fields (each
/// validated as declared) or every declared field when none/empty given.
fn column_names(domain: &DomainDef, fields: &Option<Vec<String>>) -> Result<Vec<String>, String> {
    match fields {
        Some(fs) if !fs.is_empty() => {
            for f in fs {
                if find_field(domain, f).is_none() {
                    return Err(format!("unknown field '{f}'"));
                }
            }
            Ok(fs.clone())
        }
        _ => Ok(domain.fields.iter().map(|f| f.name.clone()).collect()),
    }
}

/// (name, type) pairs for reading a result row back into typed JSON.
fn selected_fields(domain: &DomainDef, fields: &Option<Vec<String>>) -> Vec<(String, FieldType)> {
    match fields {
        Some(fs) if !fs.is_empty() => fs
            .iter()
            .filter_map(|f| find_field(domain, f).map(|d| (f.clone(), d.field_type)))
            .collect(),
        _ => domain.fields.iter().map(|f| (f.name.clone(), f.field_type)).collect(),
    }
}

/// Operators accepted per field type, mirroring oos-dsls-ts
/// operatorsForType so the runtime accepts exactly what the LLM is
/// taught to send. Names are the SQL-side ids (gte/lte/contains).
fn allowed_ops(t: FieldType) -> &'static [&'static str] {
    match t {
        FieldType::String | FieldType::Text => &["eq", "ne", "contains"],
        FieldType::Int | FieldType::Float => &["eq", "ne", "gt", "gte", "lt", "lte"],
        FieldType::Bool => &["eq"],
        FieldType::Date | FieldType::Datetime => &["eq", "gt", "lt"],
    }
}

/// SQL comparison fragment for a non-`contains` operator.
fn op_sql(op: &str) -> Option<&'static str> {
    match op {
        "eq" => Some("="),
        "ne" => Some("<>"),
        "gt" => Some(">"),
        "gte" => Some(">="),
        "lt" => Some("<"),
        "lte" => Some("<="),
        _ => None,
    }
}

/// Postgres type a bound value is cast to, derived from the declared
/// field type. Lets us bind everything as text and let the server coerce.
fn pg_cast(t: FieldType) -> &'static str {
    match t {
        FieldType::Int => "bigint",
        FieldType::Float => "double precision",
        FieldType::Bool => "boolean",
        FieldType::String | FieldType::Text => "text",
        FieldType::Date => "date",
        FieldType::Datetime => "timestamptz",
    }
}

fn id_cast(domain: &DomainDef) -> &'static str {
    find_field(domain, "id").map(|f| pg_cast(f.field_type)).unwrap_or("bigint")
}

/// Coerces a JSON value to the text form bound to a `$n::type` param.
/// JSON null becomes a SQL NULL bind; `contains` wraps the value in %..%.
fn coerce_bind(v: &Value, contains: bool) -> Option<String> {
    let s = match v {
        Value::Null => return None,
        Value::String(s) => s.clone(),
        Value::Bool(b) => b.to_string(),
        Value::Number(n) => n.to_string(),
        other => other.to_string(),
    };
    if contains {
        Some(format!("%{s}%"))
    } else {
        Some(s)
    }
}

fn is_action_allowed(domain: &DomainDef, role: &str, action: &str) -> bool {
    for p in &domain.permissions {
        if p.role == role {
            return p.actions.iter().any(|a| action_name(*a) == action);
        }
    }
    false
}

fn action_name(a: PermissionAction) -> &'static str {
    match a {
        PermissionAction::Read => "read",
        PermissionAction::Write => "write",
        PermissionAction::Delete => "delete",
    }
}

// ─── execution + row decoding ────────────────────────────────────────

async fn run(pool: &PgPool, sql: &str, binds: &[Option<String>]) -> Result<Vec<sqlx::postgres::PgRow>, sqlx::Error> {
    let mut q = sqlx::query(sql);
    for b in binds {
        q = q.bind(b.clone());
    }
    q.fetch_all(pool).await
}

async fn run_one(pool: &PgPool, sql: &str, binds: &[Option<String>]) -> Result<Option<sqlx::postgres::PgRow>, sqlx::Error> {
    let mut q = sqlx::query(sql);
    for b in binds {
        q = q.bind(b.clone());
    }
    q.fetch_optional(pool).await
}

/// Reads a row (all columns aliased to text) into a typed JSON object,
/// re-typing each column from its declared field type.
fn row_to_json(row: &sqlx::postgres::PgRow, fields: &[(String, FieldType)]) -> Value {
    let mut map = Map::new();
    for (name, ty) in fields {
        let raw: Option<String> = row.try_get(name.as_str()).ok().flatten();
        let val = match raw {
            None => Value::Null,
            Some(s) => retype(&s, *ty),
        };
        map.insert(name.clone(), val);
    }
    Value::Object(map)
}

/// Converts a text-rendered column back to a typed JSON value. A value
/// that doesn't parse (shouldn't happen given the column type) falls
/// back to the raw string rather than erroring the whole row.
fn retype(s: &str, ty: FieldType) -> Value {
    match ty {
        FieldType::Int => s.trim().parse::<i64>().map(Value::from).unwrap_or(Value::String(s.to_string())),
        FieldType::Float => s
            .trim()
            .parse::<f64>()
            .ok()
            .and_then(serde_json::Number::from_f64)
            .map(Value::Number)
            .unwrap_or(Value::String(s.to_string())),
        // Postgres renders bool as 't'/'f' when cast to text.
        FieldType::Bool => Value::Bool(matches!(s, "t" | "true" | "TRUE" | "T")),
        _ => Value::String(s.to_string()),
    }
}

/// Loads every declared meta as a {value,label} list, keyed by meta name
/// (the same key a field's optionsRef points at). These are the form's
/// dropdown sources — the master data delivered alongside the record.
/// A meta on a non-default dsn is not yet supported (single pool); such
/// a meta resolves to an empty list with a log line rather than failing
/// the whole request.
async fn load_options(domain: &DomainDef, pool: &PgPool) -> Map<String, Value> {
    let mut out = Map::new();
    for m in &domain.metas {
        let order = m.order_by.as_ref().map(|o| format!(" ORDER BY {o}")).unwrap_or_default();
        let sql = format!(
            "SELECT ({})::text AS value, ({})::text AS label FROM {}{order}",
            m.value_field, m.label_field, m.table,
        );
        match sqlx::query(&sql).fetch_all(pool).await {
            Ok(rows) => {
                let list: Vec<Value> = rows
                    .iter()
                    .map(|r| {
                        json!({
                            "value": r.try_get::<Option<String>, _>("value").ok().flatten(),
                            "label": r.try_get::<Option<String>, _>("label").ok().flatten(),
                        })
                    })
                    .collect();
                out.insert(m.name.clone(), Value::Array(list));
            }
            Err(e) => {
                eprintln!("[oosgql] options '{}' load failed: {e}", m.name);
                out.insert(m.name.clone(), json!([]));
            }
        }
    }
    out
}

#[cfg(test)]
mod tests {
    use super::*;
    use oos_dsls::domain::parse_domain;

    const PERSON: &str = r#"
domain person from person@demo {
  permission admin   read, write, delete
  permission user    read
  field id        : int    readonly
  field firstname : string filterable
  field age       : int    filterable
  field dept_id   : int    filterable options = departments
  field created_at: datetime readonly
  meta departments from department id name order_by name
}
"#;

    fn person() -> DomainDef {
        parse_domain(PERSON).unwrap()
    }

    fn qreq(json: Value) -> QueryReq {
        serde_json::from_value(json).unwrap()
    }

    #[test]
    fn select_all_fields_no_filter() {
        let (sql, binds) = build_select(&person(), &qreq(json!({ "domain": "person" }))).unwrap();
        assert!(sql.starts_with("SELECT (id)::text AS id"));
        assert!(sql.contains("FROM person"));
        assert!(!sql.contains("WHERE"));
        assert!(binds.is_empty());
    }

    #[test]
    fn where_binds_and_casts() {
        let (sql, binds) = build_select(
            &person(),
            &qreq(json!({ "domain": "person", "where": [{ "field": "age", "op": "gte", "value": 18 }] })),
        )
        .unwrap();
        assert!(sql.contains("WHERE age >= $1::bigint"));
        assert_eq!(binds, vec![Some("18".to_string())]);
    }

    #[test]
    fn contains_wraps_value_and_uses_ilike() {
        let (sql, binds) = build_select(
            &person(),
            &qreq(json!({ "domain": "person", "where": [{ "field": "firstname", "op": "contains", "value": "an" }] })),
        )
        .unwrap();
        assert!(sql.contains("firstname ILIKE $1"));
        assert_eq!(binds, vec![Some("%an%".to_string())]);
    }

    #[test]
    fn id_filter_allowed_even_though_id_not_marked_filterable() {
        let r = build_select(
            &person(),
            &qreq(json!({ "domain": "person", "where": [{ "field": "id", "op": "eq", "value": 5 }] })),
        );
        assert!(r.is_ok());
    }

    #[test]
    fn rejects_filter_on_non_filterable_field() {
        let err = build_select(
            &person(),
            &qreq(json!({ "domain": "person", "where": [{ "field": "created_at", "op": "gt", "value": "x" }] })),
        )
        .unwrap_err();
        assert!(err.contains("not filterable"));
    }

    #[test]
    fn rejects_wrong_operator_for_type() {
        // contains is a string op; age is int.
        let err = build_select(
            &person(),
            &qreq(json!({ "domain": "person", "where": [{ "field": "age", "op": "contains", "value": "5" }] })),
        )
        .unwrap_err();
        assert!(err.contains("not allowed"));
    }

    #[test]
    fn rejects_unknown_field() {
        let err = build_select(&person(), &qreq(json!({ "domain": "person", "fields": ["nope"] }))).unwrap_err();
        assert!(err.contains("unknown field"));
    }

    #[test]
    fn insert_skips_readonly_and_id() {
        let mut set = Map::new();
        set.insert("firstname".into(), json!("Anna"));
        set.insert("id".into(), json!(99)); // must be ignored
        set.insert("created_at".into(), json!("2020")); // readonly, ignored
        let (sql, binds) = build_mutate(&person(), "insert", &set, None).unwrap();
        assert!(sql.contains("INSERT INTO person (firstname)"));
        assert!(sql.contains("VALUES ($1::text)"));
        assert!(sql.contains("RETURNING (id)::text AS id"));
        assert_eq!(binds, vec![Some("Anna".to_string())]);
    }

    #[test]
    fn update_appends_id_bind_last() {
        let mut set = Map::new();
        set.insert("firstname".into(), json!("Bob"));
        let (sql, binds) = build_mutate(&person(), "update", &set, Some(&json!(7))).unwrap();
        assert!(sql.contains("UPDATE person SET firstname = $1::text WHERE id = $2::bigint"));
        assert_eq!(binds, vec![Some("Bob".to_string()), Some("7".to_string())]);
    }

    #[test]
    fn update_without_id_errors() {
        let mut set = Map::new();
        set.insert("firstname".into(), json!("Bob"));
        assert!(build_mutate(&person(), "update", &set, None).is_err());
    }

    #[test]
    fn delete_builds_by_id() {
        let (sql, binds) = build_mutate(&person(), "delete", &Map::new(), Some(&json!(3))).unwrap();
        assert!(sql.contains("DELETE FROM person WHERE id = $1::bigint"));
        assert_eq!(binds, vec![Some("3".to_string())]);
    }

    #[test]
    fn permission_gate_is_fail_closed() {
        let p = person();
        assert!(is_action_allowed(&p, "admin", "delete"));
        assert!(is_action_allowed(&p, "user", "read"));
        assert!(!is_action_allowed(&p, "user", "write"));
        assert!(!is_action_allowed(&p, "ghost", "read"));
    }
}
