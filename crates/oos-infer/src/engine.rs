//! The backend-agnostic inference contract.

use anyhow::Result;

use crate::openai::ChatMessage;

/// Sampling and length parameters for one generation, normalised from the
/// incoming request so backends receive concrete values rather than options.
#[derive(Debug, Clone)]
pub struct GenParams {
    pub max_tokens: usize,
    pub temperature: f32,
    pub top_p: f32,
}

impl Default for GenParams {
    fn default() -> Self {
        Self {
            max_tokens: 512,
            temperature: 0.7,
            top_p: 0.95,
        }
    }
}

/// The product of one generation plus the token accounting the API reports.
#[derive(Debug, Clone)]
pub struct Generation {
    pub text: String,
    pub prompt_tokens: usize,
    pub completion_tokens: usize,
}

/// A loaded model that can generate. Implemented once per accelerator backend
/// (oosmlx via MLX, later ooscuda via CUDA). `generate` is intentionally
/// blocking: the server calls it from a blocking task, so a backend can drive
/// synchronous GPU compute without fighting the async runtime.
pub trait Engine: Send + Sync {
    /// Identifier reported to clients — the requested HF repo id or local path.
    fn model_id(&self) -> &str;

    /// Run a full generation for the given chat messages.
    fn generate(&self, messages: &[ChatMessage], params: &GenParams) -> Result<Generation>;
}
