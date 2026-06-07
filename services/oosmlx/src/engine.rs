//! The MLX-backed Engine.
//!
//! With `--features mlx` this runs the real Gemma 3 forward pass (see
//! `model.rs`) in a greedy decode loop. Without the feature it loads the
//! tokenizer + config and returns a placeholder, so non-Apple/CI builds stay
//! green. Either way the API and the ooscuda contract are identical.

use anyhow::{anyhow, Context, Result};
use oos_infer::engine::{Engine, GenParams, Generation};
use oos_infer::openai::ChatMessage;
use oos_infer::ModelFiles;
use tokenizers::Tokenizer;

use crate::config::GemmaConfig;

pub struct MlxEngine {
    model_id: String,
    tokenizer: Tokenizer,
    #[allow(dead_code)]
    config: GemmaConfig,
    #[cfg(feature = "mlx")]
    model: std::sync::Mutex<crate::model::GemmaModel>,
}

impl MlxEngine {
    pub fn load(files: &ModelFiles, model_id: String) -> Result<Self> {
        let tokenizer = Tokenizer::from_file(&files.tokenizer_json)
            .map_err(|e| anyhow!("loading tokenizer {}: {e}", files.tokenizer_json.display()))?;
        let config = GemmaConfig::load(&files.config_json).context("loading model config")?;

        tracing::info!(
            layers = config.num_hidden_layers,
            hidden = config.hidden_size,
            heads = config.num_attention_heads,
            kv_heads = config.num_key_value_heads,
            head_dim = config.head_dim,
            vocab = config.vocab_size,
            "loaded Gemma config"
        );

        #[cfg(feature = "mlx")]
        let model = {
            let m = crate::model::GemmaModel::load(&files.dir, &config).context("loading weights")?;
            tracing::info!("MLX weights loaded");
            std::sync::Mutex::new(m)
        };

        Ok(Self {
            model_id,
            tokenizer,
            config,
            #[cfg(feature = "mlx")]
            model,
        })
    }

    /// Build a Gemma chat prompt for the last user turn. The turn markers are
    /// added tokens in Gemma's tokenizer, so we encode them literally.
    fn build_prompt(&self, messages: &[ChatMessage]) -> String {
        let user = messages
            .iter()
            .rev()
            .find(|m| m.role == "user")
            .map(|m| m.content.as_str())
            .unwrap_or("");
        format!("<bos><start_of_turn>user\n{user}<end_of_turn>\n<start_of_turn>model\n")
    }
}

impl Engine for MlxEngine {
    fn model_id(&self) -> &str {
        &self.model_id
    }

    #[cfg(feature = "mlx")]
    fn generate(&self, messages: &[ChatMessage], params: &GenParams) -> Result<Generation> {
        let prompt = self.build_prompt(messages);
        let encoding = self
            .tokenizer
            .encode(prompt, false)
            .map_err(|e| anyhow!("tokenize: {e}"))?;
        let ids: Vec<i32> = encoding.get_ids().iter().map(|&u| u as i32).collect();
        let prompt_tokens = ids.len();

        let eos = self.config.eos_token_id as i32;
        let end_of_turn = self
            .tokenizer
            .token_to_id("<end_of_turn>")
            .map(|id| id as i32);

        let model = self
            .model
            .lock()
            .map_err(|_| anyhow!("model mutex poisoned"))?;

        // Prefill the prompt on the first step; afterwards feed only the new
        // token and let the KV cache stand in for the rest of the prefix.
        let mut cache = crate::model::KvCache::new(self.config.num_hidden_layers);
        let mut step: Vec<i32> = ids;
        let mut out: Vec<u32> = Vec::new();
        for _ in 0..params.max_tokens {
            let next = model.forward_argmax(&step, &mut cache)?;
            if next == eos || Some(next) == end_of_turn {
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
        let prompt = self.build_prompt(messages);
        let prompt_tokens = self
            .tokenizer
            .encode(prompt, false)
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
