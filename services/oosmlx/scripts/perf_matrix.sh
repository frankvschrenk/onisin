#!/bin/bash
# 3x3 performance matrix (task #48, note 196): runtimes oosmlx / mlx_lm /
# ollama x models gemma-4-26B-A4B-nvfp4 / Devstral-24B-MXFP4 /
# Qwen3.6-35B-A3B-4bit. Thin driver over bench.sh -- one full sequential
# bench.sh run per model so only ever one big model is resident (note 170:
# the 2026-06-11 two-resident-26B swap death), with ollama unloaded between
# legs by bench.sh itself.
#
# Why ollama is N/A for two cells: ollama holds only gemma4:26b-mlx locally,
# and no format-matching ollama build of Devstral-MXFP4 or Qwen3.6-35B-A3B
# exists -- which is the whole reason oosmlx exists (run the newest quants
# ollama cannot). Those cells are reported N/A rather than measured against a
# mismatched quant.
#
# usage: perf_matrix.sh [gen-tokens] [prompt-mult]
#   perf_matrix.sh            # 256 gen, ~2k prompt (default)
#   perf_matrix.sh 256 206    # ~7.2k long-context regime
set -u
SELF="$(cd "$(dirname "$0")" && pwd)"
BENCH="$SELF/bench.sh"
GEN="${1:-256}"
MULT="${2:-55}"
OUT=/tmp/oosmlx_perf_matrix.out
export PATH="$HOME/.cargo/bin:/opt/homebrew/bin:/usr/bin:/bin:/usr/sbin:/sbin"
: >"$OUT"

# Build the engine once up front so every cell measures the same current
# HEAD binary (bench.sh only builds when the binary is absent).
cd "$SELF/../../.."
echo "building target/release/oosmlx ..." | tee -a "$OUT"
cargo build -p oosmlx --release --features mlx >>"$OUT" 2>&1 || { echo "BUILD FAILED"; tail -20 "$OUT"; exit 1; }

run() {  # label  hf-id  ollama-tag(empty=skip)
  echo "########## MODEL $1 ##########" | tee -a "$OUT"
  bash "$BENCH" "$2" "$3" "$GEN" "$MULT" 2>&1 | tee -a "$OUT"
  echo "" | tee -a "$OUT"
}

run gemma    mlx-community/gemma-4-26b-a4b-it-nvfp4                                  gemma4:26b-mlx
run devstral mlx-community/mistralai_Devstral-Small-2-24B-Instruct-2512-MLX-MXFP4    ""
run qwen     mlx-community/Qwen3.6-35B-A3B-4bit                                      ""

echo ""
echo "==================== PERF MATRIX (gen=$GEN mult=$MULT, temp0) ===================="
python3 - "$OUT" "$GEN" "$MULT" <<'PYEOF'
import re, sys
out, gen, mult = open(sys.argv[1]).read(), sys.argv[2], sys.argv[3]

# Walk the captured log: a MODEL marker switches the current model bucket,
# each LEG line carries one (runtime, prefill_tps, decode_tps) measurement.
# The oosmlx-temp0.7 line is decode-only (no prefill phase), so it needs its
# own capture rather than the prefill+decode leg regex.
models, cur = {}, None
leg_re = re.compile(r'LEG (\S+) prefill_tps=(\d+) decode_tps=(\d+)')
temp07_re = re.compile(r'LEG oosmlx-temp0\.7 decode_tps=(\d+)')
for line in out.splitlines():
    m = re.search(r'########## MODEL (\S+)', line)
    if m:
        cur = m.group(1); models.setdefault(cur, {}); continue
    g = leg_re.search(line)
    if g and cur:
        models[cur][g.group(1)] = (int(g.group(2)), int(g.group(3))); continue
    d = temp07_re.search(line)
    if d and cur:
        models[cur]["oosmlx-temp0.7"] = (0, int(d.group(1)))

runtimes = ["oosmlx", "mlx_lm", "ollama"]
order = [m for m in ("gemma", "devstral", "qwen") if m in models]

def cell(model, rt):
    v = models.get(model, {}).get(rt)
    return f"{v[0]:>4}/{v[1]:<3}" if v else "  N/A  "

w = 14
print("prefill_tps / decode_tps\n")
print("model".ljust(w) + "".join(rt.ljust(w) for rt in runtimes))
print("-" * (w * (len(runtimes) + 1)))
for model in order:
    print(model.ljust(w) + "".join(cell(model, rt).ljust(w) for rt in runtimes))

# The oosmlx default request path (on-device sampler) is measured separately.
print("")
for model in order:
    v = models.get(model, {}).get("oosmlx-temp0.7")
    if v:
        print(f"  oosmlx {model} default-path (temp0.7) decode_tps={v[1]}")
PYEOF
