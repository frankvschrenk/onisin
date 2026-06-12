//! Model families behind the MLX backend.
//!
//! oosmlx is not tied to one model: `load` reads the architecture from the
//! checkpoint's config.json and dispatches to a family that implements the
//! `Model` trait. Gemma 3 is the first family (it was the bootstrap target for
//! bringing the engine up); adding another -- Llama, Mistral, a newer Gemma --
//! is one new module plus one match arm in `load`, with nothing above this
//! trait changing. Unknown architectures fail loudly instead of misbehaving.
//!
//! Everything here is MLX-specific (it operates on `mlx_rs::Array`), so the
//! whole module sits behind the `mlx` feature; the sampling and KV-cache parts
//! are family-agnostic and shared by every `Model`.

mod gemma3;
mod gemma4;
mod toolfmt;
// Not a `Model` family: the MTP drafter for speculative decoding. It is
// consumed by the speculative round-loop, not by `load`'s dispatch.
mod gemma4_assistant;
mod speculative;

use std::path::Path;

use anyhow::{bail, Context, Result};
use mlx_rs::ops::indexing::{TryIndexMutOp, TryIndexOp};
use mlx_rs::{ops, Array, Dtype};
use oos_infer::openai::ChatMessage;
use oos_infer::ModelFiles;
use tokenizers::Tokenizer;

/// A loaded model family. Adding a family means adding an impl plus a dispatch
/// arm in [`load`]; the transports, API and decode loop above never change.
pub trait Model: Send {
    /// Number of transformer layers, used to size the KV cache.
    fn num_layers(&self) -> usize;

    /// Logits `[vocab]` for the next position, advancing `cache`. On the first
    /// call `tokens` is the whole prompt (prefill); on later calls it is the
    /// single most recent token.
    fn forward_logits(&self, tokens: &[i32], cache: &mut KvCache) -> Result<Array>;

    /// Render this family's chat prompt for the given messages. `thinking`
    /// asks for the family's reasoning mode where one exists (gemma4's
    /// thought channel); families without one ignore it. `tools` are
    /// advertised functions rendered into the family's native declaration
    /// grammar; families without one ignore them.
    fn render_prompt(
        &self,
        messages: &[ChatMessage],
        thinking: bool,
        tools: &[oos_infer::openai::Tool],
    ) -> String;

    /// Token ids that stop generation (eos plus any turn terminator).
    fn stop_tokens(&self) -> &[i32];

    /// Model-specific accelerated greedy generation: the whole completion in
    /// one call, or `None` when the family has no accelerated path and the
    /// engine should run its generic per-token loop. Implementations mirror
    /// that loop's contract: greedy, at most `max_tokens` ids, cut *before*
    /// the first stop token. Today only the speculative target/drafter pair
    /// overrides this.
    fn generate_greedy(&self, _prompt: &[i32], _max_tokens: usize) -> Result<Option<Vec<u32>>> {
        Ok(None)
    }

    /// The family's reasoning channel, when it has one: the engine uses the
    /// marker ids to route thinking-channel tokens out of the answer (both
    /// for streaming and for the final reasoning/content split), and the
    /// channel name to strip the `{name}\n` line the model opens with.
    fn reasoning_channel(&self) -> Option<ReasoningChannel> {
        None
    }

    /// The family's tool-call block markers, when it has a native tool
    /// grammar: the engine captures `open..close` token spans out of the
    /// answer and hands the decoded spans to [`Model::parse_tool_call`].
    fn tool_call_markers(&self) -> Option<ToolCallMarkers> {
        None
    }

    /// Parse one decoded tool-call span (markers excluded, special tokens
    /// kept) into `(function name, JSON-encoded arguments object)`. Only
    /// meaningful for families that report [`Model::tool_call_markers`].
    fn parse_tool_call(&self, _span: &str) -> Result<(String, String)> {
        bail!("this model family has no tool-call grammar")
    }
}

/// Marker ids of a model family's tool-call block; see
/// [`Model::tool_call_markers`].
#[derive(Debug, Clone, Copy)]
pub struct ToolCallMarkers {
    pub open: i32,
    pub close: i32,
}

/// Marker ids and name of a model family's reasoning channel; see
/// [`Model::reasoning_channel`].
#[derive(Debug, Clone, Copy)]
pub struct ReasoningChannel {
    pub open: i32,
    pub close: i32,
    pub name: &'static str,
}

/// Per-layer key/value cache for incremental decoding.
///
/// Each slot holds one layer's K/V post-RoPE in head-major `[1, nkv, cap, hd]`
/// buffers, written in place per step instead of re-concatenating the whole
/// history -- the concat cache copied O(n) per token per layer and dominated
/// decode throughput at long contexts. Sliding-window layers additionally
/// retain only the last `window` positions in a ring, capping their memory
/// and making their single-token decode mask vacuous. Owned by the decode
/// loop, so a `Model` stays stateless and shareable.
pub struct KvCache {
    slots: Vec<KvSlot>,
    offset: usize,
    linear: bool,
}

impl KvCache {
    /// A cache whose sliding-window slots rotate: only the last `window`
    /// positions per such layer are retained. The default for plain decoding.
    pub fn new(num_layers: usize) -> Self {
        Self::build(num_layers, false)
    }

    /// A cache that keeps every position in every slot. The speculative
    /// round-loop needs this: its rollback ([`KvCache::truncate`]) must
    /// restore positions a rotating slot would already have evicted.
    pub fn new_linear(num_layers: usize) -> Self {
        Self::build(num_layers, true)
    }

    fn build(num_layers: usize, linear: bool) -> Self {
        Self {
            slots: (0..num_layers).map(|_| KvSlot::new(linear)).collect(),
            offset: 0,
            linear,
        }
    }

    /// Positions already cached; also the RoPE/mask offset for the next step.
    pub fn offset(&self) -> usize {
        self.offset
    }

    /// Whether every slot keeps its full history. Masks for sliding layers
    /// must then exclude out-of-window keys themselves; with rotation the
    /// retention does that for single-token steps.
    pub fn is_linear(&self) -> bool {
        self.linear
    }

    /// Advance the position counter after a step processed `n` tokens.
    pub fn advance(&mut self, n: usize) {
        self.offset += n;
    }

    /// The per-layer slots, for a model to write this step's K/V into.
    pub fn slots_mut(&mut self) -> &mut [KvSlot] {
        &mut self.slots
    }

    /// Valid cached K/V of one layer in temporal order, for exporting to a
    /// consumer outside the forward pass (the speculative drafter borrows the
    /// target's accumulated keys/values). `None` until that layer cached
    /// anything.
    pub fn layer_kv(&self, layer: usize) -> Result<Option<(Array, Array)>> {
        self.slots[layer].temporal()
    }

    /// Roll the cache back to the first `len` positions. Speculative decoding
    /// verifies a whole draft block in one forward and then discards the part
    /// past the accepted prefix; with buffered slots the rollback is just a
    /// length reset -- stale positions are overwritten by the next write.
    /// Fails on a rotating slot that already evicted history (the round-loop
    /// therefore runs on [`KvCache::new_linear`]).
    pub fn truncate(&mut self, len: usize) -> Result<()> {
        for slot in &mut self.slots {
            slot.truncate(len)?;
        }
        self.offset = self.offset.min(len);
        Ok(())
    }
}

/// Buffer growth chunk: the per-step cost is one in-place slice write, with a
/// copy of the valid prefix only every `GROW` positions.
const GROW: i32 = 256;

/// One layer's K/V storage: pre-allocated `[1, nkv, cap, hd]` buffers with the
/// first `len` positions valid. Linear slots grow without bound; rotating
/// slots (sliding-window layers) cap at the window and overwrite the oldest
/// position ring-style.
pub struct KvSlot {
    k: Option<Array>,
    v: Option<Array>,
    cap: i32,
    /// Valid positions stored (the temporal length retained).
    len: i32,
    /// Next ring write index; equals `len % cap` while content is temporal.
    idx: i32,
    /// Whether the ring ever dropped a position; rollback past that point is
    /// impossible.
    evicted: bool,
    /// Ignore any retention window and grow without bound (linear cache).
    force_linear: bool,
}

impl KvSlot {
    fn new(force_linear: bool) -> Self {
        Self {
            k: None,
            v: None,
            cap: 0,
            len: 0,
            idx: 0,
            evicted: false,
            force_linear,
        }
    }

    /// Store this step's K/V (`[1, nkv, seq, hd]`, post-RoPE) and return the
    /// K/V to attend over. The returned arrays are handles into the buffers,
    /// not copies. A wrapped ring is returned in rotated (non-temporal) order;
    /// that is sound because it only happens when every retained key passes
    /// the causal+window test (single-token decode), and attention without a
    /// mask is permutation-invariant over keys -- positions live in the
    /// RoPE'd keys themselves, not in storage order.
    pub fn update(
        &mut self,
        k_new: &Array,
        v_new: &Array,
        window: Option<i32>,
    ) -> Result<(Array, Array)> {
        let window = if self.force_linear { None } else { window };
        let seq = k_new.shape()[2];

        if let Some(w) = window {
            // Steady-state ring decode: one in-place write over the oldest
            // position, no growth, no copy -- the hot path.
            if seq == 1 && self.len == w {
                let i = self.idx;
                let k = self.k.as_mut().expect("full ring has buffers");
                k.try_index_mut((.., .., i..i + 1, ..), k_new)?;
                let v = self.v.as_mut().expect("full ring has buffers");
                v.try_index_mut((.., .., i..i + 1, ..), v_new)?;
                self.idx = (i + 1) % w;
                self.evicted = true;
                return Ok((
                    self.k.clone().expect("set above"),
                    self.v.clone().expect("set above"),
                ));
            }
            // A multi-token step pushing past the window (long prefill, or a
            // follow-up turn): attend over retained-old + new in temporal
            // order -- the step's own early queries need the step's early
            // keys -- and persist only the trailing window.
            if self.len + seq > w {
                let (ka, va) = match self.temporal()? {
                    Some((ok, ov)) => (
                        ops::concatenate_axis(&[ok, k_new.clone()], 2)?,
                        ops::concatenate_axis(&[ov, v_new.clone()], 2)?,
                    ),
                    None => (k_new.clone(), v_new.clone()),
                };
                let total = self.len + seq;
                let keep = w.min(total);
                let kt = ka.try_index((.., .., total - keep..total, ..))?;
                let vt = va.try_index((.., .., total - keep..total, ..))?;
                self.replace(&kt, &vt, keep, w)?;
                self.idx = keep % w;
                self.evicted = self.evicted || total > w;
                return Ok((ka, va));
            }
            // Still filling the window: append like a linear slot, capacity
            // clamped to the window so the ring buffer is exact once full.
        }

        // Append into the growing buffer.
        let need = self.len + seq;
        self.ensure_cap(need, window, k_new)?;
        let (s, e) = (self.len, need);
        let k = self.k.as_mut().expect("ensure_cap allocated");
        k.try_index_mut((.., .., s..e, ..), k_new)?;
        let v = self.v.as_mut().expect("ensure_cap allocated");
        v.try_index_mut((.., .., s..e, ..), v_new)?;
        self.len = need;
        self.idx = self.len % self.cap;
        Ok((
            self.k.as_ref().expect("set above").try_index((
                ..,
                ..,
                0..self.len,
                ..,
            ))?,
            self.v.as_ref().expect("set above").try_index((
                ..,
                ..,
                0..self.len,
                ..,
            ))?,
        ))
    }

    /// Grow the buffers to hold `need` positions, chunked and clamped to the
    /// retention window; copies the valid prefix once per growth.
    fn ensure_cap(&mut self, need: i32, clamp: Option<i32>, like: &Array) -> Result<()> {
        if self.cap >= need {
            return Ok(());
        }
        let mut cap = (need + GROW - 1) / GROW * GROW;
        if let Some(w) = clamp {
            cap = cap.min(w);
        }
        let shape = like.shape();
        let grown = [shape[0], shape[1], cap, shape[3]];
        let mut k = ops::zeros_dtype(&grown, like.dtype())?;
        let mut v = ops::zeros_dtype(&grown, like.dtype())?;
        if self.len > 0 {
            let old_k = self.k.as_ref().expect("len > 0 implies buffers");
            let old_v = self.v.as_ref().expect("len > 0 implies buffers");
            k.try_index_mut(
                (.., .., 0..self.len, ..),
                old_k.try_index((.., .., 0..self.len, ..))?,
            )?;
            v.try_index_mut(
                (.., .., 0..self.len, ..),
                old_v.try_index((.., .., 0..self.len, ..))?,
            )?;
        }
        self.k = Some(k);
        self.v = Some(v);
        self.cap = cap;
        Ok(())
    }

    /// Re-seat the buffers with `keep` temporal positions at capacity `cap`.
    fn replace(&mut self, k: &Array, v: &Array, keep: i32, cap: i32) -> Result<()> {
        let shape = k.shape();
        let fresh = [shape[0], shape[1], cap, shape[3]];
        let mut kb = ops::zeros_dtype(&fresh, k.dtype())?;
        let mut vb = ops::zeros_dtype(&fresh, v.dtype())?;
        kb.try_index_mut((.., .., 0..keep, ..), k)?;
        vb.try_index_mut((.., .., 0..keep, ..), v)?;
        self.k = Some(kb);
        self.v = Some(vb);
        self.cap = cap;
        self.len = keep;
        Ok(())
    }

    /// Valid content in temporal order: a view for un-wrapped slots, a
    /// re-stitched concat once the ring has wrapped.
    fn temporal(&self) -> Result<Option<(Array, Array)>> {
        let (Some(k), Some(v)) = (self.k.as_ref(), self.v.as_ref()) else {
            return Ok(None);
        };
        if self.len == 0 {
            return Ok(None);
        }
        Ok(Some(if self.len == self.cap && self.idx != 0 {
            let i = self.idx;
            (
                ops::concatenate_axis(
                    &[
                        k.try_index((.., .., i..self.len, ..))?,
                        k.try_index((.., .., 0..i, ..))?,
                    ],
                    2,
                )?,
                ops::concatenate_axis(
                    &[
                        v.try_index((.., .., i..self.len, ..))?,
                        v.try_index((.., .., 0..i, ..))?,
                    ],
                    2,
                )?,
            )
        } else {
            (
                k.try_index((.., .., 0..self.len, ..))?,
                v.try_index((.., .., 0..self.len, ..))?,
            )
        }))
    }

    /// Roll back to the first `len` absolute positions: a length reset, the
    /// stale tail is overwritten by later writes. Only sound while the slot
    /// still holds its full history.
    fn truncate(&mut self, len: usize) -> Result<()> {
        let len = len as i32;
        if self.len <= len {
            return Ok(());
        }
        if self.evicted {
            bail!("cannot truncate a KV slot past evicted positions (use a linear cache)");
        }
        self.len = len;
        if self.cap > 0 {
            self.idx = self.len % self.cap;
        }
        Ok(())
    }
}

/// One step's attention masks, built once per forward step and shared by all
/// layers of a family. Hoisted out of the per-layer path because an explicit
/// `[seq, klen]` mask is the one potentially large allocation per step; the
/// `None`/`Causal` kinds run SDPA's fused paths with nothing materialized.
pub(crate) struct StepMasks {
    pub(crate) full: MaskKind,
    pub(crate) sliding: MaskKind,
}

pub(crate) enum MaskKind {
    /// No mask needed: every cached key passes the causal+window test.
    None,
    /// SDPA's fused causal mode. MLX aligns the queries to the *last* `seq`
    /// key positions, which matches the cached-prefix layout (including
    /// speculative multi-token verify steps) for any key start.
    Causal,
    /// An explicit additive mask, built on device (sliding window only).
    Mask(Array),
}

/// Build the masks for one step at absolute position `offset`, `seq` tokens
/// wide. `retained` says sliding slots rotate (keep only the trailing
/// `window`): their keys then start at `offset - min(offset, window)` instead
/// of 0, and a single-token step needs no sliding mask at all -- retention
/// already evicted everything out of window. `dtype` is the family's compute
/// dtype: an additive mask must match the attention scores, or SDPA promotes
/// the whole score tensor and silently drops back to wider compute.
pub(crate) fn step_masks(
    offset: i32,
    seq: i32,
    window: i32,
    retained: bool,
    dtype: Dtype,
) -> Result<StepMasks> {
    let full = if seq <= 1 {
        MaskKind::None
    } else {
        MaskKind::Causal
    };

    let old = if retained { offset.min(window) } else { offset };
    let key_start = offset - old;
    let sliding = if seq <= 1 && retained {
        MaskKind::None
    } else if old + seq - 1 < window {
        // Even the oldest attended key is within the window of the newest
        // query; the constraint is vacuous and plain causal remains.
        if seq <= 1 {
            MaskKind::None
        } else {
            MaskKind::Causal
        }
    } else {
        MaskKind::Mask(sliding_mask(offset, seq, key_start, old + seq, window, dtype)?)
    };
    Ok(StepMasks { full, sliding })
}

/// Additive sliding-window mask `[1, 1, seq, klen]`, built on device: the
/// position grids come from `arange`, the comparisons and the select stay
/// lazy MLX ops, so nothing crosses the host. Query row `qi` sits at absolute
/// position `offset + qi`, key column `kj` at `key_start + kj`; a position is
/// allowed when causal (`kpos <= qpos`) and within the window
/// (`qpos - kpos < window`). Disallowed positions get a large finite
/// negative -- effectively -inf for the softmax, but finite to avoid NaN.
fn sliding_mask(
    offset: i32,
    seq: i32,
    key_start: i32,
    klen: i32,
    window: i32,
    dtype: Dtype,
) -> Result<Array> {
    let q = Array::arange::<_, i32>(offset, offset + seq, None)?.reshape(&[seq, 1])?;
    let k = Array::arange::<_, i32>(key_start, key_start + klen, None)?.reshape(&[1, klen])?;
    let causal = k.le(&q)?;
    let in_window = q.subtract(&k)?.lt(&Array::from_int(window))?;
    let allowed = causal.logical_and(&in_window)?;
    let zero = Array::from_slice(&[0.0f32], &[1]);
    let masked = Array::from_slice(&[-1e30f32], &[1]);
    let m = ops::r#where(&allowed, &zero, &masked)?.as_dtype(dtype)?;
    Ok(m.reshape(&[1, 1, seq, klen])?)
}

/// Choose the next token id from a logit row: greedy argmax when `temperature`
/// is non-positive (deterministic), otherwise temperature + top-p sampling.
pub fn pick(logits: &Array, temperature: f32, top_p: f32) -> Result<i32> {
    if temperature <= 0.0 {
        let next = ops::indexing::argmax(logits, false)?;
        Ok(next.item::<u32>() as i32)
    } else {
        let next = sample_top_p(logits, temperature, top_p)?;
        Ok(next.item::<u32>() as i32)
    }
}

/// Top-p (nucleus) sampling over one logit row, with temperature, built as
/// lazy device ops in the mlx_lm sampler shape: softmax over the scaled row,
/// ascending argsort plus cumulative sum, zero out everything outside the
/// nucleus, draw with `categorical` over the masked log-probabilities, and
/// map the draw back through the sort order.
///
/// On device because the previous CPU sampler pulled the full f32 logit row
/// (1 MB on a 262k vocabulary) to the host and sorted it -- several ms per
/// token on the *default* request path (GenParams temperature 0.7), invisible
/// to the temp-0 benchmarks. Here the whole sampler fuses into the forward's
/// evaluation and only a scalar crosses to the host. Softmax and cumsum run
/// in f32: a 262k-element cumulative sum in bf16 drowns exactly the small
/// tail probabilities top-p is supposed to weigh. `temperature` is > 0 here.
fn sample_top_p(logits: &Array, temperature: f32, top_p: f32) -> Result<Array> {
    let t = Array::from_slice(&[temperature], &[1]);
    let scaled = logits.as_dtype(Dtype::Float32)?.divide(&t)?;
    let probs = ops::softmax_axis(&scaled, -1, None)?;

    // Ascending sort: the nucleus is every position whose inclusive
    // cumulative mass exceeds 1 - top_p (the highest-probability tail).
    let order = ops::argsort_axis(&probs, -1)?;
    let sorted = ops::indexing::take_along_axis(&probs, &order, -1)?;
    let cum = sorted.cumsum(-1, None, None)?;
    let threshold = Array::from_slice(&[1.0 - top_p], &[1]);
    let keep = cum.gt(&threshold)?;
    let zero = Array::from_slice(&[0.0f32], &[1]);
    let masked = ops::r#where(&keep, &sorted, &zero)?;

    // `categorical` renormalises internally (softmax over log p restores the
    // nucleus distribution; log 0 = -inf drops the masked positions) and
    // draws an index into the sorted order; map it back to a vocabulary id.
    let drawn = mlx_rs::random::categorical(&masked.log()?, None, None, None)?;
    Ok(ops::indexing::take_along_axis(
        &order,
        &drawn.reshape(&[1])?,
        -1,
    )?)
}

#[cfg(all(test, feature = "mlx"))]
mod sampler_tests {
    use super::*;

    /// Two MLX-touching tests on parallel harness threads segfault inside
    /// Metal; the harness has no per-module serialization, so the tests take
    /// this lock instead of requiring --test-threads=1.
    static MLX_LOCK: std::sync::Mutex<()> = std::sync::Mutex::new(());

    /// A clear argmax with a collapsed nucleus must be picked every time:
    /// top_p = 0.01 keeps only positions whose inclusive ascending cumulative
    /// mass exceeds 0.99, which is the top token alone unless its probability
    /// is under 0.01.
    #[test]
    fn collapsed_nucleus_is_greedy() -> Result<()> {
        let _guard = MLX_LOCK.lock().expect("mlx test lock");
        let logits = Array::from_slice(&[0.0f32, 1.0, 2.0, 10.0], &[4]);
        for _ in 0..50 {
            let tok = sample_top_p(&logits, 0.8, 0.01)?;
            assert_eq!(tok.item::<u32>(), 3);
        }
        Ok(())
    }

    /// Classic nucleus boundary: probabilities (0.2, 0.3, 0.5) with
    /// top_p = 0.6 keep exactly {0.5, 0.3} -- the 0.2 token must never be
    /// drawn, and both nucleus members must appear over enough draws.
    #[test]
    fn nucleus_boundary_masks_the_tail() -> Result<()> {
        let _guard = MLX_LOCK.lock().expect("mlx test lock");
        let logits = Array::from_slice(&[0.2f32.ln(), 0.3f32.ln(), 0.5f32.ln()], &[3]);
        let mut seen = [0usize; 3];
        for _ in 0..200 {
            // Temperature 1.0 keeps the constructed probabilities exact.
            let tok = sample_top_p(&logits, 1.0, 0.6)?;
            seen[tok.item::<u32>() as usize] += 1;
        }
        assert_eq!(seen[0], 0, "tail token outside the nucleus was drawn");
        assert!(
            seen[1] > 0 && seen[2] > 0,
            "nucleus member never drawn: {seen:?}"
        );
        assert!(
            seen[2] > seen[1],
            "draw frequencies ignore probability: {seen:?}"
        );
        Ok(())
    }
}

#[derive(serde::Deserialize)]
struct ArchPeek {
    #[serde(default)]
    model_type: Option<String>,
    #[serde(default)]
    architectures: Option<Vec<String>>,
}

/// Read the model family from config.json: prefer the first `architectures`
/// entry (e.g. "Gemma3ForCausalLM"), fall back to `model_type`.
fn detect_arch(config_json: &Path) -> Result<String> {
    let text = std::fs::read_to_string(config_json)
        .with_context(|| format!("reading {}", config_json.display()))?;
    let peek: ArchPeek = serde_json::from_str(&text)
        .with_context(|| format!("parsing {}", config_json.display()))?;
    peek.architectures
        .and_then(|v| v.into_iter().next())
        .or(peek.model_type)
        .context("config.json has neither `architectures` nor `model_type`")
}

/// Load the model for `files`, dispatching on its architecture.
pub fn load(files: &ModelFiles, tokenizer: &Tokenizer) -> Result<Box<dyn Model>> {
    let arch = detect_arch(&files.config_json)?;
    let lower = arch.to_lowercase();
    // `gemma4_assistant` is a small speculative-decoding draft head, not the MoE
    // chat model -- same "gemma4" name, different topology. It must fall through
    // to the unsupported arm rather than mis-load through the MoE path (which
    // would die deep in config parsing with a misleading error).
    if lower.contains("gemma4") && !lower.contains("assistant") {
        let target = gemma4::Gemma4Model::load(files, tokenizer)?;
        // Pair an MTP drafter when one is explicitly requested (env opt-in,
        // see speculative::DRAFT_ENV for why not auto): greedy requests then
        // run the speculative round-loop; sampled requests and everything
        // else are unchanged. Pairing never fails the target load.
        Ok(match speculative::find_drafter(&target) {
            Some(drafter) => Box::new(speculative::SpecPair::new(target, drafter)),
            None => Box::new(target),
        })
    } else if lower.contains("gemma3") {
        Ok(Box::new(gemma3::Gemma3Model::load(files, tokenizer)?))
    } else {
        bail!("unsupported model architecture: {arch} (supported: gemma3, gemma4)")
    }
}
