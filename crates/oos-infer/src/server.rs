//! The OpenAI-compatible HTTP surface, shared by every backend.

use std::net::SocketAddr;
use std::sync::Arc;
use std::time::{SystemTime, UNIX_EPOCH};

use axum::{
    extract::State,
    http::StatusCode,
    response::{IntoResponse, Response},
    routing::{get, post},
    Json, Router,
};

use crate::engine::{Engine, GenParams};
use crate::openai::{ChatMessage, ChatRequest, ChatResponse, Choice, ModelCard, ModelList, Usage};

type Shared = Arc<dyn Engine>;

/// Build the router for a loaded engine.
pub fn router(engine: Shared) -> Router {
    Router::new()
        .route("/v1/models", get(list_models))
        .route("/v1/chat/completions", post(chat_completions))
        .with_state(engine)
}

/// Bind and serve until the process is stopped.
pub async fn serve(addr: SocketAddr, engine: Shared) -> anyhow::Result<()> {
    let listener = tokio::net::TcpListener::bind(addr).await?;
    tracing::info!(%addr, model = engine.model_id(), "oos-infer serving OpenAI-compatible API");
    axum::serve(listener, router(engine)).await?;
    Ok(())
}

async fn list_models(State(engine): State<Shared>) -> Json<ModelList> {
    Json(ModelList {
        object: "list",
        data: vec![ModelCard {
            id: engine.model_id().to_string(),
            object: "model",
            owned_by: "onisin",
        }],
    })
}

async fn chat_completions(
    State(engine): State<Shared>,
    Json(req): Json<ChatRequest>,
) -> Result<Json<ChatResponse>, AppError> {
    let params = GenParams {
        max_tokens: req.max_tokens.unwrap_or(512),
        temperature: req.temperature.unwrap_or(0.7),
        top_p: req.top_p.unwrap_or(0.95),
    };
    let model = req.model.clone();

    // Generation is compute-bound and blocking; keep it off the async runtime.
    let generation = tokio::task::spawn_blocking(move || engine.generate(&req.messages, &params))
        .await
        .map_err(|e| anyhow::anyhow!("generation task failed: {e}"))?;
    let generation = generation?;

    Ok(Json(ChatResponse {
        id: format!("chatcmpl-{}", now()),
        object: "chat.completion",
        created: now(),
        model,
        choices: vec![Choice {
            index: 0,
            message: ChatMessage {
                role: "assistant".to_string(),
                content: generation.text,
            },
            finish_reason: "stop".to_string(),
        }],
        usage: Usage {
            prompt_tokens: generation.prompt_tokens,
            completion_tokens: generation.completion_tokens,
            total_tokens: generation.prompt_tokens + generation.completion_tokens,
        },
    }))
}

fn now() -> u64 {
    SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .unwrap_or_default()
        .as_secs()
}

/// Wraps any error into a JSON 500 so handlers can use `?`.
struct AppError(anyhow::Error);

impl From<anyhow::Error> for AppError {
    fn from(e: anyhow::Error) -> Self {
        Self(e)
    }
}

impl IntoResponse for AppError {
    fn into_response(self) -> Response {
        tracing::error!(error = %self.0, "request failed");
        (
            StatusCode::INTERNAL_SERVER_ERROR,
            Json(serde_json::json!({ "error": { "message": self.0.to_string() } })),
        )
            .into_response()
    }
}
