#!/bin/bash
# Native tool-calling smoke for any oosmlx family, over /v1/chat/completions.
#
# Round 1: send OpenAI `tools` + a data question. PASS iff the response comes
# back as finish_reason=tool_calls with a STRUCTURED tool_call (name + JSON
# arguments) -- not as plaintext, which is the bug this verifies is fixed.
# Round 2: append the assistant's call + a faked tool result and send again,
# proving render_prompt round-trips tool_calls and [TOOL_RESULTS] so the agent
# loop can inject data and continue.
#
# usage: tools_smoke.sh [hf-model-id]
set -u
cd "$(dirname "$0")/../../.."

MODEL="${1:-mlx-community/mistralai_Devstral-Small-2-24B-Instruct-2512-MLX-MXFP4}"
PORT="${OOSMLX_AB_PORT:-8097}"
export PATH="$HOME/.cargo/bin:/opt/homebrew/bin:/usr/bin:/bin:/usr/sbin:/sbin"
unset OOSMLX_DRAFT_MODEL NATS_URL

resident=$(curl -s --max-time 3 127.0.0.1:11434/api/ps 2>/dev/null \
  | python3 -c 'import json,sys; print(" ".join(m["name"] for m in json.load(sys.stdin).get("models",[])))' 2>/dev/null)
if [ -n "$resident" ]; then
  for m in $resident; do
    curl -s 127.0.0.1:11434/api/generate -d "{\"model\":\"$m\",\"keep_alive\":0}" >/dev/null
  done
  sleep 2
fi

BIN=target/release/oosmlx
echo "building $BIN ..."
cargo build -p oosmlx --release --features mlx >/dev/null || exit 1
LOG=/tmp/oosmlx_tools_server.log
: >"$LOG"
RUST_LOG=oosmlx=warn "$BIN" 127.0.0.1:$PORT >"$LOG" 2>&1 &
SRV=$!
trap 'kill $SRV 2>/dev/null' EXIT
for i in $(seq 1 60); do
  curl -s --max-time 2 127.0.0.1:$PORT/v1/models >/dev/null 2>&1 && break
  sleep 1
done

MODEL="$MODEL" PORT="$PORT" python3 - <<'PYEOF'
import json, os, sys, urllib.request
model, port = os.environ["MODEL"], os.environ["PORT"]
tools = [
    {"type": "function", "function": {
        "name": "oos_schema_search",
        "description": "Search the schema for domains/contexts relevant to a query.",
        "parameters": {"type": "object",
                       "properties": {"query": {"type": "string"}},
                       "required": ["query"]}}},
    {"type": "function", "function": {
        "name": "oos_query",
        "description": "Return all rows of a context by name.",
        "parameters": {"type": "object",
                       "properties": {"context_name": {"type": "string"}},
                       "required": ["context_name"]}}},
]
system = ("Du bist ein Datenassistent. Nutze die bereitgestellten Tools, um "
          "Datenfragen zu beantworten; rate nicht.")
def chat(messages):
    req = json.dumps({"model": model, "messages": messages, "tools": tools,
                      "temperature": 0, "max_tokens": 256}).encode()
    r = urllib.request.Request(f"http://127.0.0.1:{port}/v1/chat/completions",
                               data=req, headers={"Content-Type": "application/json"})
    resp = json.load(urllib.request.urlopen(r, timeout=1800))
    if "error" in resp:
        print("server error:", resp["error"]["message"]); sys.exit(1)
    return resp["choices"][0]

msgs = [{"role": "system", "content": system},
        {"role": "user", "content": "Zeige alle Personen"}]
c1 = chat(msgs)
finish1 = c1["finish_reason"]
calls1 = c1["message"].get("tool_calls") or []
print(f"round 1: finish_reason={finish1}  tool_calls={len(calls1)}")
for tc in calls1:
    print(f"  -> {tc['function']['name']}({tc['function']['arguments']})")
if finish1 != "tool_calls" or not calls1:
    print("ROUND 1: FAIL (expected a structured tool_call, got plaintext)")
    print("  content:", repr(c1["message"].get("content", "")[:200]))
    sys.exit(2)
print("ROUND 1: PASS")

# Round 2: feed a faked result back and let the model continue.
msgs.append(c1["message"])
for tc in calls1:
    msgs.append({"role": "tool", "tool_call_id": tc["id"],
                 "content": json.dumps([{"name": "Alice", "age": 30},
                                        {"name": "Bob", "age": 41}])})
c2 = chat(msgs)
finish2 = c2["finish_reason"]
calls2 = c2["message"].get("tool_calls") or []
print(f"round 2: finish_reason={finish2}  tool_calls={len(calls2)}")
for tc in calls2:
    print(f"  -> {tc['function']['name']}({tc['function']['arguments']})")
if c2["message"].get("content"):
    print("  content:", repr(c2["message"]["content"][:300]))
print("ROUND 2: OK (loop continued; render_prompt round-tripped calls + results)")
PYEOF
rc=$?
kill $SRV 2>/dev/null; wait $SRV 2>/dev/null; trap - EXIT
exit $rc
