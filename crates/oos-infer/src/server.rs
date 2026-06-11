//! The OpenAI-compatible HTTP surface -- the external face of the engine. The
//! request path itself lives in `crate::complete`, shared with the NATS
//! transport so both behave identically.

use std::net::SocketAddr;
use std::sync::Arc;

use axum::{
    extract::State,
    http::StatusCode,
    response::sse::{Event, KeepAlive, Sse},
    response::{IntoResponse, Response},
    routing::{get, post},
    Json, Router,
};
use futures::stream::{self, StreamExt};

use crate::engine::Engine;
use crate::openai::{ChatRequest, ModelList};

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
    tracing::info!(%addr, available = engine.available_models().len(), "oos-infer serving OpenAI-compatible API");
    axum::serve(listener, router(engine)).await?;
    Ok(())
}

async fn list_models(State(engine): State<Shared>) -> Json<ModelList> {
    Json(crate::complete::models(&engine))
}

async fn chat_completions(
    State(engine): State<Shared>,
    Json(req): Json<ChatRequest>,
) -> Result<Response, AppError> {
    if !req.stream {
        return Ok(Json(crate::complete::chat(engine, req).await?).into_response());
    }
    // OpenAI's streaming shape: SSE frames each carrying one
    // chat.completion.chunk, an in-band error object if generation fails
    // mid-stream, and a literal [DONE] sentinel to finish.
    let rx = crate::complete::chat_stream(engine, req);
    let chunks = stream::unfold(rx, |mut rx| async move {
        rx.recv().await.map(|item| (item, rx))
    })
    .map(|item| match item {
        Ok(chunk) => Event::default().json_data(chunk),
        Err(e) => {
            tracing::error!(error = %e, "stream failed");
            Event::default().json_data(serde_json::json!({ "error": { "message": e.to_string() } }))
        }
    })
    .chain(stream::iter([Ok(Event::default().data("[DONE]"))]));
    Ok(Sse::new(chunks)
        .keep_alive(KeepAlive::default())
        .into_response())
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
