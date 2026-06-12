#!/bin/bash
# Three-way inference benchmark: oosmlx vs mlx_lm vs Ollama on one model.
#
# Measures warm prefill/decode tok/s with the same ~2k-token prompt and the
# same generation length on every runtime. Each leg runs a short warmup
# generation first so Metal kernel compilation never pollutes the measured
# numbers, and the legs run strictly sequentially -- never two 26B-class
# models resident at once (see task note on the 2026-06-11 swap incident).
#
# usage: bench.sh <hf-model-id> [ollama-tag] [gen-tokens] [prompt-mult]
#   bench.sh mlx-community/gemma-4-26b-a4b-it-nvfp4 gemma4:26b-mlx 256
#   bench.sh mlx-community/gemma-4-26b-a4b-it-4bit "" 256 206   # ~7.2k context
#
# prompt-mult scales the filler prompt: 55 (default) is ~2k tokens, 206 is
# ~7.2k -- the long-context regime where prefill chunking and the KV ring
# carry the load. The oosmlx leg also reports a temperature-0.7 decode line:
# that is the default request path (on-device sampler included), which the
# temp-0 measurement alone cannot see regress.
#
# The Ollama leg is skipped when no tag is given. The mlx_lm leg expects a
# venv with mlx-lm installed; override with MLXLM_PYTHON (default
# /tmp/mlxlm-venv/bin/python -- recreate via:
#   python3 -m venv /tmp/mlxlm-venv && /tmp/mlxlm-venv/bin/pip install mlx-lm).
set -u
cd "$(dirname "$0")/../../.."

MODEL="${1:?usage: bench.sh <hf-model-id> [ollama-tag] [gen-tokens] [prompt-mult]}"
OLLAMA_TAG="${2:-}"
GEN="${3:-256}"
MULT="${4:-55}"
PORT="${OOSMLX_BENCH_PORT:-8093}"
MLXLM_PYTHON="${MLXLM_PYTHON:-/tmp/mlxlm-venv/bin/python}"
PROMPT_FILE=/tmp/oosmlx_bench_prompt.txt
export PATH="$HOME/.cargo/bin:/opt/homebrew/bin:/usr/bin:/bin:/usr/sbin:/sbin"

# Deterministic filler prompt, MULT repetitions of ~36 tokens: at the default
# 55 (~2k tokens) prefill dominates its phase and the sliding-window (1024)
# machinery is exercised while a full leg stays in the tens of seconds; 206
# (~7.2k) probes the long-context regime.
python3 - "$PROMPT_FILE" "$MULT" <<'PYEOF'
import sys
filler = ("Die Lagerhalle in Dortmund verzeichnete im dritten Quartal "
          "einen deutlichen Anstieg der Durchlaufzeiten, weil die neue "
          "Sortieranlage erst teilweise kalibriert war. ") * int(sys.argv[2])
open(sys.argv[1], "w").write(
    filler + "\nFasse den obigen Text ausfuehrlich zusammen und bewerte die Lage.")
PYEOF

# Refuse to measure against a machine that already holds a resident model.
resident=$(curl -s --max-time 3 127.0.0.1:11434/api/ps 2>/dev/null \
  | python3 -c 'import json,sys; print(" ".join(m["name"] for m in json.load(sys.stdin).get("models",[])))' 2>/dev/null)
if [ -n "$resident" ]; then
  echo "unloading resident ollama model(s): $resident"
  for m in $resident; do
    curl -s 127.0.0.1:11434/api/generate -d "{\"model\":\"$m\",\"keep_alive\":0}" >/dev/null
  done
  sleep 2
fi

echo "== leg 1: oosmlx ($MODEL) =="
BIN=target/release/oosmlx
if [ ! -x "$BIN" ]; then
  echo "building $BIN ..."
  cargo build -p oosmlx --release --features mlx >/dev/null || exit 1
fi
unset NATS_URL
LOG=/tmp/oosmlx_bench_server.log
RUST_LOG=oosmlx=info "$BIN" 127.0.0.1:$PORT >"$LOG" 2>&1 &
SRV=$!
trap 'kill $SRV 2>/dev/null' EXIT
for i in $(seq 1 60); do
  curl -s --max-time 2 127.0.0.1:$PORT/v1/models >/dev/null 2>&1 && break
  sleep 1
done
python3 - "$MODEL" "$PORT" "$PROMPT_FILE" "$GEN" <<'PYEOF'
import json, sys, urllib.request
model, port, pf, gen = sys.argv[1], sys.argv[2], sys.argv[3], int(sys.argv[4])
prompt = open(pf).read()
def chat(max_tokens, temp=0):
    req = json.dumps({"model": model,
                      "messages": [{"role": "user", "content": prompt}],
                      "temperature": temp, "max_tokens": max_tokens}).encode()
    r = urllib.request.Request(f"http://127.0.0.1:{port}/v1/chat/completions",
                               data=req, headers={"Content-Type": "application/json"})
    return json.load(urllib.request.urlopen(r, timeout=1800))
chat(8)        # warmup: load + kernel compile
resp = chat(gen)
if "error" in resp:
    print("oosmlx error:", resp["error"]["message"]); sys.exit(1)
# Default-path run: temperature 0.7 exercises the on-device sampler, whose
# cost the greedy measurement cannot see.
resp = chat(gen, 0.7)
if "error" in resp:
    print("oosmlx error:", resp["error"]["message"]); sys.exit(1)
PYEOF
[ $? -ne 0 ] && exit 1
kill $SRV 2>/dev/null; wait $SRV 2>/dev/null; trap - EXIT
# The last two phases lines are the temp-0 and the temp-0.7 measured runs.
phases=$(grep -o 'generation phases.*' "$LOG" | sed 's/\x1b\[[0-9;]*m//g' | tail -2 | head -1)
prefill=$(echo "$phases" | grep -o 'prefill_tps=[0-9]*' | cut -d= -f2)
decode=$(echo "$phases" | grep -o 'decode_tps=[0-9]*' | cut -d= -f2)
ptok=$(echo "$phases" | grep -o 'prompt_tokens=[0-9]*' | cut -d= -f2)
echo "LEG oosmlx prefill_tps=$prefill decode_tps=$decode (prompt=$ptok gen=$GEN)"
phases=$(grep -o 'generation phases.*' "$LOG" | sed 's/\x1b\[[0-9;]*m//g' | tail -1)
decode=$(echo "$phases" | grep -o 'decode_tps=[0-9]*' | cut -d= -f2)
echo "LEG oosmlx-temp0.7 decode_tps=$decode (default request path)"

echo "== leg 2: mlx_lm ($MODEL) =="
if [ ! -x "$MLXLM_PYTHON" ]; then
  echo "LEG mlx_lm SKIPPED ($MLXLM_PYTHON missing; see header for setup)"
else
  "$MLXLM_PYTHON" - "$MODEL" "$PROMPT_FILE" "$GEN" <<'PYEOF'
import sys
from mlx_lm import load, stream_generate
from mlx_lm.sample_utils import make_sampler
model_id, pf, gen = sys.argv[1], sys.argv[2], int(sys.argv[3])
model, tok = load(model_id)
msgs = [{"role": "user", "content": open(pf).read()}]
prompt = tok.apply_chat_template(msgs, add_generation_prompt=True)
sampler = make_sampler(temp=0.0)
for _ in stream_generate(model, tok, prompt, max_tokens=8, sampler=sampler):
    pass  # warmup
last = None
for last in stream_generate(model, tok, prompt, max_tokens=gen, sampler=sampler):
    pass
print(f"LEG mlx_lm prefill_tps={last.prompt_tps:.0f} "
      f"decode_tps={last.generation_tps:.0f} "
      f"(prompt={last.prompt_tokens} gen={last.generation_tokens})")
PYEOF
fi

if [ -n "$OLLAMA_TAG" ]; then
  echo "== leg 3: ollama ($OLLAMA_TAG) =="
  python3 - "$OLLAMA_TAG" "$PROMPT_FILE" "$GEN" <<'PYEOF'
import json, sys, urllib.request
tag, pf, gen = sys.argv[1], sys.argv[2], int(sys.argv[3])
prompt = open(pf).read()
def chat(content, max_tokens):
    req = json.dumps({"model": tag, "stream": False,
                      "messages": [{"role": "user", "content": content}],
                      "options": {"temperature": 0, "num_predict": max_tokens}}).encode()
    r = urllib.request.Request("http://127.0.0.1:11434/api/chat", data=req,
                               headers={"Content-Type": "application/json"})
    return json.load(urllib.request.urlopen(r, timeout=1800))
# Warmup on a *different* prompt: Ollama caches the KV prefix across
# requests, so warming with the measured prompt would skip its prefill.
chat("Antworte mit einem Wort: Hauptstadt von Frankreich?", 8)
r = chat(prompt, gen)
if r["prompt_eval_count"] < 100:
    print(f"WARN ollama prompt cache hit (prompt_eval_count={r['prompt_eval_count']}), prefill_tps unreliable")
prefill = r["prompt_eval_count"] / (r["prompt_eval_duration"] / 1e9)
decode = r["eval_count"] / (r["eval_duration"] / 1e9)
print(f"LEG ollama prefill_tps={prefill:.0f} decode_tps={decode:.0f} "
      f"(prompt={r['prompt_eval_count']} gen={r['eval_count']})")
# Leave the machine clean: a resident Ollama model plus the next oosmlx
# load is exactly the two-resident-26B situation the header forbids.
urllib.request.urlopen(urllib.request.Request(
    "http://127.0.0.1:11434/api/generate",
    data=json.dumps({"model": tag, "keep_alive": 0}).encode(),
    headers={"Content-Type": "application/json"}), timeout=30)
PYEOF
fi
