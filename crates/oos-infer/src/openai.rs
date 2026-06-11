//! Minimal OpenAI-compatible wire types — the single LLM integration shape the
//! rest of onisin already speaks, so any existing client works unchanged.

use serde::{Deserialize, Serialize};

#[derive(Debug, Deserialize)]
pub struct ChatRequest {
    pub model: String,
    pub messages: Vec<ChatMessage>,
    #[serde(default)]
    pub max_tokens: Option<usize>,
    #[serde(default)]
    pub temperature: Option<f32>,
    #[serde(default)]
    pub top_p: Option<f32>,
    /// When set, the response is streamed as chat.completion.chunk events
    /// (SSE frames over HTTP) instead of one chat.completion object.
    #[serde(default)]
    pub stream: bool,
    /// NATS extension: the subject chunks are published to while generation
    /// runs (requires `stream: true`). Request-Reply stays untouched -- the
    /// full ChatResponse still arrives as the reply, doubling as completion
    /// signal -- so streaming over NATS is purely additive, and the chunk
    /// objects match the SSE frames exactly. Ignored over HTTP.
    #[serde(default)]
    pub stream_subject: Option<String>,
    /// Extension (Qwen/DashScope convention): let the model reason in its
    /// thinking channel before answering. The reasoning comes back separately
    /// as `reasoning_content` (DeepSeek convention) on the message and on
    /// stream deltas; `content` stays the clean answer either way.
    #[serde(default)]
    pub enable_thinking: bool,
    /// Tools advertised to the model (OpenAI function-calling shape). The
    /// backend renders these into the model family's native declaration
    /// syntax; families without one ignore them.
    #[serde(default)]
    pub tools: Option<Vec<Tool>>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ChatMessage {
    pub role: String,
    /// OpenAI clients send `content: null` on assistant messages that carry
    /// only tool calls; null folds to empty so the rest of the code can keep
    /// treating content as plain text.
    #[serde(default, deserialize_with = "null_as_empty")]
    pub content: String,
    /// The model's reasoning, when thinking was enabled; never part of
    /// `content`. Optional on the wire in both directions.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub reasoning_content: Option<String>,
    /// Tool calls the assistant requested (assistant messages only).
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub tool_calls: Option<Vec<ToolCall>>,
    /// On role "tool" messages: the call this result answers.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub tool_call_id: Option<String>,
}

fn null_as_empty<'de, D: serde::Deserializer<'de>>(d: D) -> Result<String, D::Error> {
    Ok(Option::<String>::deserialize(d)?.unwrap_or_default())
}

/// One advertised tool. `parameters` stays raw JSON Schema: each backend
/// renders it into its family's declaration grammar, so no schema model is
/// imposed here.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Tool {
    #[serde(rename = "type")]
    pub kind: String,
    pub function: ToolFunction,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ToolFunction {
    pub name: String,
    #[serde(default)]
    pub description: String,
    #[serde(default)]
    pub parameters: Option<serde_json::Value>,
}

/// A tool call the model requested, in OpenAI shape: `arguments` is a
/// JSON-encoded object string, not parsed JSON.
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

#[derive(Debug, Serialize)]
pub struct ChatResponse {
    pub id: String,
    pub object: &'static str,
    pub created: u64,
    pub model: String,
    pub choices: Vec<Choice>,
    pub usage: Usage,
}

#[derive(Debug, Serialize)]
pub struct Choice {
    pub index: u32,
    pub message: ChatMessage,
    pub finish_reason: String,
}

#[derive(Debug, Serialize)]
pub struct Usage {
    pub prompt_tokens: usize,
    pub completion_tokens: usize,
    pub total_tokens: usize,
}

/// One streamed slice of a completion (`object: chat.completion.chunk`).
/// The final chunk carries `finish_reason` and, deviating-but-compatible
/// with OpenAI's opt-in stream_options, always the usage -- our own clients
/// want the accounting and foreign clients ignore the extra field.
#[derive(Debug, Serialize)]
pub struct ChatChunk {
    pub id: String,
    pub object: &'static str,
    pub created: u64,
    pub model: String,
    pub choices: Vec<ChunkChoice>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub usage: Option<Usage>,
}

#[derive(Debug, Serialize)]
pub struct ChunkChoice {
    pub index: u32,
    pub delta: Delta,
    pub finish_reason: Option<String>,
}

/// The incremental part of a chunk: the first chunk announces the role, the
/// middle ones carry content, the final one is empty apart from finish_reason.
#[derive(Debug, Default, Serialize)]
pub struct Delta {
    #[serde(skip_serializing_if = "Option::is_none")]
    pub role: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub content: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub reasoning_content: Option<String>,
    /// Tool calls, delivered whole on the final chunk rather than as
    /// incremental fragments: a call is only useful complete, and the
    /// fragment encoding would buy nothing but client-side reassembly.
    #[serde(skip_serializing_if = "Option::is_none")]
    pub tool_calls: Option<Vec<ToolCall>>,
}

#[derive(Debug, Serialize)]
pub struct ModelList {
    pub object: &'static str,
    pub data: Vec<ModelCard>,
}

#[derive(Debug, Serialize)]
pub struct ModelCard {
    pub id: String,
    pub object: &'static str,
    pub owned_by: &'static str,
}
