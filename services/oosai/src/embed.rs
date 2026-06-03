//! OpenAI-compatible embedding client.
//!
//! Direct port of oos-embed-ts/EmbedClient: talks POST /v1/embeddings
//! to any compatible endpoint (Ollama, vLLM, hosted). Kept as the only
//! place that knows the model/URL/credentials, so NATS callers stay
//! ignorant of them. Three-attempt retry with backoff on transient
//! failures (429/5xx/network); permanent 4xx fail immediately because
//! retrying a 400 is just noise.

use std::time::Duration;

use serde::Deserialize;

use crate::error::EmbedError;

/// Slice of the /v1/embeddings response we care about.
#[derive(Deserialize)]
struct EmbeddingResponse {
    data: Vec<EmbeddingDatum>,
}

#[derive(Deserialize)]
struct EmbeddingDatum {
    embedding: Vec<f32>,
}

/// Wraps an embeddings endpoint with retries. Stateless beyond the
/// resolved config, so it is safe to share across concurrent callers
/// behind an `Arc`.
pub struct EmbedClient {
    http: reqwest::Client,
    /// Base URL including the `/v1` suffix.
    base_url: String,
    api_key: String,
    model: String,
}

impl EmbedClient {
    /// Builds a client. `base_url` is normalised the same way the TS
    /// client does it: trailing slashes stripped, `/v1` appended unless
    /// already present.
    pub fn new(base_url: &str, api_key: &str, model: &str, timeout: Duration) -> Self {
        let trimmed = base_url.trim_end_matches('/');
        let base = if trimmed.ends_with("/v1") {
            trimmed.to_string()
        } else {
            format!("{trimmed}/v1")
        };
        let http = reqwest::Client::builder()
            .timeout(timeout)
            .build()
            .expect("reqwest client builds with default config");
        EmbedClient {
            http,
            base_url: base,
            api_key: api_key.to_string(),
            model: model.to_string(),
        }
    }

    /// Returns the configured model name (served verbatim on
    /// oos.cmd.embed.meta).
    pub fn model(&self) -> &str {
        &self.model
    }

    /// Probes the model's true output dimension by embedding one short
    /// string and measuring the result. This is ground truth from the
    /// endpoint, not a guess from the model name — downstream services
    /// size their vector columns against it.
    pub async fn probe_dim(&self) -> Result<usize, EmbedError> {
        Ok(self.embed("probe").await?.len())
    }

    /// Embeds `text`, retrying up to three times on transient errors
    /// with 100ms/300ms backoff. Returns the permanent error or the
    /// last transient one once attempts are exhausted.
    pub async fn embed(&self, text: &str) -> Result<Vec<f32>, EmbedError> {
        let mut last: Option<EmbedError> = None;
        for attempt in 0..3 {
            match self.attempt(text).await {
                Ok(vec) => return Ok(vec),
                Err(err) => {
                    if !err.is_transient() {
                        return Err(err);
                    }
                    last = Some(err);
                    if attempt < 2 {
                        let backoff = 100 * (attempt + 1) * (attempt + 1);
                        tokio::time::sleep(Duration::from_millis(backoff as u64)).await;
                    }
                }
            }
        }
        Err(last.unwrap_or(EmbedError::Empty(self.model.clone())))
    }

    /// One HTTP round-trip. Network failures and 429/5xx are tagged
    /// transient; other non-2xx are permanent.
    async fn attempt(&self, text: &str) -> Result<Vec<f32>, EmbedError> {
        let body = serde_json::json!({ "model": self.model, "input": text });
        let mut req = self
            .http
            .post(format!("{}/embeddings", self.base_url))
            .json(&body);
        if !self.api_key.is_empty() {
            req = req.bearer_auth(&self.api_key);
        }

        let resp = req
            .send()
            .await
            .map_err(|e| EmbedError::Unreachable(e.to_string()))?;

        let status = resp.status();
        if !status.is_success() {
            let transient = status.as_u16() == 429 || status.is_server_error();
            return Err(EmbedError::Http {
                status: status.as_u16(),
                transient,
            });
        }

        let parsed: EmbeddingResponse = resp
            .json()
            .await
            .map_err(|e| EmbedError::Unreachable(e.to_string()))?;
        match parsed.data.into_iter().next() {
            Some(d) if !d.embedding.is_empty() => Ok(d.embedding),
            _ => Err(EmbedError::Empty(self.model.clone())),
        }
    }
}
