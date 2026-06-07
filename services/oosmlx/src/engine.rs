//! The MLX-backed Engine.
//!
//! With `--features mlx` this loads a model (dispatched by architecture in
//! `crate::models`) and runs a real forward pass in an incremental decode loop.
//! Without the feature it loads the tokenizer and returns a placeholder, so
//! non-Apple/CI builds stay green. Either way the API and the ooscuda contract
//! are identical. The decode loop is model-agnostic: it asks the model for
//! logits and the stop tokens, and picks greedily or by sampling.

use anyhow::{anyhow, Result};
use oos_infer::engine::{Engine, GenParams, Generation};
use oos_infer::openai::ChatMessage;
use oos_infer::ModelFiles;
use tokenizers::Tokenizer;

pub struct MlxEngine {
    model_id: String,
    tokenizer: Tokenizer,
    #[cfg(feature = "mlx")]
    model: std::sync::Mutex<Box<dyn crate::models::Model>>,
}

impl MlxEngine {
    pub fn load(files: &ModelFiles, model_id: String) -> Result<Self> {
        let tokenizer = Tokenizer::from_file(&files.tokenizer_json)
            .map_err(|e| anyhow!("loading tokenizer {}: {e}", files.tokenizer_json.display()))?;

        #[cfg(feature = "mlx")]
        let model = {
            let m = crate::models::load(files, &tokenizer)?;
            tracing::info!(layers = m.num_layers(), "model loaded");
            std::sync::Mutex::new(m)
        };

        Ok(Self {
            model_id,
            tokenizer,
            #[cfg(feature = "mlx")]
            model,
        })
    }
}

impl Engine for MlxEngine {
    fn model_id(&self) -> &str {
        &self.model_id
    }

    #[cfg(feature = "mlx")]
    fn generate(&self, messages: &[ChatMessage], params: &GenParams) -> Result<Generation> {
        let model = self
            .model
            .lock()
            .map_err(|_| anyhow!("model mutex poisoned"))?;

        let prompt = model.render_prompt(messages);
        let encoding = self
            .tokenizer
            .encode(prompt, false)
            .map_err(|e| anyhow!("tokenize: {e}"))?;
        let prompt_ids: Vec<i32> = encoding.get_ids().iter().map(|&u| u as i32).collect();
        let prompt_tokens = prompt_ids.len();

        // Prefill the prompt on the first step; afterwards feed only the new
        // token and let the KV cache stand in for the rest of the prefix.
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

        let text = self
            .tokenizer
            .decode(&out, true)
            .map_err(|e| anyhow!("detokenize: {e}"))?;

        Ok(Generation {
            text,
            prompt_tokens,
            completion_tokens: out.len(),
        })
    }

    #[cfg(not(feature = "mlx"))]
    fn generate(&self, messages: &[ChatMessage], _params: &GenParams) -> Result<Generation> {
        let user = messages
            .iter()
            .rev()
            .find(|m| m.role == "user")
            .map(|m| m.content.as_str())
            .unwrap_or("");
        let prompt_tokens = self
            .tokenizer
            .encode(user, false)
            .map_err(|e| anyhow!("tokenize: {e}"))?
            .len();
        let text = format!(
            "[oosmlx] built without the `mlx` feature: tokenizer OK ({prompt_tokens} prompt tokens), \
             but the MLX forward pass is not compiled in. Rebuild with `--features mlx`."
        );
        Ok(Generation {
            text,
            prompt_tokens,
            completion_tokens: 0,
        })
    }
}
