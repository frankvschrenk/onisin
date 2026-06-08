//! Minimal OpenAI-compatible chat-completions client (Rust port of the
//! Bun agent's openai.ts).
//!
//! Why a hand-rolled client rather than an LLM SDK: the surface we need
//! is one non-streaming POST to /v1/chat/completions, and the endpoint
//! must work against whatever the user configured (Ollama, vLLM, OpenAI)
//! — a thin reqwest call keeps the dependency footprint and the
//! behaviour predictable. Tool-calling is wired (tools go out in the
//! request, tool_calls are parsed back) for the chat ReAct loop;
//! streaming stays unused — every turn is one non-streaming POST.

use serde::{Deserialize, Serialize};
use serde_json::Value;

/// One message in the chat-completions transcript. Field names are the
/// OpenAI wire names (snake_case) on purpose: the webview forwards
/// history in exactly this shape, so it round-trips without remapping.
#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(tag = "role", rename_all = "lowercase")]
pub enum Message {
    System { content: String },
    User { content: String },
    Assistant {
        content: Option<String>,
        #[serde(skip_serializing_if = "Option::is_none")]
        tool_calls: Option<Vec<ToolCall>>,
    },
    Tool {
        tool_call_id: String,
        content: String,
    },
}

impl Message {
    pub fn system(content: impl Into<String>) -> Self {
        Message::System { content: content.into() }
    }
    pub fn user(content: impl Into<String>) -> Self {
        Message::User { content: content.into() }
    }
}

/// A single tool-call as emitted by the LLM. Carried in assistant
/// history messages and parsed out of responses for the ReAct loop.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ToolCall {
    pub id: String,
    #[serde(rename = "type")]
    pub kind: String,
    pub function: ToolCallFunction,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ToolCallFunction {
    pub name: String,
    pub arguments: String,
}

/// Token usage as reported by the LLM. Missing fields default to zero —
/// many local servers omit `usage` and recording zero beats failing the
/// turn over a missing field. Field names are camelCase to match the
/// webview's Usage/TurnTrace shape directly.
#[derive(Debug, Clone, Default, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct Usage {
    // Serialize as camelCase (promptTokens) for the webview trace; accept
    // the OpenAI snake_case (prompt_tokens) on the way in via alias. Each
    // defaults to 0 so a server that omits a field never fails the turn.
    #[serde(default, alias = "prompt_tokens")]
    pub prompt_tokens: u64,
    #[serde(default, alias = "completion_tokens")]
    pub completion_tokens: u64,
    #[serde(default, alias = "total_tokens")]
    pub total_tokens: u64,
}

/// One chat completion request.
pub struct ChatRequest {
    pub base_url: String,
    pub api_key: String,
    pub model: String,
    pub messages: Vec<Message>,
    pub temperature: f64,
    pub max_tokens: u64,
    pub timeout_ms: u64,
    /// OpenAI-shape function tool descriptors, or None for a plain turn.
    /// Sent only when present: some servers reject an empty tools array,
    /// and ask/translate deliberately run tool-free.
    pub tools: Option<Vec<Value>>,
}

/// Decoded assistant reply.
pub struct ChatReply {
    /// Assistant text (empty when the model returned only tool calls).
    pub text: String,
    /// Tool calls the model requested this step; empty on a final answer.
    // Read by the chat ReAct loop (next slice), not by ask/translate.
    #[allow(dead_code)]
    pub tool_calls: Vec<ToolCall>,
    /// The assistant message verbatim, so the ReAct loop can append it to
    /// the transcript before executing the tool calls.
    #[allow(dead_code)]
    pub raw_message: Message,
    /// Token usage summed by the caller across a turn.
    pub usage: Usage,
}

/// Sends one completion and returns the assistant text + usage. Errors
/// on transport failure, timeout, and non-2xx responses; the caller
/// turns those into an error turn-trace.
pub async fn chat_completion(req: ChatRequest) -> anyhow::Result<ChatReply> {
    let url = format!("{}/v1/chat/completions", req.base_url.trim_end_matches('/'));
    let bearer = if req.api_key.is_empty() { "ollama".to_string() } else { req.api_key.clone() };

    let mut body = serde_json::json!({
        "model": req.model,
        "messages": req.messages,
        "stream": false,
        "temperature": req.temperature,
        "max_tokens": req.max_tokens,
    });
    // Advertise tools only when the caller passes them; an empty array
    // trips some OpenAI-compatible servers.
    if let Some(tools) = &req.tools {
        body["tools"] = serde_json::json!(tools);
    }

    let client = reqwest::Client::builder()
        .timeout(std::time::Duration::from_millis(req.timeout_ms.max(1000)))
        .build()?;

    let res = client
        .post(&url)
        .bearer_auth(bearer)
        .json(&body)
        .send()
        .await
        .map_err(|e| {
            if e.is_timeout() {
                anyhow::anyhow!("chat completions timed out after {}ms", req.timeout_ms)
            } else {
                anyhow::anyhow!("chat completions request failed: {e}")
            }
        })?;

    let status = res.status();
    if !status.is_success() {
        let text = res.text().await.unwrap_or_default();
        let snippet: String = text.chars().take(200).collect();
        anyhow::bail!("chat completions HTTP {}: {snippet}", status.as_u16());
    }

    let parsed: CompletionResponse = res
        .json()
        .await
        .map_err(|e| anyhow::anyhow!("chat completions: bad response body: {e}"))?;

    let choice = parsed
        .choices
        .into_iter()
        .next()
        .ok_or_else(|| anyhow::anyhow!("chat completions: no choice returned"))?;

    let content = choice.message.content;
    let calls = choice.message.tool_calls.unwrap_or_default();
    let text = content.clone().unwrap_or_default();
    let raw_message = Message::Assistant {
        content,
        tool_calls: if calls.is_empty() { None } else { Some(calls.clone()) },
    };
    Ok(ChatReply {
        text,
        tool_calls: calls,
        raw_message,
        usage: parsed.usage.unwrap_or_default(),
    })
}

// ─── Response wire types ─────────────────────────────────────────────

#[derive(Deserialize)]
struct CompletionResponse {
    choices: Vec<Choice>,
    usage: Option<Usage>,
}

#[derive(Deserialize)]
struct Choice {
    message: ResponseMessage,
}

#[derive(Deserialize)]
struct ResponseMessage {
    #[serde(default)]
    content: Option<String>,
    #[serde(default)]
    tool_calls: Option<Vec<ToolCall>>,
}
