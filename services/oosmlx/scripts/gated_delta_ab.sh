#!/bin/bash
# Kernel-vs-ops A/B for the GatedDeltaNet recurrence (Task #48).
#
# Runs the SAME greedy prompt through one oosmlx build twice: once on the
# sequential ops recurrence (default), once on the single-launch Metal kernel
# (OOSMLX_GATED_DELTA=kernel). temp 0 => deterministic, so the two completions
# must land in the same equivalence class -- ideally byte-identical, though the
# simd_sum reduction order plus bf16 kernel inputs may flip a boundary token
# (mlx_lm's own kernel does the same). The ops path is already byte-identical to
# mlx_lm (see 6af38a3 / qwen35_parity.py), so ops==kernel validates the kernel.
#
# The env switch is read per forward but fixed per process, so the server is
# started once per mode. OOSMLX_DRAFT_MODEL is cleared so temp-0 stays on the
# generic decode loop. A warmup request absorbs model load + kernel compile.
#
# usage: gated_delta_ab.sh [hf-model-id] [gen-tokens]
set -u
cd "$(dirname "$0")/../../.."
MODEL="${1:-mlx-community/Qwen3.5-9B-MLX-4bit}"
GEN="${2:-80}"
PORT="${OOSMLX_AB_PORT:-8095}"
export PATH="$HOME/.cargo/bin:/opt/homebrew/bin:/usr/bin:/bin:/usr/sbin:/sbin"
unset OOSMLX_DRAFT_MODEL NATS_URL
USER_MSG="Erklaere in drei Saetzen, warum der Himmel blau ist."

# Free any resident ollama model first (note 170: avoid memory pressure).
resident=$(curl -s --max-time 3 127.0.0.1:11434/api/ps 2>/dev/null \
  | python3 -c 'import json,sys; print(" ".join(m["name"] for m in json.load(sys.stdin).get("models",[])))' 2>/dev/null)
for m in $resident; do
  curl -s 127.0.0.1:11434/api/generate -d "{\"model\":\"$m\",\"keep_alive\":0}" >/dev/null
done
[ -n "$resident" ] && { echo "unloaded ollama: $resident"; sleep 2; }

BIN=target/release/oosmlx
echo "building $BIN ..."
cargo build -p oosmlx --release --features mlx >/dev/null || { echo "build failed"; exit 1; }

# $1 = label, $2 = OOSMLX_GATED_DELTA value (empty => ops default)
gen() {
  local label="$1" envval="$2"
  local log="/tmp/oosmlx_gd_ab_${label}.log"
  : >"$log"
  if [ -n "$envval" ]; then export OOSMLX_GATED_DELTA="$envval"; else unset OOSMLX_GATED_DELTA; fi
  RUST_LOG=oosmlx=info "$BIN" "127.0.0.1:$PORT" >"$log" 2>&1 &
  local srv=$!
  local up=""
  for i in $(seq 1 90); do
    curl -s --max-time 2 "127.0.0.1:$PORT/v1/models" >/dev/null 2>&1 && { up=1; break; }
    sleep 1
  done
  if [ -z "$up" ]; then kill $srv 2>/dev/null; echo "SERVER_NOT_UP"; return 1; fi
  python3 - "$MODEL" "$PORT" "$USER_MSG" "$GEN" <<'PYEOF'
import json, sys, urllib.request
model, port, msg, gen = sys.argv[1], sys.argv[2], sys.argv[3], int(sys.argv[4])
def chat(c, n):
    req = json.dumps({"model": model, "messages": [{"role": "user", "content": c}],
                      "temperature": 0, "max_tokens": n}).encode()
    r = urllib.request.Request(f"http://127.0.0.1:{port}/v1/chat/completions",
                               data=req, headers={"Content-Type": "application/json"})
    return json.load(urllib.request.urlopen(r, timeout=1800))
chat("Antworte mit einem Wort: Hauptstadt von Frankreich?", 8)  # warmup
resp = chat(msg, gen)
if "error" in resp:
    print("SERVER_ERROR:", resp["error"]["message"]); sys.exit(1)
print(resp["choices"][0]["message"]["content"])
PYEOF
  local rc=$?
  kill $srv 2>/dev/null; wait $srv 2>/dev/null
  return $rc
}

echo "=== ops ==="
ops_out=$(gen ops "") || { echo "ops run failed"; exit 1; }
printf '%s\n' "$ops_out"
echo "=== kernel ==="
kern_out=$(gen kernel "kernel") || { echo "kernel run failed"; exit 1; }
printf '%s\n' "$kern_out"

echo "=== compare ==="
if [ "$ops_out" = "$kern_out" ]; then
  echo "BYTE-IDENTICAL: PASS"
else
  echo "DIFFER (check whether it is a single boundary-token flip = same class)"
  n=${#ops_out}; [ ${#kern_out} -lt $n ] && n=${#kern_out}
  i=0; while [ $i -lt $n ] && [ "${ops_out:$i:1}" = "${kern_out:$i:1}" ]; do i=$((i+1)); done
  echo "first diff at char $i"
  echo "ops   [$i:]=${ops_out:$i:60}"
  echo "kernel[$i:]=${kern_out:$i:60}"
fi
