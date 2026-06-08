//! System-prompt builder for one chat turn (port of agent/prompt.ts).
//!
//! A sandwich: a stable behaviour + domain-index layer on top, the user
//! message at the bottom (the loop appends it), and on-demand schema
//! chunks fetched mid-loop as tool results in between. The static layers
//! come from oos.cmd.global (standing instructions) and oos.cmd.domains
//! (the rich domain index). Both are fetched once per turn; the index is
//! tiny and a per-turn fetch keeps the prompt fresh against schema edits.
//!
//! View-hinted prompts (the resolver's pre-LLM domain/view pick) are
//! deferred until a view index exists in oosai; until then every turn
//! takes the worked-examples path and the LLM finds the domain itself.

use serde_json::{json, Value};

use crate::chat::AgentCtx;

/// Assembles the static system-prompt text for one turn.
pub async fn build_system_prompt(ctx: &AgentCtx) -> String {
    let globals = fetch_globals(ctx).await;
    let domains = fetch_domain_index(ctx).await;

    let sections = [
        build_header(ctx),
        render_globals(&globals),
        render_domain_index(&domains),
        WORKED_EXAMPLES.to_string(),
        FOOTER.to_string(),
    ];
    sections
        .into_iter()
        .filter(|s| !s.trim().is_empty())
        .collect::<Vec<_>>()
        .join("\n\n")
}

// ─── section renderers ─────────────────────────

/// Opening lines; includes the caller's name/role when authenticated so
/// the LLM can answer in scope and decline out-of-scope writes.
fn build_header(ctx: &AgentCtx) -> String {
    let mut lines = vec![
        "You are OOS Assistant, the AI inside Onisin OS.".to_string(),
        "Reply in the user's language. Keep replies brief — the data goes into a side tab; the chat is for confirmation and follow-up.".to_string(),
    ];
    if !ctx.username.is_empty() || !ctx.user_role.is_empty() {
        let who = if ctx.username.is_empty() {
            String::new()
        } else {
            format!("Current user: {}", ctx.username)
        };
        let role = if ctx.user_role.is_empty() {
            String::new()
        } else {
            format!("(role: {})", ctx.user_role)
        };
        lines.push(format!("{who} {role}").trim().to_string());
    }
    if !ctx.user_role.is_empty() {
        lines.push(
            "Writes (insert/update/delete) are allowed for role admin and manager; role user may only read. If the user asks for a change their role does not permit, explain politely that they lack write access.".to_string(),
        );
    }
    lines.join("\n")
}

fn render_globals(prompts: &[(String, String)]) -> String {
    if prompts.is_empty() {
        return String::new();
    }
    let body = prompts
        .iter()
        .map(|(name, chunk)| format!("## {}\n{}", name, chunk.trim()))
        .collect::<Vec<_>>()
        .join("\n\n");
    format!("# Behaviour conventions\n\n{body}")
}

fn render_domain_index(domains: &[DomainEntry]) -> String {
    if domains.is_empty() {
        return "# Available domains\n\n(none registered yet)".to_string();
    }
    let lines = domains
        .iter()
        .map(|d| {
            let scope = d.scope.as_ref().map(|s| format!(" — {s}")).unwrap_or_default();
            format!("- **{}** ({}){}", d.name, d.source, scope)
        })
        .collect::<Vec<_>>()
        .join("\n");
    format!("# Available domains\n\n{lines}")
}

const WORKED_EXAMPLES: &str = "# How to answer a data question\n\n1. Recognise which domain the user means. Use the *Available domains* list above as the index.\n2. Call `oos_schema_search` with a short query (e.g. \"person\") to get the field list, the per-field operators, and the where shape.\n3. Call `oos_query` with `context_name` set to the domain name. For filters, pass a `where` array of {field, op, value} entries — use ONLY fields from the chunk's `ALLOWED query fields` line and operators the chunk lists. Omit `where` for all rows.\n4. Reply briefly. Do not paste the data back — the frontend opens it as a tab automatically when oos_query succeeds.\n\n## Filters narrow the rows, not the field set\n\nA `where` clause changes which rows return; it never changes which fields you get. Leave `fields` unset unless the user explicitly asks for a subset.\n\n## Example: \"Zeig mir alle Personen\"\n\n- oos_schema_search({ query: \"person\" })\n- oos_query({ context_name: \"person\" })\n- Reply: \"Hier sind alle Personen.\"\n\n## Example: \"Personen aus Berlin\"\n\n- oos_schema_search({ query: \"person\" })\n- oos_query({ context_name: \"person\", where: [{ field: \"city\", op: \"eq\", value: \"Berlin\" }] })\n- Reply: \"Hier sind die Personen aus Berlin.\"\n\n## Example: \"Personen älter als 49 in London\" (combined filters, AND)\n\n- oos_schema_search({ query: \"person\" })\n- oos_query({ context_name: \"person\", where: [{ field: \"age\", op: \"gt\", value: 49 }, { field: \"city\", op: \"eq\", value: \"London\" }] })\n- Reply: \"Hier die Personen über 49 aus London.\"";

const FOOTER: &str = "# Rules\n\n- Never invent field names. If a field is not in the chunk's ALLOWED query fields list, do not use it.\n- Use only the operators the chunk lists for a field; the runtime rejects any other operator.\n- Do not attempt writes (insert/update/delete) — only reads are available in this mode.\n- If `oos_query` returns an error, read it, fix the where or fields, and retry once. If it still fails, reply with a brief apology.";

// ─── fetch helpers ─────────────────────────────

/// One domain index entry from oos.cmd.domains.
struct DomainEntry {
    name: String,
    source: String,
    scope: Option<String>,
}

async fn fetch_globals(ctx: &AgentCtx) -> Vec<(String, String)> {
    match ctx.request("oos.cmd.global", &json!({})).await {
        Ok(v) => v
            .get("prompts")
            .and_then(Value::as_array)
            .map(|a| {
                a.iter()
                    .filter_map(|p| {
                        let name = p.get("name")?.as_str()?.to_string();
                        let chunk = p.get("chunk")?.as_str()?.to_string();
                        Some((name, chunk))
                    })
                    .collect()
            })
            .unwrap_or_default(),
        Err(e) => {
            eprintln!("[oosagent] oos.cmd.global failed: {e}");
            Vec::new()
        }
    }
}

async fn fetch_domain_index(ctx: &AgentCtx) -> Vec<DomainEntry> {
    match ctx.request("oos.cmd.domains", &json!({})).await {
        Ok(v) => v
            .get("domains")
            .and_then(Value::as_array)
            .map(|a| {
                a.iter()
                    .filter_map(|d| {
                        let name = d.get("name")?.as_str()?.to_string();
                        let source = d.get("source").and_then(Value::as_str).unwrap_or("").to_string();
                        let scope = d.get("scope").and_then(Value::as_str).map(|s| s.to_string());
                        Some(DomainEntry { name, source, scope })
                    })
                    .collect()
            })
            .unwrap_or_default(),
        Err(e) => {
            eprintln!("[oosagent] oos.cmd.domains failed: {e}");
            Vec::new()
        }
    }
}
