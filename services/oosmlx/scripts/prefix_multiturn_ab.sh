#!/bin/bash
# Prompt-prefix cache A/B for the genuine multi-turn extension case.
#
# Unlike prefix_ab.sh (same prompt twice), this models a real follow-up turn:
# turn 2 keeps turn 1's long user message verbatim at its head and appends an
# assistant reply plus a new user question. The cache, frozen at turn 1's
# reusable boundary, must be reused when turn 2 is sent -- byte-identical to
# computing turn 2 cold, and skipping the long shared head.
#
# One server, three measured requests, no env toggle:
#   1. turn2 COLD   (no matching snapshot yet)      -> resp_cold, cached_prefix 0
#   2. turn1        (freezes the snapshot at its boundary B1)
#   3. turn2 WARM   (extends turn1's boundary)       -> resp_warm, cached_prefix B1
# PASS iff resp_cold == resp_warm. This also covers full-attention families
# (ministral3): their reusable boundary is the whole prompt (the at_end path),
# where same-prompt-twice cannot trigger reuse but a real extension does.
#
# usage: prefix_multiturn_ab.sh <hf-model-id> [gen-tokens] [prompt-mult]
set -u
cd "$(dirname "$0")/../../.."

MODEL="${1:?usage: prefix_multiturn_ab.sh <hf-model-id> [gen-tokens] [prompt-mult]}"
GEN="${2:-96}"
MULT="${3:-100}"
PORT="${OOSMLX_AB_PORT:-8095}"
export PATH="$HOME/.cargo/bin:/opt/homebrew/bin:/usr/bin:/bin:/usr/sbin:/sbin"
unset OOSMLX_DRAFT_MODEL NATS_URL

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
# Always rebuild: an existing binary from an earlier session tests stale code.
echo "building $BIN ..."
cargo build -p oosmlx --release --features mlx >/dev/null || exit 1
LOG=/tmp/oosmlx_prefix_mt_server.log
: >"$LOG"
RUST_LOG=oosmlx=info "$BIN" 127.0.0.1:$PORT >"$LOG" 2>&1 &
SRV=$!
trap 'kill $SRV 2>/dev/null' EXIT
for i in $(seq 1 60); do
  curl -s --max-time 2 127.0.0.1:$PORT/v1/models >/dev/null 2>&1 && break
  sleep 1
done

python3 - "$MODEL" "$PORT" "$GEN" "$MULT" <<'PYEOF'
import json, sys, time, urllib.request
model, port, gen, mult = sys.argv[1], sys.argv[2], int(sys.argv[3]), int(sys.argv[4])
long_user = ("Die Lagerhalle in Dortmund verzeichnete im dritten Quartal einen "
             "deutlichen Anstieg der Durchlaufzeiten, weil die neue Sortieranlage "
             "erst teilweise kalibriert war. ") * mult
turn1 = [{"role": "user", "content": long_user}]
turn2 = [{"role": "user", "content": long_user},
         {"role": "assistant",
          "content": "Die Sortieranlage war im dritten Quartal nur teilweise kalibriert."},
         {"role": "user", "content": "Nenne in einem Wort das betroffene Quartal."}]
def chat(messages, max_tokens):
    req = json.dumps({"model": model, "messages": messages,
                      "temperature": 0, "max_tokens": max_tokens}).encode()
    r = urllib.request.Request(f"http://127.0.0.1:{port}/v1/chat/completions",
                               data=req, headers={"Content-Type": "application/json"})
    t = time.time()
    resp = json.load(urllib.request.urlopen(r, timeout=1800))
    if "error" in resp:
        print("server error:", resp["error"]["message"]); sys.exit(1)
    return resp["choices"][0]["message"]["content"], time.time() - t

chat([{"role": "user", "content": "Hauptstadt von Frankreich?"}], 8)  # warmup
cold, t_cold = chat(turn2, gen)
chat(turn1, 8)               # freeze the snapshot at turn 1's boundary
warm, t_warm = chat(turn2, gen)

print(f"turn2 cold  wall={t_cold:6.2f}s  chars={len(cold)}")
print(f"turn2 warm  wall={t_warm:6.2f}s  chars={len(warm)}")
if cold == warm:
    print("BYTE-IDENTICAL: PASS")
else:
    print("BYTE-IDENTICAL: FAIL")
    n = min(len(cold), len(warm))
    i = next((k for k in range(n) if cold[k] != warm[k]), n)
    print(f"  first diff at char {i}")
    print(f"  cold[{i}:{i+60}]={cold[i:i+60]!r}")
    print(f"  warm[{i}:{i+60}]={warm[i:i+60]!r}")
    sys.exit(2)
PYEOF
rc=$?
kill $SRV 2>/dev/null; wait $SRV 2>/dev/null; trap - EXIT

# Phase lines: turn2-cold, turn1, turn2-warm. cached_prefix should be
# 0 on the cold turn2 and the turn1 boundary on the warm turn2.
echo "--- server phase lines (turn2-cold, turn1, turn2-warm) ---"
grep -o 'generation phases.*' "$LOG" | sed 's/\x1b\[[0-9;]*m//g' | tail -3
exit $rc
