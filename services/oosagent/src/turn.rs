//! Turn handlers for oosagent: the per-turn LLM endpoints the webview
//! seam calls. ask and translate are plain LLM (no tools); chat is the
//! ReAct loop (tools + AgentEvent streaming). event (RAG over the event
//! subsystem) is still a webview-side stub until that backend exists.
//!
//! Subjects:
//!   oos.cmd.turn.ask        {turnId, settings, tuning?, history, user}
//!                              -> {text, error?, trace}
//!   oos.cmd.turn.translate  {turnId, settings, tuning?, source,
//!                            sourceLang, targetLang}
//!                              -> {text, error?, trace}
//!   oos.cmd.turn.chat       {turnId, settings, tuning?, history, user,
//!                            userRole?, username?, viewHint?}
//!                              -> {text, error?, trace}; streams
//!                              AgentEvents on oos.agent.event.<turnId>
//!   oos.cmd.turn.cancel     {turnId} -> {ok, cancelled}
//!
//! Each subject runs its own task and each message a further spawned
//! task, so a slow LLM call never blocks the subscription loop (same
//! pattern as the oosgql/oosai dispatchers). The LLM connection and
//! tuning travel in the payload; nothing here reads KV.

use std::collections::HashMap;
use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::{Arc, Mutex};

use async_nats::{Client, Subscriber};
use chrono::{DateTime, Utc};
use futures::StreamExt;
use serde::Deserialize;
use serde_json::{json, Value};

use crate::chat::{run_agent, AgentCtx, LlmConn, Tune};
use crate::llm::{chat_completion, ChatRequest, Message, Usage};

/// turnId -> cancel flag, so oos.cmd.turn.cancel can trip an in-flight
/// chat turn. Inserted when a chat turn starts, removed when it ends.
type Cancels = Arc<Mutex<HashMap<String, Arc<AtomicBool>>>>;

// Tuning defaults mirror DEFAULT_TUNING in the webview's agent-types.ts.
const DEFAULT_TEMPERATURE: f64 = 0.2;
const DEFAULT_TIMEOUT_MS: u64 = 5 * 60 * 1000;
const DEFAULT_MAX_TOKENS: u64 = 4096;
const DEFAULT_TOP_HITS: u64 = 10;

/// Ask mode is a plain conversation — the model has no data tools here,
/// so it must answer from its own knowledge and not pretend to fetch.
const ASK_PROMPT: &str = "You are OOS Assistant, the AI inside Onisin OS. \
Answer the user directly and concisely, in the user's language. This is a \
plain conversation: you have no database tools in this mode, so do not \
claim to fetch records or open tabs — answer from your own knowledge.";

#[derive(Deserialize)]
struct Settings {
    #[serde(rename = "llmBaseUrl")]
    llm_base_url: String,
    #[serde(rename = "llmApiKey", default)]
    llm_api_key: String,
    #[serde(rename = "llmModel")]
    llm_model: String,
}

#[derive(Deserialize, Default)]
struct Tuning {
    temperature: Option<f64>,
    #[serde(rename = "timeoutMs")]
    timeout_ms: Option<u64>,
    #[serde(rename = "maxTokens")]
    max_tokens: Option<u64>,
    #[serde(rename = "topHits")]
    top_hits: Option<u64>,
}

#[derive(Deserialize)]
struct AskReq {
    settings: Settings,
    #[serde(default)]
    tuning: Option<Tuning>,
    #[serde(default)]
    history: Vec<Message>,
    user: String,
}

#[derive(Deserialize)]
struct TranslateReq {
    settings: Settings,
    #[serde(default)]
    tuning: Option<Tuning>,
    source: String,
    #[serde(rename = "sourceLang", default)]
    source_lang: String,
    #[serde(rename = "targetLang", default)]
    target_lang: String,
}

/// Subscribes to the turn subjects and spawns their serving tasks.
pub async fn serve(client: Client) -> anyhow::Result<()> {
    let ask = client.subscribe("oos.cmd.turn.ask").await?;
    let translate = client.subscribe("oos.cmd.turn.translate").await?;

    let chat = client.subscribe("oos.cmd.turn.chat").await?;
    let cancel = client.subscribe("oos.cmd.turn.cancel").await?;

    let cancels: Cancels = Arc::new(Mutex::new(HashMap::new()));

    tokio::spawn(handle(client.clone(), ask, Kind::Ask));
    tokio::spawn(handle(client.clone(), translate, Kind::Translate));
    tokio::spawn(handle_chat(client.clone(), chat, cancels.clone()));
    tokio::spawn(handle_cancel(client.clone(), cancel, cancels));

    println!(
        "[oosagent] listening on oos.cmd.turn.{{ask,translate,chat,cancel}}; event stays a webview stub until the event subsystem"
    );
    Ok(())
}

#[derive(Clone, Copy)]
enum Kind {
    Ask,
    Translate,
}

async fn handle(client: Client, mut sub: Subscriber, kind: Kind) {
    while let Some(msg) = sub.next().await {
        let Some(reply) = msg.reply.clone() else { continue };
        let client = client.clone();
        tokio::spawn(async move {
            let payload = match kind {
                Kind::Ask => run_ask(&msg.payload).await,
                Kind::Translate => run_translate(&msg.payload).await,
            };
            reply_json(&client, reply, &payload).await;
        });
    }
}

async fn run_ask(payload: &[u8]) -> Value {
    let req: AskReq = match serde_json::from_slice(payload) {
        Ok(r) => r,
        Err(e) => return error_turn(format!("bad request: {e}")),
    };
    let mut messages = Vec::with_capacity(req.history.len() + 2);
    messages.push(Message::system(ASK_PROMPT));
    messages.extend(req.history);
    messages.push(Message::user(req.user));
    run_plain(req.settings, req.tuning.unwrap_or_default(), messages).await
}

async fn run_translate(payload: &[u8]) -> Value {
    let req: TranslateReq = match serde_json::from_slice(payload) {
        Ok(r) => r,
        Err(e) => return error_turn(format!("bad request: {e}")),
    };
    if req.source.trim().is_empty() {
        return error_turn("source text is empty".to_string());
    }
    let messages = vec![
        Message::system(translate_prompt(&req.source_lang, &req.target_lang)),
        Message::user(req.source),
    ];
    run_plain(req.settings, req.tuning.unwrap_or_default(), messages).await
}

// ─── chat (ReAct loop) ──────────────────────────

#[derive(Deserialize)]
struct ChatReq {
    #[serde(rename = "turnId")]
    turn_id: String,
    settings: Settings,
    #[serde(default)]
    tuning: Option<Tuning>,
    #[serde(default)]
    history: Vec<Message>,
    user: String,
    #[serde(rename = "userRole", default)]
    user_role: String,
    #[serde(default)]
    username: String,
    #[serde(rename = "viewHint", default)]
    view_hint: Option<String>,
}

async fn handle_chat(client: Client, mut sub: Subscriber, cancels: Cancels) {
    while let Some(msg) = sub.next().await {
        let Some(reply) = msg.reply.clone() else { continue };
        let client = client.clone();
        let cancels = cancels.clone();
        tokio::spawn(async move {
            let payload = run_chat(client.clone(), cancels, &msg.payload).await;
            reply_json(&client, reply, &payload).await;
        });
    }
}

async fn handle_cancel(client: Client, mut sub: Subscriber, cancels: Cancels) {
    while let Some(msg) = sub.next().await {
        let Some(reply) = msg.reply.clone() else { continue };
        let client = client.clone();
        let cancels = cancels.clone();
        tokio::spawn(async move {
            let payload = run_cancel(&cancels, &msg.payload);
            reply_json(&client, reply, &payload).await;
        });
    }
}

/// Runs one chat turn: registers a cancel flag, drives the ReAct loop,
/// then replies with the final text + trace. Live progress streams via
/// AgentEvents inside the loop on oos.agent.event.<turnId>.
async fn run_chat(client: Client, cancels: Cancels, payload: &[u8]) -> Value {
    let req: ChatReq = match serde_json::from_slice(payload) {
        Ok(r) => r,
        Err(e) => return error_turn(format!("bad request: {e}")),
    };
    let tuning = req.tuning.unwrap_or_default();

    let cancel = Arc::new(AtomicBool::new(false));
    cancels.lock().unwrap().insert(req.turn_id.clone(), cancel.clone());

    let ctx = AgentCtx {
        client,
        conn: LlmConn {
            base_url: req.settings.llm_base_url,
            api_key: req.settings.llm_api_key,
            model: req.settings.llm_model,
        },
        tune: Tune {
            temperature: tuning.temperature.unwrap_or(DEFAULT_TEMPERATURE),
            timeout_ms: tuning.timeout_ms.unwrap_or(DEFAULT_TIMEOUT_MS),
            max_tokens: tuning.max_tokens.unwrap_or(DEFAULT_MAX_TOKENS),
            top_hits: tuning.top_hits.unwrap_or(DEFAULT_TOP_HITS),
        },
        turn_id: req.turn_id.clone(),
        user_role: req.user_role,
        username: req.username,
        view_hint: req.view_hint,
        cancel,
    };

    let result = run_agent(&ctx, req.history, req.user).await;
    cancels.lock().unwrap().remove(&req.turn_id);

    let mut reply = json!({ "text": result.text, "trace": result.trace });
    if let Some(e) = result.error {
        reply["error"] = json!(e);
    }
    reply
}

/// Trips the cancel flag for a turn, if it is still running.
fn run_cancel(cancels: &Cancels, payload: &[u8]) -> Value {
    #[derive(Deserialize)]
    struct CancelReq {
        #[serde(rename = "turnId")]
        turn_id: String,
    }
    let req: CancelReq = match serde_json::from_slice(payload) {
        Ok(r) => r,
        Err(e) => return json!({ "ok": false, "error": format!("bad request: {e}") }),
    };
    let found = match cancels.lock().unwrap().get(&req.turn_id) {
        Some(flag) => {
            flag.store(true, Ordering::Relaxed);
            true
        }
        None => false,
    };
    json!({ "ok": true, "cancelled": found })
}

/// Shared plain-LLM path: one completion, then a {text, trace} reply.
async fn run_plain(settings: Settings, tuning: Tuning, messages: Vec<Message>) -> Value {
    let started = Utc::now();
    let t0 = std::time::Instant::now();

    let res = chat_completion(ChatRequest {
        base_url: settings.llm_base_url,
        api_key: settings.llm_api_key,
        model: settings.llm_model,
        messages,
        temperature: tuning.temperature.unwrap_or(DEFAULT_TEMPERATURE),
        max_tokens: tuning.max_tokens.unwrap_or(DEFAULT_MAX_TOKENS),
        timeout_ms: tuning.timeout_ms.unwrap_or(DEFAULT_TIMEOUT_MS),
        tools: None,
    })
    .await;

    let duration_ms = t0.elapsed().as_millis() as u64;
    let finished = Utc::now();

    match res {
        Ok(reply) => json!({
            "text": reply.text,
            "trace": trace(&started, &finished, duration_ms, reply.usage, "success", None),
        }),
        Err(e) => {
            let msg = e.to_string();
            json!({
                "text": "",
                "error": msg,
                "trace": trace(&started, &finished, duration_ms, Usage::default(), "error", Some(&msg)),
            })
        }
    }
}

/// Builds a TurnTrace value matching the webview's TurnTrace shape.
fn trace(
    started: &DateTime<Utc>,
    finished: &DateTime<Utc>,
    duration_ms: u64,
    usage: Usage,
    status: &str,
    error: Option<&str>,
) -> Value {
    let mut t = json!({
        "startedAt": started.to_rfc3339(),
        "finishedAt": finished.to_rfc3339(),
        "durationMs": duration_ms,
        "usage": usage,
        "toolCalls": [],
        "status": status,
    });
    if let Some(e) = error {
        t["errorMessage"] = json!(e);
    }
    t
}

/// A failed turn that never reached the LLM (bad payload / empty input).
fn error_turn(message: String) -> Value {
    let now = Utc::now().to_rfc3339();
    json!({
        "text": "",
        "error": message,
        "trace": {
            "startedAt": now,
            "finishedAt": now,
            "durationMs": 0,
            "usage": Usage::default(),
            "toolCalls": [],
            "status": "error",
            "errorMessage": message,
        }
    })
}

fn translate_prompt(source_lang: &str, target_lang: &str) -> String {
    let from = if source_lang.trim().is_empty() {
        "the source language".to_string()
    } else {
        source_lang.to_string()
    };
    let to = if target_lang.trim().is_empty() {
        "English".to_string()
    } else {
        target_lang.to_string()
    };
    format!(
        "You are a translation engine. Translate the user's text from {from} into {to}. \
         Output only the translation — no commentary, no quotes, no explanation. \
         Preserve line breaks, markdown, and inline formatting."
    )
}

/// Encodes `payload` as JSON and publishes it to the reply subject.
async fn reply_json(client: &Client, reply: async_nats::Subject, payload: &Value) {
    match serde_json::to_vec(payload) {
        Ok(bytes) => {
            let _ = client.publish(reply, bytes.into()).await;
        }
        Err(e) => eprintln!("[oosagent] reply encode failed: {e}"),
    }
}
