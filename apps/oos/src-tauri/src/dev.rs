//! Dev-tab agentic backend.
//!
//! `dev_run` is a Tauri command the DevPanel webview invokes with a plain
//! text prompt. It spins up a RIG agent backed by any OpenAI-compatible
//! endpoint (oosmlx on :8080 or Ollama on :11434) and wires bench's NATS
//! subjects as tools. Progress events are streamed back to the webview via
//! Tauri's `app.emit` so the panel can render them incrementally.
//!
//! Step 1: bench tools — fs_read, fs_list, fs_edit, exec, git_status.
//! Step 2: oosmem tools — memory_search, memory_write.

use std::fmt;

use async_nats::Client as NatsClient;
use rig_core::{
    client::CompletionClient,
    completion::{Prompt, ToolDefinition},
    providers::openai::CompletionsClient,
    tool::Tool,
};
use serde::{Deserialize, Serialize};
use serde_json::{json, Value};
use tauri::{AppHandle, Emitter};

// ── Tool error type ──────────────────────────────────────────────────

/// ToolError wraps a plain string so it satisfies the std::error::Error
/// bound that rig_core::tool::Tool::Error requires in 0.39.
#[derive(Debug)]
pub struct ToolError(String);

impl fmt::Display for ToolError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str(&self.0)
    }
}

impl std::error::Error for ToolError {}

impl From<String> for ToolError {
    fn from(s: String) -> Self { ToolError(s) }
}

// ── Event types emitted to the webview ──────────────────────────────

/// Every push from the agent loop lands as a `dev_event` Tauri event.
/// The webview pattern-matches on `kind` to update the UI.
#[derive(Clone, Serialize)]
#[serde(tag = "kind", rename_all = "snake_case")]
// ToolCall and ToolResult are reserved for future streaming hooks
// that will intercept the agent loop mid-turn.
#[allow(dead_code)]
pub enum DevEvent {
    /// Intermediate text chunk (e.g. agent started, thinking).
    Token { text: String },
    /// A tool the agent decided to call.
    ToolCall { name: String, args: String },
    /// The raw string result bench returned for that tool call.
    ToolResult { name: String, result: String },
    /// Agent has finished; final answer text.
    Done { answer: String },
    /// Unrecoverable error.
    Error { message: String },
}

// ── NATS helper ──────────────────────────────────────────────────────

/// Fire a NATS request and return the reply as a JSON Value.
///
/// Takes an owned subject String so it can be passed into the async-nats
/// future without borrowing from the caller's stack frame.
async fn nats_request(
    nc: &NatsClient,
    subject: String,
    payload: Value,
) -> Result<Value, ToolError> {
    let bytes = serde_json::to_vec(&payload)
        .map_err(|e| ToolError(e.to_string()))?;
    let msg = nc
        .request(subject, bytes.into())
        .await
        .map_err(|e| ToolError(e.to_string()))?;
    serde_json::from_slice(&msg.payload)
        .map_err(|e| ToolError(e.to_string()))
}

// ── Tool: fs_read ────────────────────────────────────────────────────

#[derive(Deserialize, Serialize)]
pub struct FsReadArgs {
    /// Absolute path to the file to read.
    pub path: String,
}

pub struct FsReadTool { pub nc: NatsClient }

impl fmt::Debug for FsReadTool {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.debug_struct("FsReadTool").finish()
    }
}

impl Tool for FsReadTool {
    const NAME: &'static str = "fs_read";
    type Error = ToolError;
    type Args = FsReadArgs;
    type Output = String;

    async fn definition(&self, _prompt: String) -> ToolDefinition {
        ToolDefinition {
            name: Self::NAME.to_string(),
            description: "Read the full text content of a file on the developer's machine. \
                Use when you need to inspect source code, config, or any text file. \
                Always read before editing. Returns the file content as a string."
                .to_string(),
            parameters: json!({
                "type": "object",
                "properties": {
                    "path": { "type": "string",
                              "description": "Absolute path, e.g. /Users/frank/repro/onisin/services/oosai/src/main.rs" }
                },
                "required": ["path"]
            }),
        }
    }

    async fn call(&self, args: Self::Args) -> Result<Self::Output, Self::Error> {
        let reply = nats_request(&self.nc, "bench.fs.read".to_string(),
            json!({ "path": args.path })).await?;
        // bench returns { content, kind, … } — surface the text content only.
        Ok(reply
            .get("content")
            .and_then(|v| v.as_str())
            .map(|s| s.to_string())
            .unwrap_or_else(|| serde_json::to_string_pretty(&reply).unwrap_or_default()))
    }
}

// ── Tool: fs_list ────────────────────────────────────────────────────

#[derive(Deserialize, Serialize)]
pub struct FsListArgs {
    /// Absolute path to the directory to list.
    pub path: String,
}

pub struct FsListTool { pub nc: NatsClient }

impl fmt::Debug for FsListTool {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.debug_struct("FsListTool").finish()
    }
}

impl Tool for FsListTool {
    const NAME: &'static str = "fs_list";
    type Error = ToolError;
    type Args = FsListArgs;
    type Output = String;

    async fn definition(&self, _prompt: String) -> ToolDefinition {
        ToolDefinition {
            name: Self::NAME.to_string(),
            description: "List files and directories inside a directory. \
                Use to explore the project structure before reading specific files. \
                Returns a JSON array of entries with name, type (file/dir), and size."
                .to_string(),
            parameters: json!({
                "type": "object",
                "properties": {
                    "path": { "type": "string",
                              "description": "Absolute directory path, e.g. /Users/frank/repro/onisin/services/oosai/src" }
                },
                "required": ["path"]
            }),
        }
    }

    async fn call(&self, args: Self::Args) -> Result<Self::Output, Self::Error> {
        let reply = nats_request(&self.nc, "bench.fs.list".to_string(),
            json!({ "path": args.path })).await?;
        Ok(serde_json::to_string_pretty(&reply).unwrap_or_else(|_| reply.to_string()))
    }
}

// ── Tool: fs_edit ────────────────────────────────────────────────────

#[derive(Deserialize, Serialize)]
pub struct FsEditArgs {
    /// Absolute path to the file to edit.
    pub path: String,
    /// Exact string to find — must appear exactly once in the file.
    pub find: String,
    /// Replacement string.
    pub replace: String,
}

pub struct FsEditTool { pub nc: NatsClient }

impl fmt::Debug for FsEditTool {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.debug_struct("FsEditTool").finish()
    }
}

impl Tool for FsEditTool {
    const NAME: &'static str = "fs_edit";
    type Error = ToolError;
    type Args = FsEditArgs;
    type Output = String;

    async fn definition(&self, _prompt: String) -> ToolDefinition {
        ToolDefinition {
            name: Self::NAME.to_string(),
            description: "Edit a file by replacing an exact unique string with new content. \
                The find string MUST appear exactly once — read the file first to confirm. \
                Prefer small targeted edits. Returns ok or an error message."
                .to_string(),
            parameters: json!({
                "type": "object",
                "properties": {
                    "path":    { "type": "string", "description": "Absolute path to the file" },
                    "find":    { "type": "string", "description": "Exact unique string to find" },
                    "replace": { "type": "string", "description": "Replacement string" }
                },
                "required": ["path", "find", "replace"]
            }),
        }
    }

    async fn call(&self, args: Self::Args) -> Result<Self::Output, Self::Error> {
        let reply = nats_request(&self.nc, "bench.fs.edit".to_string(),
            json!({ "path": args.path, "find": args.find, "replace": args.replace })).await?;
        Ok(serde_json::to_string(&reply).unwrap_or_else(|_| "ok".to_string()))
    }
}

// ── Tool: exec ───────────────────────────────────────────────────────

#[derive(Deserialize, Serialize)]
pub struct ExecArgs {
    /// Command binary name.
    pub command: String,
    /// Command arguments.
    pub args: Vec<String>,
    /// Working directory (absolute path).
    pub cwd: String,
}

pub struct ExecTool { pub nc: NatsClient }

impl fmt::Debug for ExecTool {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.debug_struct("ExecTool").finish()
    }
}

impl Tool for ExecTool {
    const NAME: &'static str = "exec";
    type Error = ToolError;
    type Args = ExecArgs;
    type Output = String;

    async fn definition(&self, _prompt: String) -> ToolDefinition {
        ToolDefinition {
            name: Self::NAME.to_string(),
            description: "Run a command on the developer's macOS machine and return stdout+stderr. \
                Use for cargo check/test, git operations, or build output inspection. \
                command is the binary name (e.g. 'cargo'), args the argument list (e.g. ['check','-p','oosai']), \
                cwd the working directory. \
                PATH includes /Users/frank/.cargo/bin:/opt/homebrew/bin:/usr/bin:/bin."
                .to_string(),
            parameters: json!({
                "type": "object",
                "properties": {
                    "command": { "type": "string",
                                 "description": "Binary name, e.g. 'cargo' or 'git'" },
                    "args":    { "type": "array", "items": { "type": "string" },
                                 "description": "Argument list, e.g. ['check','-p','oosai']" },
                    "cwd":     { "type": "string",
                                 "description": "Working directory, e.g. /Users/frank/repro/onisin" }
                },
                "required": ["command", "args", "cwd"]
            }),
        }
    }

    async fn call(&self, args: Self::Args) -> Result<Self::Output, Self::Error> {
        let reply = nats_request(&self.nc, "bench.exec.exec".to_string(), json!({
            "command": args.command,
            "args":    args.args,
            "cwd":     args.cwd,
            "env": { "PATH": "/Users/frank/.cargo/bin:/opt/homebrew/bin:/usr/bin:/bin:/usr/sbin:/sbin" }
        })).await?;
        let stdout = reply.get("stdout").and_then(|v| v.as_str()).unwrap_or("");
        let stderr = reply.get("stderr").and_then(|v| v.as_str()).unwrap_or("");
        let code   = reply.get("exit_code").and_then(|v| v.as_i64()).unwrap_or(0);
        Ok(format!("exit={code}\nstdout:\n{stdout}\nstderr:\n{stderr}"))
    }
}

// ── Tool: git_status ─────────────────────────────────────────────────

#[derive(Deserialize, Serialize)]
pub struct GitStatusArgs {
    /// Repository root to inspect.
    pub path: String,
}

pub struct GitStatusTool { pub nc: NatsClient }

impl fmt::Debug for GitStatusTool {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.debug_struct("GitStatusTool").finish()
    }
}

impl Tool for GitStatusTool {
    const NAME: &'static str = "git_status";
    type Error = ToolError;
    type Args = GitStatusArgs;
    type Output = String;

    async fn definition(&self, _prompt: String) -> ToolDefinition {
        ToolDefinition {
            name: Self::NAME.to_string(),
            description: "Get git status for a repository — branch and modified files. \
                Use to understand what has changed before committing or reporting."
                .to_string(),
            parameters: json!({
                "type": "object",
                "properties": {
                    "path": { "type": "string",
                              "description": "Absolute path to the git repo root, e.g. /Users/frank/repro/onisin" }
                },
                "required": ["path"]
            }),
        }
    }

    async fn call(&self, args: Self::Args) -> Result<Self::Output, Self::Error> {
        let reply = nats_request(&self.nc, "bench.git.status".to_string(),
            json!({ "path": args.path })).await?;
        Ok(serde_json::to_string_pretty(&reply).unwrap_or_else(|_| reply.to_string()))
    }
}

// ── Tool: memory_search ──────────────────────────────────────────────

#[derive(Deserialize, Serialize)]
pub struct MemSearchArgs {
    /// Topic phrase close to the subject being investigated.
    pub query: String,
}

pub struct MemSearchTool { pub nc: NatsClient }

impl fmt::Debug for MemSearchTool {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.debug_struct("MemSearchTool").finish()
    }
}

impl Tool for MemSearchTool {
    const NAME: &'static str = "memory_search";
    type Error = ToolError;
    type Args = MemSearchArgs;
    type Output = String;

    async fn definition(&self, _prompt: String) -> ToolDefinition {
        ToolDefinition {
            name: Self::NAME.to_string(),
            description: "Search past work sessions stored in oosmem for architecture decisions, \
                findings, and context. Use when the user asks about a previous decision \
                or technology choice, or says 'how did we solve X'. \
                Query phrase should be close to the topic, 3–8 words."
                .to_string(),
            parameters: json!({
                "type": "object",
                "properties": {
                    "query": { "type": "string",
                               "description": "Topic phrase, e.g. 'pipeline runner nats subjects' or 'oosmlx tool calling'" }
                },
                "required": ["query"]
            }),
        }
    }

    async fn call(&self, args: Self::Args) -> Result<Self::Output, Self::Error> {
        let reply = nats_request(&self.nc, "bench.memory.search".to_string(),
            json!({ "query": args.query, "k": 5 })).await?;
        Ok(serde_json::to_string_pretty(&reply).unwrap_or_else(|_| reply.to_string()))
    }
}

// ── Tool: memory_write ────────────────────────────────────────────────

#[derive(Deserialize, Serialize)]
pub struct MemWriteArgs {
    /// Short kebab-case topic handle (≤200 chars).
    pub topic: String,
    /// Condensed finding to store.
    pub content: String,
    /// Trace dimension: space, time, or action.
    pub trace: String,
}

pub struct MemWriteTool { pub nc: NatsClient }

impl fmt::Debug for MemWriteTool {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.debug_struct("MemWriteTool").finish()
    }
}

impl Tool for MemWriteTool {
    const NAME: &'static str = "memory_write";
    type Error = ToolError;
    type Args = MemWriteArgs;
    type Output = String;

    async fn definition(&self, _prompt: String) -> ToolDefinition {
        ToolDefinition {
            name: Self::NAME.to_string(),
            description: "Store a condensed finding in oosmem so it survives across sessions. \
                Use after completing a non-trivial investigation or edit. \
                topic is kebab-case ≤200 chars. \
                trace: 'space' (location/structure), 'time' (sequence), 'action' (what was done+outcome)."
                .to_string(),
            parameters: json!({
                "type": "object",
                "properties": {
                    "topic":   { "type": "string",
                                 "description": "Short kebab-case handle, e.g. 'dev-tab-rig-bench-integration'" },
                    "content": { "type": "string",
                                 "description": "The condensed finding, decision, or observation" },
                    "trace":   { "type": "string", "enum": ["space","time","action"],
                                 "description": "space=location, time=sequence, action=what was done" }
                },
                "required": ["topic", "content", "trace"]
            }),
        }
    }

    async fn call(&self, args: Self::Args) -> Result<Self::Output, Self::Error> {
        let reply = nats_request(&self.nc, "bench.memory.write".to_string(), json!({
            "stream_id": 1,
            "topic":     args.topic,
            "content":   args.content,
            "trace":     args.trace,
        })).await?;
        Ok(serde_json::to_string(&reply).unwrap_or_else(|_| "ok".to_string()))
    }
}

// ── Agent preamble ───────────────────────────────────────────────────

// The preamble is intentionally prescriptive about tool use order:
// memory_search first (surfaces past decisions), then fs_list/fs_read
// before touching any file, write memory after non-trivial work.
// Without explicit sequencing instructions local models skip the search step.
const PREAMBLE: &str = concat!(
    "You are a senior developer with direct filesystem and shell access to the ",
    "Onisin OS monorepo at /Users/frank/repro/onisin on macOS. ",
    "Stack: Rust headless services (oosai, oosgql, oosagent, oosmlx) in services/; ",
    "Bun/TypeScript + Tauri desktop apps (oos, oosd, ooso, bench) in apps/; ",
    "shared Rust crates in crates/; NATS as the message bus throughout. ",
    "\n\n",
    "MANDATORY tool use order — never skip steps:\n",
    "1. ALWAYS call memory_search first with a query close to the topic. ",
    "   This surfaces architecture decisions and findings from past sessions.\n",
    "2. Call fs_list on the relevant directory before reading or editing any file.\n",
    "3. Call fs_read on a file before proposing or applying any edit.\n",
    "4. Use fs_edit for targeted single-string replacements (find must be unique).\n",
    "5. Use exec with command/args/cwd to run cargo, bun, or git commands.\n",
    "6. After completing non-trivial work call memory_write (trace=action) ",
    "   with a concise summary of what was done and why.\n",
    "\n",
    "Never guess at file contents or directory structure — look first. ",
    "Never summarise what tools exist without calling memory_search and fs_list. ",
    "Report findings concisely; state the next concrete step.",
);

// ── Tauri command ────────────────────────────────────────────────────

/// Payload the webview sends when the user submits a Dev prompt.
#[derive(Deserialize)]
pub struct DevRunArgs {
    pub prompt:   String,
    pub base_url: String,
    pub api_key:  String,
    pub model:    String,
    pub nats_url: String,
}

/// Run a Dev agent turn and stream events back to the webview.
///
/// Returns immediately — the actual work happens on a spawned task.
/// Events are emitted as `dev_event` on the Tauri event bus.
#[tauri::command]
pub async fn dev_run(app: AppHandle, args: DevRunArgs) -> Result<(), String> {
    tauri::async_runtime::spawn(async move {
        run_agent_turn(app, args).await;
    });
    Ok(())
}

async fn run_agent_turn(app: AppHandle, args: DevRunArgs) {
    let emit = |event: DevEvent| {
        let _ = app.emit("dev_event", event);
    };

    let nc = match async_nats::connect(&args.nats_url).await {
        Ok(c) => c,
        Err(e) => {
            emit(DevEvent::Error {
                message: format!("NATS connect failed ({}): {}", args.nats_url, e),
            });
            return;
        }
    };

    // Build the agent inline — Agent<M,P> has two generic params that make
    // naming the return type of a separate builder fn awkward in stable Rust.
    // CompletionsClient uses the Chat Completions API which all local endpoints
    // (oosmlx, Ollama) implement; the Responses API is OpenAI-only.
    let key = if args.api_key.is_empty() { "sk-no-key" } else { &args.api_key };
    // Normalize: settings store the bare host (http://127.0.0.1:8080);
    // RIG appends /chat/completions directly, so we need the /v1 prefix here.
    let base_url = if args.base_url.ends_with("/v1") {
        args.base_url.clone()
    } else {
        format!("{}/v1", args.base_url.trim_end_matches('/'))
    };
    let client = CompletionsClient::builder()
        .api_key(key)
        .base_url(&base_url)
        .build()
        .expect("CompletionsClient::build failed");
    let agent = client
        .agent(&args.model)
        .preamble(PREAMBLE)
        .default_max_turns(20)
        .tool(MemSearchTool { nc: nc.clone() })
        .tool(MemWriteTool  { nc: nc.clone() })
        .tool(FsReadTool    { nc: nc.clone() })
        .tool(FsListTool    { nc: nc.clone() })
        .tool(FsEditTool    { nc: nc.clone() })
        .tool(ExecTool      { nc: nc.clone() })
        .tool(GitStatusTool { nc })
        .build();

    emit(DevEvent::Token { text: "Agent started…\n".to_string() });

    match agent.prompt(args.prompt.as_str()).await {
        Ok(answer) => emit(DevEvent::Done { answer }),
        Err(e)     => emit(DevEvent::Error { message: format!("{e}") }),
    }
}
