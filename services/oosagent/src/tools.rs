//! Read-side tools the chat loop exposes to the LLM.
//!
//! Two tools for the read-only first cut:
//!   oos_schema_search — wraps oos.cmd.search; returns the chunk(s) that
//!     describe a domain's fields, filter operators and where-shape.
//!   oos_query         — wraps oos.cmd.data.query; runs a structured read
//!     and opens the rows as a result tab. No GraphQL: the query is a
//!     domain name plus an optional {field, op, value} where array, which
//!     is exactly what data.query validates against the DomainDef.
//!
//! Schemas live next to execution on purpose — one file is one place to
//! keep the JSON-arg structure and its handler in agreement.

use serde_json::{json, Value};

use crate::chat::AgentCtx;

/// OpenAI-shape function descriptors handed to the LLM each step.
pub fn tool_schemas() -> Vec<Value> {
    vec![
        json!({
            "type": "function",
            "function": {
                "name": "oos_schema_search",
                "description": "Look up how to query one domain: its fields, the operators each field accepts, and the where-clause shape. Pass a short query (e.g. \"person\", \"notes\"). ALWAYS call this before oos_query for a domain.",
                "parameters": {
                    "type": "object",
                    "properties": {
                        "query": { "type": "string", "description": "A short topic — usually a domain name." },
                        "limit": { "type": "integer", "description": "Max chunks to return. Default 2, max 5." }
                    },
                    "required": ["query"]
                }
            }
        }),
        json!({
            "type": "function",
            "function": {
                "name": "oos_query",
                "description": "Run a read query against one domain and open the result as a tab. Use only fields from the chunk's ALLOWED list and operators the chunk lists. Put filters in `where`; omit `where` for all rows.",
                "parameters": {
                    "type": "object",
                    "properties": {
                        "context_name": { "type": "string", "description": "Domain name, e.g. \"person\"." },
                        "where": {
                            "type": "array",
                            "description": "Filter entries, AND-combined. Omit for all rows.",
                            "items": {
                                "type": "object",
                                "properties": {
                                    "field": { "type": "string", "description": "An ALLOWED field name." },
                                    "op": { "type": "string", "description": "An operator the chunk lists for the field (eq, ne, contains, gt, gte, lt, lte)." },
                                    "value": { "description": "Comparison value (string or number)." }
                                },
                                "required": ["field", "op", "value"]
                            }
                        },
                        "fields": {
                            "type": "array",
                            "description": "Optional subset of ALLOWED fields. Omit to return all fields.",
                            "items": { "type": "string" }
                        },
                        "limit": { "type": "integer", "description": "Optional maximum number of rows." }
                    },
                    "required": ["context_name"]
                }
            }
        }),
    ]
}

/// Dispatches a tool by name. Always returns a JSON-serialisable value
/// (never throws): failures come back as { error } so the LLM can read
/// the message and recover on the next step.
pub async fn run_tool(ctx: &AgentCtx, name: &str, args: &Value) -> Value {
    match name {
        "oos_schema_search" => run_schema_search(ctx, args).await,
        "oos_query" => run_query(ctx, args).await,
        other => json!({ "error": format!("unknown tool: {other}") }),
    }
}

// ─── oos_schema_search ────────────────────────────

async fn run_schema_search(ctx: &AgentCtx, args: &Value) -> Value {
    let query = args.get("query").and_then(Value::as_str).unwrap_or("").trim();
    if query.is_empty() {
        return json!({ "error": "query argument is required" });
    }
    // The LLM may pass its own limit; otherwise use the configured
    // top-hits default. Clamp matches oos.cmd.search's own ceiling.
    let limit = clamp_limit(args.get("limit").and_then(Value::as_i64), ctx.tune.top_hits as i64);
    match ctx.request("oos.cmd.search", &json!({ "query": query, "limit": limit })).await {
        Ok(reply) => {
            if let Some(err) = reply.get("error").and_then(Value::as_str) {
                return json!({ "error": err });
            }
            json!({ "chunks": reply.get("hits").cloned().unwrap_or_else(|| json!([])) })
        }
        Err(e) => json!({ "error": format!("schema search failed: {e}") }),
    }
}

fn clamp_limit(raw: Option<i64>, default: i64) -> i64 {
    raw.unwrap_or(default).clamp(1, 50)
}

// ─── oos_query ────────────────────────────────

async fn run_query(ctx: &AgentCtx, args: &Value) -> Value {
    let context_name = args.get("context_name").and_then(Value::as_str).unwrap_or("").trim();
    if context_name.is_empty() {
        return json!({ "error": "context_name is required" });
    }
    // Build the data.query request. where/fields/limit pass through when
    // present; data.query whitelists every field and operator against the
    // DomainDef, so a bad name comes back as a plain { error } the LLM can
    // fix. Reads are ungated, so no role travels here.
    let mut req = json!({ "domain": context_name });
    if let Some(w) = args.get("where") {
        req["where"] = w.clone();
    }
    if let Some(f) = args.get("fields") {
        req["fields"] = f.clone();
    }
    if let Some(l) = args.get("limit") {
        req["limit"] = l.clone();
    }

    let reply = match ctx.request("oos.cmd.data.query", &req).await {
        Ok(r) => r,
        Err(e) => return json!({ "error": format!("data query failed: {e}") }),
    };
    if let Some(err) = reply.get("error").and_then(Value::as_str) {
        return json!({ "error": err });
    }
    let rows = reply.get("rows").cloned().unwrap_or_else(|| json!([]));

    // Side-effect: open a result tab in the webview. The full rows go to
    // the UI here; the LLM gets only a short summary so the next prompt
    // doesn't balloon with row data.
    ctx.emit(json!({
        "type": "tab_open",
        "turnId": ctx.turn_id,
        "contextName": context_name,
        "rows": rows,
        "viewName": ctx.view_hint,
    }))
    .await;

    json!({ "ok": true, "summary": summarise_rows(&rows) })
}

fn summarise_rows(rows: &Value) -> String {
    match rows.as_array() {
        Some(a) => format!("{} row{}", a.len(), if a.len() == 1 { "" } else { "s" }),
        None => "no rows".to_string(),
    }
}
