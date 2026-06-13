#!/bin/bash
# One-shot load+generate smoke for a single oosmlx model (the qwen3_5_moe
# 35B-A3B). Builds release, frees any resident ollama model (note 170), starts
# the server once, sends one short greedy request, prints the completion plus
# the timing line, and tears the server down.
#
# usage: qwen_moe_smoke.sh [hf-model-id] [gen-tokens] [prompt]
set -u
cd "$(dirname "$0")/../../.."
MODEL="${1:-mlx-community/Qwen3.6-35B-A3B-4bit}"
GEN="${2:-48}"
PROMPT="${3:-Erklaere in drei Saetzen, warum der Himmel blau ist.}"
PORT="${OOSMLX_AB_PORT:-8096}"
export PATH="$HOME/.cargo/bin:/opt/homebrew/bin:/usr/bin:/bin:/usr/sbin:/sbin"
unset OOSMLX_DRAFT_MODEL NATS_URL OOSMLX_GATED_DELTA

# Free any resident ollama model first (note 170: 35B needs the RAM).
resident=$(curl -s --max-time 3 127.0.0.1:11434/api/ps 2>/dev/null \
  | python3 -c 'import json,sys; print(" ".join(m["name"] for m in json.load(sys.stdin).get("models",[])))' 2>/dev/null)
for m in $resident; do
  curl -s 127.0.0.1:11434/api/generate -d "{\"model\":\"$m\",\"keep_alive\":0}" >/dev/null
done
[ -n "$resident" ] && { echo "unloaded ollama: $resident"; sleep 2; }

BIN=target/release/oosmlx
echo "building $BIN ..."
cargo build -p oosmlx --release --features mlx >/dev/null || { echo "build failed"; exit 1; }

LOG=/tmp/oosmlx_qwen_moe_smoke.log
: >"$LOG"
RUST_LOG=oosmlx=info "$BIN" "127.0.0.1:$PORT" >"$LOG" 2>&1 &
srv=$!
up=""
for i in $(seq 1 90); do
  curl -s --max-time 2 "127.0.0.1:$PORT/v1/models" >/dev/null 2>&1 && { up=1; break; }
  sleep 1
done
if [ -z "$up" ]; then kill $srv 2>/dev/null; echo "SERVER_NOT_UP"; tail -8 "$LOG"; exit 1; fi

python3 - "$MODEL" "$PORT" "$PROMPT" "$GEN" <<'PYEOF'
import json, sys, urllib.request
model, port, msg, gen = sys.argv[1], sys.argv[2], sys.argv[3], int(sys.argv[4])
req = json.dumps({"model": model, "messages": [{"role": "user", "content": msg}],
                  "temperature": 0, "max_tokens": gen}).encode()
r = urllib.request.Request(f"http://127.0.0.1:{port}/v1/chat/completions",
                           data=req, headers={"Content-Type": "application/json"})
try:
    resp = json.load(urllib.request.urlopen(r, timeout=3600))
except Exception as e:
    print("REQUEST_FAILED:", e); sys.exit(1)
if "error" in resp:
    print("SERVER_ERROR:", resp["error"]["message"]); sys.exit(1)
print(resp["choices"][0]["message"]["content"])
PYEOF
rc=$?
echo "=== phases ==="
grep -o 'generation phases.*' "$LOG" | sed 's/\x1b\[[0-9;]*m//g' | tail -1
[ $rc -ne 0 ] && { echo "--- server log tail ---"; tail -12 "$LOG"; }
kill $srv 2>/dev/null; wait $srv 2>/dev/null
exit $rc
