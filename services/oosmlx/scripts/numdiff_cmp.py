"""Compare two numdiff checkpoint dumps; print per-checkpoint diff stats.

Usage: python numdiff_cmp.py <ours.safetensors> <ref.safetensors>

Reads the dumps written by the Rust test `numdiff::dump_hidden_states` and by
numdiff_ref.py and prints max/mean absolute and max relative error per
checkpoint plus the final-position argmax of both logit rows. Healthy parity
at float32 sits around 1e-5 absolute with matching argmax; a divergence that
starts at one layer and grows points at that layer's math.
"""

import sys

import mlx.core as mx

ours = mx.load(sys.argv[1])
ref = mx.load(sys.argv[2])
for k in sorted(set(ours) & set(ref)):
    a, b = ours[k], ref[k]
    if a.shape != b.shape:
        print(f"{k:12s} SHAPE MISMATCH {a.shape} vs {b.shape}")
        continue
    d = mx.abs(a - b)
    rel = d / mx.maximum(mx.abs(b), 1e-6)
    print(
        f"{k:12s} max_abs={d.max().item():.3e} mean_abs={d.mean().item():.3e}"
        f" max_rel={rel.max().item():.3e}"
    )
la, lb = ours["logits"][-1], ref["logits"][-1]
print("argmax ours:", mx.argmax(la).item(), " ref:", mx.argmax(lb).item())
