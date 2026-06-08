//! The chat ReAct loop (Rust port of the Bun agent/loop.ts).
//!
//! Shape: ask the LLM with the knowledge-sandwich system prompt and the
//! tool schemas; if it answers in prose, that's the final answer; if it
//! requests tools, run them, append the results to the transcript, and
//! ask again. Bounded by MAX_STEPS so a confused model can't spin — a
//! typical read is 2 calls (search + query).
//!
//! Every tool call and the final message stream out as AgentEvents on
//! oos.agent.event.<turnId> so the webview renders progress live without
//! polling; the same data accrues into a TurnTrace returned with the
//! final reply for the Activity tab. The loop itself writes nothing to
//! disk — it gathers and emits.

use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::Arc;

use async_nats::Client;
use chrono::{DateTime, Utc};
use serde_json::{json, Value};

use crate::llm::{chat_completion, ChatRequest, Message, ToolCall, Usage};
use crate::prompt::build_system_prompt;
use crate::tools::{run_tool, tool_schemas};

/// Maximum LLM round-trips per turn.
const MAX_STEPS: usize = 8;

/// LLM connection for one turn (from the turn payload's settings).
pub struct LlmConn {
    pub base_url: String,
    pub api_key: String,
    pub model: String,
}

/// Resolved per-turn tuning.
pub struct Tune {
    pub temperature: f64,
    pub timeout_ms: u64,
    pub max_tokens: u64,
    pub top_hits: u64,
}

/// Everything one chat turn needs: the bus client (for tool RPCs and
/// event publishing), the LLM connection + tuning, identity for the
/// prompt, and a cancel flag the oos.cmd.turn.cancel handler can trip.
pub struct AgentCtx {
    pub client: Client,
    pub conn: LlmConn,
    pub tune: Tune,
    pub turn_id: String,
    pub user_role: String,
    pub username: String,
    pub view_hint: Option<String>,
    pub cancel: Arc<AtomicBool>,
}

impl AgentCtx {
    /// True once the cancel handler has tripped this turn's flag.
    pub fn aborted(&self) -> bool {
        self.cancel.load(Ordering::Relaxed)
    }

    /// Publishes one AgentEvent on this turn's event subject. Fire-and-
    /// forget: a dropped event must never fail the turn.
    pub async fn emit(&self, event: Value) {
        let subject = format!("oos.agent.event.{}", self.turn_id);
        if let Ok(bytes) = serde_json::to_vec(&event) {
            let _ = self.client.publish(subject, bytes.into()).await;
        }
    }

    /// NATS request-reply returning the decoded JSON reply. Used by the
    /// tools to reach oos.cmd.search / oos.cmd.data.query.
    pub async fn request(&self, subject: &str, payload: &Value) -> anyhow::Result<Value> {
        let bytes = serde_json::to_vec(payload)?;
        let msg = self
            .client
            .request(subject.to_string(), bytes.into())
            .await
            .map_err(|e| anyhow::anyhow!("nats request {subject} failed: {e}"))?;
        let value = serde_json::from_slice(&msg.payload)?;
        Ok(value)
    }
}

/// Outcome of one finished turn.
pub struct ChatResult {
    /// Final assistant text shown in the chat.
    pub text: String,
    /// Set when the turn failed before producing an answer.
    pub error: Option<String>,
    /// TurnTrace value (camelCase) for the webview to persist.
    pub trace: Value,
}

/// Drives one chat turn end-to-end.
pub async fn run_agent(ctx: &AgentCtx, history: Vec<Message>, user: String) -> ChatResult {
    let started = Utc::now();
    let t0 = std::time::Instant::now();
    let mut usage = Usage::default();
    let mut tool_traces: Vec<Value> = Vec::new();

    let system = build_system_prompt(ctx).await;
    let mut messages: Vec<Message> = Vec::with_capacity(history.len() + 2);
    messages.push(Message::system(system));
    messages.extend(history);
    messages.push(Message::user(user));

    let mut step = 0usize;
    while step < MAX_STEPS {
        if ctx.aborted() {
            return cancelled(ctx, &started, t0.elapsed().as_millis() as u64, &usage, &tool_traces).await;
        }
        step += 1;

        let reply = match chat_completion(ChatRequest {
            base_url: ctx.conn.base_url.clone(),
            api_key: ctx.conn.api_key.clone(),
            model: ctx.conn.model.clone(),
            messages: messages.clone(),
            temperature: ctx.tune.temperature,
            max_tokens: ctx.tune.max_tokens,
            timeout_ms: ctx.tune.timeout_ms,
            tools: Some(tool_schemas()),
        })
        .await
        {
            Ok(r) => r,
            Err(e) => {
                // A cancel during the in-flight call surfaces as an
                // error; report it as a cancellation, not a failure.
                if ctx.aborted() {
                    return cancelled(ctx, &started, t0.elapsed().as_millis() as u64, &usage, &tool_traces).await;
                }
                let msg = e.to_string();
                ctx.emit(json!({ "type": "agent_error", "turnId": ctx.turn_id, "message": msg })).await;
                let trace = build_trace(&started, t0.elapsed().as_millis() as u64, &usage, &tool_traces, "error", Some(&msg));
                return ChatResult { text: String::new(), error: Some(msg), trace };
            }
        };

        // Sum usage before moving the message into the transcript.
        usage.prompt_tokens += reply.usage.prompt_tokens;
        usage.completion_tokens += reply.usage.completion_tokens;
        usage.total_tokens += reply.usage.total_tokens;

        let no_tools = reply.tool_calls.is_empty();
        // Keep the assistant turn (incl. its tool calls) in the transcript
        // so the follow-up tool messages have something to attach to.
        messages.push(reply.raw_message);

        if no_tools {
            let text = reply.text.trim().to_string();
            ctx.emit(json!({ "type": "assistant_message", "turnId": ctx.turn_id, "text": text })).await;
            let trace = build_trace(&started, t0.elapsed().as_millis() as u64, &usage, &tool_traces, "success", None);
            return ChatResult { text, error: None, trace };
        }

        for call in &reply.tool_calls {
            if ctx.aborted() {
                return cancelled(ctx, &started, t0.elapsed().as_millis() as u64, &usage, &tool_traces).await;
            }
            dispatch_tool_call(ctx, call, &mut messages, &mut tool_traces).await;
        }
    }

    // Step budget exhausted before a final answer.
    let fallback = "Ich habe das Limit an Tool-Schritten erreicht, bevor ich eine Antwort fertig hatte. Bitte präzisiere deine Frage.".to_string();
    ctx.emit(json!({ "type": "assistant_message", "turnId": ctx.turn_id, "text": fallback })).await;
    let trace = build_trace(&started, t0.elapsed().as_millis() as u64, &usage, &tool_traces, "step_limit", None);
    ChatResult { text: fallback, error: None, trace }
}

/// Runs one tool call: emits start/end events, records the trace entry,
/// and appends the tool result to the transcript for the next step.
async fn dispatch_tool_call(
    ctx: &AgentCtx,
    call: &ToolCall,
    messages: &mut Vec<Message>,
    tool_traces: &mut Vec<Value>,
) {
    let name = call.function.name.clone();
    let args = parse_args(&call.function.arguments);
    let started_at = Utc::now();
    let t0 = std::time::Instant::now();

    ctx.emit(json!({
        "type": "tool_call_start",
        "turnId": ctx.turn_id,
        "callId": call.id,
        "name": name,
        "args": args,
    }))
    .await;

    let result = run_tool(ctx, &name, &args).await;
    let ok = result.get("error").is_none();
    let duration_ms = t0.elapsed().as_millis() as u64;

    tool_traces.push(json!({
        "callId": call.id,
        "name": name,
        "args": args,
        "startedAt": started_at.to_rfc3339(),
        "finishedAt": Utc::now().to_rfc3339(),
        "durationMs": duration_ms,
        "ok": ok,
        "result": result,
    }));

    // The LLM sees the full JSON result on the next step.
    messages.push(Message::Tool {
        tool_call_id: call.id.clone(),
        content: result.to_string(),
    });

    ctx.emit(json!({
        "type": "tool_call_end",
        "turnId": ctx.turn_id,
        "callId": call.id,
        "name": name,
        "ok": ok,
        "summary": summarise_result(&result),
    }))
    .await;
}

/// User-aborted turn: emit a lifecycle event plus a short notice that
/// fills the placeholder bubble, and finalise the trace as cancelled.
async fn cancelled(
    ctx: &AgentCtx,
    started: &DateTime<Utc>,
    elapsed_ms: u64,
    usage: &Usage,
    tool_traces: &[Value],
) -> ChatResult {
    let text = "Abgebrochen.".to_string();
    ctx.emit(json!({ "type": "cancelled", "turnId": ctx.turn_id })).await;
    ctx.emit(json!({ "type": "assistant_message", "turnId": ctx.turn_id, "text": text })).await;
    let trace = build_trace(started, elapsed_ms, usage, tool_traces, "cancelled", None);
    ChatResult { text, error: None, trace }
}

/// Builds a TurnTrace value matching the webview's TurnTrace shape.
fn build_trace(
    started: &DateTime<Utc>,
    duration_ms: u64,
    usage: &Usage,
    tool_calls: &[Value],
    status: &str,
    error: Option<&str>,
) -> Value {
    let mut t = json!({
        "startedAt": started.to_rfc3339(),
        "finishedAt": Utc::now().to_rfc3339(),
        "durationMs": duration_ms,
        "usage": usage,
        "toolCalls": tool_calls,
        "status": status,
    });
    if let Some(e) = error {
        t["errorMessage"] = json!(e);
    }
    t
}

/// Decodes the JSON-string tool arguments. Malformed JSON becomes a
/// structured error the tool layer returns to the LLM, never a panic.
fn parse_args(raw: &str) -> Value {
    serde_json::from_str(raw).unwrap_or_else(|_| {
        let snippet: String = raw.chars().take(80).collect();
        json!({ "error": format!("invalid JSON arguments: {snippet}") })
    })
}

/// Short, UI-friendly summary for the tool-call card. The LLM still gets
/// the full JSON in the transcript regardless.
fn summarise_result(result: &Value) -> String {
    if let Some(err) = result.get("error").and_then(Value::as_str) {
        return format!("error: {err}");
    }
    if let Some(chunks) = result.get("chunks").and_then(Value::as_array) {
        return format!("{} chunk(s)", chunks.len());
    }
    if let Some(summary) = result.get("summary").and_then(Value::as_str) {
        return summary.to_string();
    }
    "ok".to_string()
}
