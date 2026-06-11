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
    /// Let the model reason in its thinking channel before answering, for
    /// model families that have one; the reasoning is returned separately.
    pub thinking: bool,
}

impl Default for GenParams {
    fn default() -> Self {
        Self {
            max_tokens: 512,
            temperature: 0.7,
            top_p: 0.95,
            thinking: false,
        }
    }
}

/// The product of one generation plus the token accounting the API reports.
#[derive(Debug, Clone)]
pub struct Generation {
    pub text: String,
    /// The thinking-channel content, when thinking was enabled and the model
    /// produced any; never mixed into `text`.
    pub reasoning: Option<String>,
    pub prompt_tokens: usize,
    pub completion_tokens: usize,
    /// Why generation ended, in OpenAI terms: "stop" (a stop token) or
    /// "length" (the max_tokens budget ran out).
    pub finish: String,
}

/// A loaded model that can generate. Implemented once per accelerator backend
/// (oosmlx via MLX, later ooscuda via CUDA). `generate` is intentionally
/// blocking: the server calls it from a blocking task, so a backend can drive
/// synchronous GPU compute without fighting the async runtime.
pub trait Engine: Send + Sync {
    /// Models available to serve, reported by GET /v1/models. Backends source
    /// this however they like (oosmlx scans the local Hugging Face cache).
    fn available_models(&self) -> Vec<String>;

    /// Run a full generation with the named model, loading it on demand. The
    /// model id is the per-request selector (an HF repo id or local path),
    /// matching how OpenAI/Ollama clients pick a model rather than the server
    /// being pinned to one at boot.
    fn generate(
        &self,
        model: &str,
        messages: &[ChatMessage],
        params: &GenParams,
    ) -> Result<Generation>;

    /// Like [`generate`](Engine::generate), but emitting incremental text as
    /// it is produced -- `emit(piece, reasoning)`, where `reasoning` marks
    /// thinking-channel pieces as opposed to answer content. The default
    /// falls back to the blocking generation and emits the whole text in one
    /// piece (reasoning first, as it happens temporally), so every backend
    /// can be streamed from day one and a backend opts into real per-token
    /// emission by overriding. The returned [`Generation`] still carries the
    /// full text and token accounting for the final-chunk bookkeeping.
    fn generate_streamed(
        &self,
        model: &str,
        messages: &[ChatMessage],
        params: &GenParams,
        emit: &mut (dyn FnMut(&str, bool) + Send),
    ) -> Result<Generation> {
        let generation = self.generate(model, messages, params)?;
        if let Some(reasoning) = generation.reasoning.as_deref() {
            emit(reasoning, true);
        }
        if !generation.text.is_empty() {
            emit(&generation.text, false);
        }
        Ok(generation)
    }
}
