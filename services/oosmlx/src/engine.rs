//! The MLX-backed Engine -- a lazy, single-resident model manager.
//!
//! Why a manager and not one fixed model: OpenAI/Ollama clients pick a model
//! per request, so the engine loads the requested model on demand and keeps it
//! resident, swapping when a different one is asked for. Only one model is held
//! at a time because these are large (a 26B is ~15 GB); a keep-alive or
//! N-resident policy can layer on later. With `--features mlx` this runs a real
//! forward pass; without it, it loads nothing and returns a placeholder so
//! non-Apple/CI builds stay green. The API and the ooscuda contract are the
//! same either way.

use anyhow::Result;
use oos_infer::engine::{Engine, GenParams, Generation};
use oos_infer::openai::ChatMessage;
use oos_infer::registry;

#[cfg(feature = "mlx")]
use anyhow::{anyhow, Context};
#[cfg(feature = "mlx")]
use oos_infer::ModelRef;
#[cfg(feature = "mlx")]
use tokenizers::Tokenizer;

pub struct MlxEngine {
    // The one resident model, loaded on first use and swapped on a different
    // request id. Behind a mutex so generations (and swaps) serialise, which is
    // what we want on a single accelerator.
    #[cfg(feature = "mlx")]
    resident: std::sync::Mutex<Option<Resident>>,
}

#[cfg(feature = "mlx")]
struct Resident {
    id: String,
    tokenizer: Tokenizer,
    model: Box<dyn crate::models::Model>,
}

impl MlxEngine {
    /// A fresh engine with nothing loaded; models load on first request.
    pub fn new() -> Self {
        Self {
            #[cfg(feature = "mlx")]
            resident: std::sync::Mutex::new(None),
        }
    }

    /// Eagerly load a model so the first request is warm (used by `--preload`).
    #[cfg(feature = "mlx")]
    pub fn preload(&self, model: &str) -> Result<()> {
        let resident = load_resident(model)?;
        *self
            .resident
            .lock()
            .map_err(|_| anyhow!("resident mutex poisoned"))? = Some(resident);
        Ok(())
    }

    #[cfg(not(feature = "mlx"))]
    pub fn preload(&self, _model: &str) -> Result<()> {
        Ok(())
    }
}

impl Default for MlxEngine {
    fn default() -> Self {
        Self::new()
    }
}

/// Resolve a model id (HF repo or local path), then load its tokenizer and the
/// architecture-dispatched MLX model.
#[cfg(feature = "mlx")]
fn load_resident(id: &str) -> Result<Resident> {
    let model_ref = ModelRef::parse(id);
    tracing::info!(?model_ref, "loading model");
    let files = registry::resolve(&model_ref).context("resolving model files")?;
    let tokenizer = Tokenizer::from_file(&files.tokenizer_json)
        .map_err(|e| anyhow!("loading tokenizer {}: {e}", files.tokenizer_json.display()))?;
    let model = crate::models::load(&files, &tokenizer)?;
    tracing::info!(model = id, layers = model.num_layers(), "model loaded");
    Ok(Resident {
        id: id.to_string(),
        tokenizer,
        model,
    })
}

impl Engine for MlxEngine {
    fn available_models(&self) -> Vec<String> {
        let mut ids = registry::list_hf_cache_models();
        // A resident model loaded from a local path won't be in the HF cache;
        // surface it too.
        #[cfg(feature = "mlx")]
        if let Ok(guard) = self.resident.lock() {
            if let Some(resident) = guard.as_ref() {
                if !ids.contains(&resident.id) {
                    ids.push(resident.id.clone());
                }
            }
        }
        ids.sort();
        ids.dedup();
        ids
    }

    #[cfg(feature = "mlx")]
    fn generate(&self, model: &str, messages: &[ChatMessage], params: &GenParams) -> Result<Generation> {
        let mut guard = self
            .resident
            .lock()
            .map_err(|_| anyhow!("resident mutex poisoned"))?;

        // Load on demand. On a different id, drop the current model *before*
        // loading the next so we never hold two large models at once (at the
        // cost of losing the warm one if the new load fails).
        let needs_load = guard.as_ref().map(|r| r.id != model).unwrap_or(true);
        if needs_load {
            *guard = None;
            *guard = Some(load_resident(model)?);
        }
        let resident = guard.as_ref().expect("resident set above");
        let model = &resident.model;
        let tokenizer = &resident.tokenizer;

        let prompt = model.render_prompt(messages);
        let encoding = tokenizer
            .encode(prompt, false)
            .map_err(|e| anyhow!("tokenize: {e}"))?;
        let prompt_ids: Vec<i32> = encoding.get_ids().iter().map(|&u| u as i32).collect();
        let prompt_tokens = prompt_ids.len();

        let mut cache = crate::models::KvCache::new(model.num_layers());
        let stop = model.stop_tokens();
        let mut step = prompt_ids;
        let mut out: Vec<u32> = Vec::new();
        for _ in 0..params.max_tokens {
            let logits = model.forward_logits(&step, &mut cache)?;
            let next = crate::models::pick(&logits, params.temperature, params.top_p)?;
            if stop.contains(&next) {
                break;
            }
            out.push(next as u32);
            step = vec![next];
        }

        let text = tokenizer
            .decode(&out, true)
            .map_err(|e| anyhow!("detokenize: {e}"))?;

        Ok(Generation {
            text,
            prompt_tokens,
            completion_tokens: out.len(),
        })
    }

    #[cfg(not(feature = "mlx"))]
    fn generate(&self, model: &str, messages: &[ChatMessage], _params: &GenParams) -> Result<Generation> {
        let user = messages
            .iter()
            .rev()
            .find(|m| m.role == "user")
            .map(|m| m.content.as_str())
            .unwrap_or("");
        let prompt_tokens = user.split_whitespace().count();
        let text = format!(
            "[oosmlx] built without the `mlx` feature: requested model `{model}` was not run \
             (the MLX forward pass is not compiled in). Rebuild with `--features mlx`."
        );
        Ok(Generation {
            text,
            prompt_tokens,
            completion_tokens: 0,
        })
    }
}
