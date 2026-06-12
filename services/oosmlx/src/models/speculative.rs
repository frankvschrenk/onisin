//! Speculative decoding: the Gemma 4 target paired with its MTP drafter.
//!
//! Google's MTP ("assistant") drafter predicts the target's own next tokens
//! from borrowed target state. A round drafts a short block, the target
//! verifies the whole block in *one* forward, and the longest matching prefix
//! is accepted -- so the expensive target runs once per round instead of once
//! per token. Upstream (mlx-vlm) measured ~3.9x tokens/s on the 26B at block
//! size 4 on an M3 Max.
//!
//! Greedy only: at temperature 0 acceptance is exact token equality, so the
//! output is the target's own greedy completion. Sampled requests bypass this
//! path entirely (exactness there would need rejection sampling, which is not
//! implemented), as does every model without a paired drafter -- the seam is
//! `Model::generate_greedy`, defaulting to "no accelerated path".

use anyhow::{bail, Result};
use mlx_rs::ops::indexing::IndexOp;
use mlx_rs::{ops, Array};
use oos_infer::openai::ChatMessage;
use oos_infer::{registry, ModelRef};

use super::gemma4::Gemma4Model;
use super::gemma4_assistant::{Gemma4AssistantConfig, Gemma4AssistantModel};
use super::{detect_arch, KvCache, Model};

/// Draft candidates per round; with the bonus token a verify block is
/// `DRAFT_STEPS + 1` wide. 4-wide blocks are what upstream measured with.
const DRAFT_STEPS: usize = 3;

/// Env opt-in for drafter pairing: unset or `off` = no speculation, anything
/// else = exactly this model (HF repo or path).
///
/// Opt-in rather than auto-paired: measured on the 26B with the mxfp4
/// assistant, acceptance on natural prose is too low to pay for the flat
/// per-round cost (~0.55 avg accepted of 3 on German, 0.81 on English, vs a
/// ~1.1 breakeven at ~80ms/round) -- speculation *slowed* real chat down,
/// while near-perfectly predictable text (counting: 2.94) confirms the
/// machinery itself is sound. Until rounds get cheaper or acceptance is
/// gated adaptively, pairing by default would tax every greedy request.
const DRAFT_ENV: &str = "OOSMLX_DRAFT_MODEL";

/// Find the explicitly requested MTP drafter for `target`. Best-effort by
/// design: any failure logs and yields `None`, the target then simply runs
/// without speculation.
pub(super) fn find_drafter(target: &Gemma4Model) -> Option<Gemma4AssistantModel> {
    let id = match std::env::var(DRAFT_ENV) {
        Ok(v) if !v.is_empty() && v != "off" => v,
        _ => return None,
    };
    match try_drafter(&id, target) {
        Ok(drafter) => {
            tracing::info!(drafter = %id, "speculative MTP drafter paired");
            Some(drafter)
        }
        Err(e) => {
            tracing::warn!(drafter = %id, error = %e, "requested drafter rejected");
            None
        }
    }
}

fn try_drafter(id: &str, target: &Gemma4Model) -> Result<Gemma4AssistantModel> {
    let files = registry::resolve(&ModelRef::parse(id))?;
    let arch = detect_arch(&files.config_json)?.to_lowercase();
    if !(arch.contains("gemma4") && arch.contains("assistant")) {
        bail!("not a gemma4_assistant architecture: {arch}");
    }
    // The drafter is target-coupled; validate the fit on the config alone
    // before loading any weights.
    let cfg = Gemma4AssistantConfig::load(&files.config_json)?;
    let t = target.config();
    if cfg.backbone_hidden_size != t.hidden_size || cfg.text.vocab_size != t.vocab_size {
        bail!(
            "drafter does not fit the target: backbone {} vs hidden {}, vocab {} vs {}",
            cfg.backbone_hidden_size,
            t.hidden_size,
            cfg.text.vocab_size,
            t.vocab_size
        );
    }
    Gemma4AssistantModel::load(&files)
}

/// A target with its paired drafter. Implements `Model` by delegating to the
/// target, so transports, sampling and every non-greedy path behave exactly
/// as without a drafter; only `generate_greedy` runs the speculative loop.
pub(super) struct SpecPair {
    target: Gemma4Model,
    drafter: Gemma4AssistantModel,
}

impl SpecPair {
    pub(super) fn new(target: Gemma4Model, drafter: Gemma4AssistantModel) -> Self {
        Self { target, drafter }
    }

    /// The speculative round-loop, mirroring the engine's generic greedy loop
    /// contract: at most `max_tokens` ids, cut *before* the first stop token.
    fn run(&self, prompt: &[i32], max_tokens: usize) -> Result<Vec<u32>> {
        let stop = self.target.stop_tokens();
        let mut out: Vec<u32> = Vec::new();
        if max_tokens == 0 {
            return Ok(out);
        }

        // Linear cache: the rollback below must restore positions a rotating
        // sliding-window slot would already have evicted.
        let mut cache = KvCache::new_linear(self.target.num_layers());

        // Prefill through the export hooks. The greedy next token (the
        // "bonus") seeds the first draft block; the last pre-norm hidden row
        // seeds the drafter's recurrence.
        let hidden = self.target.forward_hidden(prompt, &mut cache)?;
        let width = hidden.shape()[1];
        let last = hidden.shape()[0] - 1;
        let mut h_prev = hidden.index(last).reshape(&[1, width])?;
        let mut bonus = argmax_scalar(&self.target.project_logits(&h_prev)?)?;
        if stop.contains(&bonus) {
            return Ok(out);
        }
        out.push(bonus as u32);

        while out.len() < max_tokens {
            // Snapshot the borrowed state. The cache holds only verified
            // positions here, so the drafter never attends rolled-back keys.
            let shared = self.target.shared_kv(&cache)?;
            let pos = cache.offset() as i32;

            // Draft up to DRAFT_STEPS candidates; a round emits at most
            // drafts + 1 tokens, so the remaining budget caps the draft.
            let remaining = max_tokens - out.len();
            let draft_n = DRAFT_STEPS.min(remaining.saturating_sub(1));
            let mut drafts: Vec<i32> = Vec::with_capacity(draft_n);
            let mut tok = bonus;
            let mut h = h_prev.clone();
            for _ in 0..draft_n {
                let emb = self.target.embed(&[tok])?;
                let inputs = ops::concatenate_axis(&[emb, h], 1)?;
                let (hid, logits) = self.drafter.forward(&inputs, &shared, pos)?;
                tok = argmax_scalar(&logits)?;
                drafts.push(tok);
                h = hid;
            }

            // Verify the whole block -- bonus plus drafts -- in one target
            // forward. Row i's argmax is the target's own choice after
            // block[..=i], i.e. the truth draft i is judged against.
            let mut block = Vec::with_capacity(1 + drafts.len());
            block.push(bonus);
            block.extend_from_slice(&drafts);
            let hidden = self.target.forward_hidden(&block, &mut cache)?;
            let logits = self.target.project_logits(&hidden)?;
            let preds = ops::indexing::argmax_axis(&logits, -1, false)?;
            preds.eval()?;
            let preds: Vec<i32> = preds.as_slice::<u32>().iter().map(|&u| u as i32).collect();

            let mut accepted = 0;
            while accepted < drafts.len() && drafts[accepted] == preds[accepted] {
                accepted += 1;
            }
            tracing::debug!(accepted, drafted = drafts.len(), "speculative round");

            // Emit the accepted drafts, then the target's token at the
            // divergence -- the free extra token when everything matched.
            for &t in &drafts[..accepted] {
                if stop.contains(&t) {
                    return Ok(out);
                }
                out.push(t as u32);
                if out.len() >= max_tokens {
                    return Ok(out);
                }
            }
            let next = preds[accepted];
            if stop.contains(&next) {
                return Ok(out);
            }
            out.push(next as u32);

            // Roll the cache back to the verified prefix (bonus + accepted
            // drafts); on full acceptance the whole block is already valid.
            // The next bonus is *not* in the cache -- it leads the next block.
            cache.truncate(pos as usize + 1 + accepted)?;
            h_prev = hidden.index(accepted as i32).reshape(&[1, width])?;
            bonus = next;
        }
        Ok(out)
    }
}

/// Greedy pick from one logit row, matching `pick` at temperature 0.
fn argmax_scalar(logits: &Array) -> Result<i32> {
    let next = ops::indexing::argmax(logits, false)?;
    Ok(next.item::<u32>() as i32)
}

impl Model for SpecPair {
    fn num_layers(&self) -> usize {
        self.target.num_layers()
    }

    fn forward_logits(&self, tokens: &[i32], cache: &mut KvCache) -> Result<Array> {
        self.target.forward_logits(tokens, cache)
    }

    fn render_prompt(
        &self,
        messages: &[ChatMessage],
        thinking: bool,
        tools: &[oos_infer::openai::Tool],
    ) -> String {
        self.target.render_prompt(messages, thinking, tools)
    }

    fn stop_tokens(&self) -> &[i32] {
        self.target.stop_tokens()
    }

    fn reasoning_channel(&self) -> Option<crate::models::ReasoningChannel> {
        self.target.reasoning_channel()
    }

    fn tool_call_markers(&self) -> Option<crate::models::ToolCallMarkers> {
        self.target.tool_call_markers()
    }

    fn parse_tool_call(&self, span: &str) -> Result<(String, String)> {
        self.target.parse_tool_call(span)
    }

    fn generate_greedy(&self, prompt: &[i32], max_tokens: usize) -> Result<Option<Vec<u32>>> {
        self.run(prompt, max_tokens).map(Some)
    }
}
