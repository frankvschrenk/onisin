# oosmlx

A native Rust inference engine for Apple Silicon, built on [mlx-rs](https://github.com/oxidecomputer/mlx-rs).
Serves models over an **OpenAI-compatible HTTP API** and optionally over **NATS Request-Reply** — drop-in
replacement for Ollama in local and edge deployments.

> **Status:** active development, production use within Onisin OS.
> The API surface is stable; model coverage is expanding.

---

## Why

Ollama is convenient but opaque: quantization choices, sampling, and KV-cache behaviour are hidden.
oosmlx exposes all of that while staying wire-compatible with the OpenAI Chat Completions API,
so any client that already works with Ollama or vLLM works without changes.

Because the forward pass is written directly against mlx-rs (not Python), the engine avoids
the Python GIL, subprocess overhead, and the f32 scalar constants that drag bf16 compute
graphs to f32 in the mlx Python bindings. On a 26B MoE model this yields:

| Metric | oosmlx | mlx_lm | Ollama |
|---|---|---|---|
| Prefill (tok/s, ~2k ctx) | **360** | 280 | 110 |
| Decode (tok/s, temp 0.7) | **32** | 30 | 28 |

*Measured on M2 Max 96 GB with `mlx-community/gemma-4-26b-a4b-it-nvfp4`. Numbers vary by model and hardware.*

---

## Supported models

| Family | Example HF repo | Notes |
|---|---|---|
| **Gemma 4** | `mlx-community/gemma-4-26b-a4b-it-nvfp4` | MoE, speculative decoding via `gemma4_assistant` drafter |
| **Gemma 3** | `mlx-community/gemma-3-12b-it-bf16` | Dense, good European language support |
| **Mistral / Devstral** | `mlx-community/Devstral-Small-2-24B-MXFP4` | Strong for code, tool calling |
| **Qwen 3.5** | `mlx-community/Qwen3-30B-A3B-4bit` | GatedDeltaNet + gated attention hybrid |

All quantization formats supported by the corresponding mlx-community repo work: bf16, 4-bit, 8-bit, mxfp4, mxfp8.

---

## Requirements

- Apple Silicon Mac (M1 or later), macOS 14+
- Rust toolchain (`rustup` recommended)
- ~first compile takes 10–20 min: mlx-rs builds the MLX C++ library from source

---

## Build

```bash
# From the repo root
cargo build --release -p oosmlx --features oosmlx/mlx
```

The `mlx` feature gates the actual Apple Silicon forward pass.
Without it the binary compiles and parses arguments but rejects inference requests —
useful for CI on non-Apple hardware.

---

## Run

```bash
# Serve on the default address (127.0.0.1:8080)
./target/release/oosmlx

# Preload a model so the first request is warm
./target/release/oosmlx --preload mlx-community/gemma-4-26b-a4b-it-nvfp4

# Custom address
./target/release/oosmlx --preload mlx-community/gemma-3-12b-it-bf16 0.0.0.0:8088
```

### Environment variables

| Variable | Default | Purpose |
|---|---|---|
| `NATS_URL` | *(unset)* | Enable NATS transport, e.g. `nats://127.0.0.1:4222` |
| `OOS_INFER_SUBJECT` | `oos.cmd.infer` | NATS subject prefix |
| `HF_HOME` | `~/.cache/huggingface` | Hugging Face model cache; `GET /v1/models` scans its `hub/` subdirectory |
| `RUST_LOG` | `info` | Log filter (uses `tracing-subscriber` env-filter syntax) |

---

## API

HTTP is always served. NATS is served additionally when `NATS_URL` is set.

### HTTP

```
GET  /v1/models                 — list models found in the local HF cache
POST /v1/chat/completions       — OpenAI-compatible chat, streaming (SSE) + blocking
```

The `model` field in the request selects and loads the model on demand.
Any model previously loaded stays resident until a different one is requested
(LRU eviction is planned; for now only one model is resident at a time).

### Request example

```bash
curl http://127.0.0.1:8080/v1/chat/completions \
  -H 'Content-Type: application/json' \
  -d '{
    "model": "mlx-community/gemma-4-26b-a4b-it-nvfp4",
    "messages": [{"role": "user", "content": "Explain KV caching in one paragraph."}],
    "stream": true,
    "temperature": 0.7,
    "max_tokens": 512
  }'
```

### NATS

When `NATS_URL` is set, two additional Request-Reply subjects are served:

```
{prefix}.models    — same as GET /v1/models
{prefix}.chat      — same as POST /v1/chat/completions
```

Default prefix: `oos.cmd.infer`.

---

## Tool calling

Gemma 4 and Mistral families support native tool calling via the standard
`tools` / `tool_choice` OpenAI fields. The template is applied by `toolfmt.rs`
which selects the correct native format per model family:

- Gemma 4 → `<|tool>…<tool|>` tokens
- Mistral → `[AVAILABLE_TOOLS]…[/AVAILABLE_TOOLS]` + `[TOOL_CALLS]` format

---

## Thinking / reasoning mode

Models that support extended reasoning (Gemma 4 27B IT, Qwen 3.5 with thinking)
respect the `enable_thinking` flag in the request:

```json
{ "enable_thinking": true }
```

Reasoning content is returned in the `reasoning_content` field of the response.

---

## Speculative decoding

For Gemma 4, a dedicated assistant model (`gemma4_assistant.rs`) acts as the
drafter in an MTP (Multi-Token Prediction) speculative decoding loop.
Pass `--preload` with the assistant model id to warm it at startup.

---

## Benchmarking

The `scripts/` directory contains shell scripts used during development:

| Script | Purpose |
|---|---|
| `bench.sh` | Three-way benchmark: oosmlx vs mlx_lm vs Ollama |
| `perf_matrix.sh` | 3×3 matrix across model families and context lengths |
| `chat.sh` | Interactive multi-turn chat for manual testing |
| `prefix_ab.sh` | A/B comparison of prompt-prefix KV cache speedup |
| `tools_smoke.sh` | Smoke test for tool-calling end-to-end |

```bash
# Example: benchmark Gemma 4 26B against mlx_lm and Ollama at 2k context
scripts/bench.sh mlx-community/gemma-4-26b-a4b-it-nvfp4 gemma4:26b-mlx 256
```

---

## Architecture

```
oosmlx/
├── src/
│   ├── main.rs          — CLI, HTTP + NATS server startup
│   ├── engine.rs        — MlxEngine: model loading, request dispatch, KV cache
│   └── models/
│       ├── mod.rs        — shared types, quantization loader (mxfp4/mxfp8/bf16)
│       ├── gemma3.rs     — Gemma 3 dense forward pass
│       ├── gemma4.rs     — Gemma 4 MoE forward pass (40 layers, 256 experts top-8)
│       ├── gemma4_assistant.rs — MTP drafter for speculative decoding
│       ├── mistral.rs    — Mistral / Devstral forward pass
│       ├── qwen3_5.rs    — Qwen 3.5 GatedDeltaNet + gated attention hybrid
│       ├── speculative.rs — speculative decoding loop
│       └── toolfmt.rs    — tool call template renderer per model family
└── scripts/             — benchmarking and smoke-test scripts
```

HTTP server and NATS transport live in the shared `oos-infer` crate
(`crates/oos-infer`), which implements the `Engine` trait. oosmlx provides
the MLX backend; ooscuda (planned) will mirror the same layout with a CUDA backend.

---

## License

Business Source License 1.1 — see [LICENSE](../../LICENSE).
Co-copyright Frank von Schrenk and Tristan von Schrenk.
