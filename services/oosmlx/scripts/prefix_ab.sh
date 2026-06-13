#!/bin/bash
# Prompt-prefix cache A/B: same long prompt sent twice to one oosmlx server.
#
# The first measured request prefills cold; the second must reuse the frozen
# prompt-prefix cache -- come out BYTE-IDENTICAL and skip almost all of the
# prefill. For a sliding-window family (gemma4) this is the decisive case:
# the prompt is far longer than the 1024 window, so the rotating slots have
# wrapped, yet reuse resumes FORWARD from the frozen reusable boundary (just
# before the empty thought-channel prefill) without ever trimming the ring.
#
# Greedy (temperature 0) so the two completions are deterministic and can be
# compared verbatim. OOSMLX_DRAFT_MODEL is cleared so temp-0 stays on the
# generic decode loop (the speculative path has its own cache and would
# bypass this feature). A short warmup request first absorbs model load and
# Metal kernel compilation so the cold/warm prefill_ms are comparable.
#
# usage: prefix_ab.sh <hf-model-id> [gen-tokens] [prompt-mult]
#   prefix_ab.sh mlx-community/gemma-4-26b-a4b-it-nvfp4 96 110   # ~3.9k ctx
set -u
cd "$(dirname "$0")/../../.."

MODEL="${1:?usage: prefix_ab.sh <hf-model-id> [gen-tokens] [prompt-mult]}"
GEN="${2:-96}"
MULT="${3:-110}"
PORT="${OOSMLX_AB_PORT:-8094}"
PROMPT_FILE=/tmp/oosmlx_prefix_ab_prompt.txt
export PATH="$HOME/.cargo/bin:/opt/homebrew/bin:/usr/bin:/bin:/usr/sbin:/sbin"
unset OOSMLX_DRAFT_MODEL NATS_URL

python3 - "$PROMPT_FILE" "$MULT" <<'PYEOF'
import sys
filler = ("Die Lagerhalle in Dortmund verzeichnete im dritten Quartal "
          "einen deutlichen Anstieg der Durchlaufzeiten, weil die neue "
          "Sortieranlage erst teilweise kalibriert war. ") * int(sys.argv[2])
open(sys.argv[1], "w").write(
    filler + "\nFasse den obigen Text in drei Saetzen zusammen.")
PYEOF

resident=$(curl -s --max-time 3 127.0.0.1:11434/api/ps 2>/dev/null \
  | python3 -c 'import json,sys; print(" ".join(m["name"] for m in json.load(sys.stdin).get("models",[])))' 2>/dev/null)
if [ -n "$resident" ]; then
  echo "unloading resident ollama model(s): $resident"
  for m in $resident; do
    curl -s 127.0.0.1:11434/api/generate -d "{\"model\":\"$m\",\"keep_alive\":0}" >/dev/null
  done
  sleep 2
fi

BIN=target/release/oosmlx
# Always rebuild: an existing binary from an earlier session would silently
# test stale code (the reason the first A/B run showed no reuse).
echo "building $BIN ..."
cargo build -p oosmlx --release --features mlx >/dev/null || exit 1
LOG=/tmp/oosmlx_prefix_ab_server.log
: >"$LOG"
RUST_LOG=oosmlx=info "$BIN" 127.0.0.1:$PORT >"$LOG" 2>&1 &
SRV=$!
trap 'kill $SRV 2>/dev/null' EXIT
for i in $(seq 1 60); do
  curl -s --max-time 2 127.0.0.1:$PORT/v1/models >/dev/null 2>&1 && break
  sleep 1
done

python3 - "$MODEL" "$PORT" "$PROMPT_FILE" "$GEN" <<'PYEOF'
import json, sys, time, urllib.request
model, port, pf, gen = sys.argv[1], sys.argv[2], sys.argv[3], int(sys.argv[4])
prompt = open(pf).read()
def chat(content, max_tokens):
    req = json.dumps({"model": model,
                      "messages": [{"role": "user", "content": content}],
                      "temperature": 0, "max_tokens": max_tokens}).encode()
    r = urllib.request.Request(f"http://127.0.0.1:{port}/v1/chat/completions",
                               data=req, headers={"Content-Type": "application/json"})
    t = time.time()
    resp = json.load(urllib.request.urlopen(r, timeout=1800))
    return resp, time.time() - t
def text(resp):
    if "error" in resp:
        print("server error:", resp["error"]["message"]); sys.exit(1)
    return resp["choices"][0]["message"]["content"]

chat("Antworte mit einem Wort: Hauptstadt von Frankreich?", 8)  # warmup
r_cold, t_cold = chat(prompt, gen)
r_warm, t_warm = chat(prompt, gen)
c, w = text(r_cold), text(r_warm)

print(f"cold  wall={t_cold:6.2f}s  chars={len(c)}")
print(f"warm  wall={t_warm:6.2f}s  chars={len(w)}")
if c == w:
    print("BYTE-IDENTICAL: PASS")
else:
    print("BYTE-IDENTICAL: FAIL")
    n = min(len(c), len(w))
    i = next((k for k in range(n) if c[k] != w[k]), n)
    print(f"  first diff at char {i}")
    print(f"  cold[{i}:{i+60}]={c[i:i+60]!r}")
    print(f"  warm[{i}:{i+60}]={w[i:i+60]!r}")
    sys.exit(2)
PYEOF
rc=$?
kill $SRV 2>/dev/null; wait $SRV 2>/dev/null; trap - EXIT

# The two measured 'generation phases' lines: cold then warm. cached_prefix
# proves reuse fired (0 cold, ~prompt length warm); prefill_ms shows the win.
echo "--- server phase lines (cold, then warm) ---"
grep -o 'generation phases.*' "$LOG" | sed 's/\x1b\[[0-9;]*m//g' | tail -2
exit $rc
