"""Numeric parity check for the qwen3_5 family against mlx_lm.

Feeds mlx_lm the EXACT oosmlx-rendered ChatML prompt, greedy-decodes, and
prints the token ids + text so the oosmlx output can be compared verbatim.
Same tokenizer + same prompt string + greedy => identical tokens iff the two
forward passes agree. Coherent output alone is not proof (the gemma4 MoE-router
bug produced coherent German while diverging numerically), hence this.

Run in the throwaway venv that has the qwen3_5-capable mlx_lm source:
    /tmp/mlxlm-venv/bin/python services/oosmlx/scripts/qwen35_parity.py
"""
import mlx.core as mx
from mlx_lm import load
from mlx_lm.generate import generate_step
from mlx_lm.sample_utils import make_sampler

MODEL = "mlx-community/Qwen3.5-9B-MLX-4bit"
USER = "Erklaere in drei Saetzen, warum der Himmel blau ist."
# Verbatim oosmlx render_prompt output (non-thinking default).
PROMPT = (
    "<|im_start|>user\n" + USER + "<|im_end|>\n"
    "<|im_start|>assistant\n<think>\n\n</think>\n\n"
)
N = 80

# Force mlx_lm onto the sequential ops recurrence (it defaults to the fused
# Metal kernel at inference). Our Rust port is the ops reduction order, so
# ops-vs-ops must be byte-identical; any divergence here is a real bug, not
# kernel-vs-ops f32 accumulation jitter.
import mlx_lm.models.gated_delta as _gd
_gd.gated_delta_kernel = _gd.gated_delta_ops

model, tokenizer = load(MODEL)
ids = tokenizer.encode(PROMPT, add_special_tokens=False)
print("prompt_tokens", len(ids))
print("first_ids", ids[:12])

out = []
sampler = make_sampler(temp=0.0)
for (tok, _logprobs), _ in zip(
    generate_step(mx.array(ids), model, sampler=sampler), range(N)
):
    t = int(tok)
    if t in tokenizer.eos_token_ids:
        break
    out.append(t)
print("gen_ids", out[:15])
print("n_gen", len(out))
print("TEXT>>>")
print(tokenizer.decode(out))
print("<<<TEXT")
