"""Reference-side dump for the oosmlx <-> mlx_lm differential diagnosis.

Counterpart of the Rust dump test `numdiff::dump_hidden_states` in
src/models/gemma4.rs: both sides run one cache-less prefill over the same
token ids and write every per-layer hidden state to a safetensors file, so a
host-side comparison (numdiff_cmp.py) pinpoints where the forwards first
diverge. This replicates Gemma4TextModel.__call__ verbatim because the model
code offers no capture hook, and it writes the token ids it used so the Rust
side consumes identical ids -- tokenizer behavior stays out of the equation.

The float32 mode casts the whole model via set_dtype, removing the bf16/f32
compute-regime difference from the comparison so only real math differences
remain (this is how the MoE router-input bug was isolated, commit fd95169).

usage:
  python numdiff_ref.py <model_dir> <prompt_file> <tokens_out.json> \
      <dump_out.safetensors> <float32|native>
"""

import json
import sys

import mlx.core as mx
from mlx_lm.models.gemma4_text import logit_softcap
from mlx_lm.utils import load

model_dir, prompt_file, tokens_out, dump_out, dtype = sys.argv[1:6]

model, tokenizer = load(model_dir)
if dtype == "float32":
    model.set_dtype(mx.float32)

prompt = open(prompt_file).read()
# The oosmlx engine encodes its rendered prompt with add_special_tokens=False
# (<bos> is a literal in the template); mirror that exactly.
ids = tokenizer.encode(prompt, add_special_tokens=False)
json.dump(ids, open(tokens_out, "w"))
print("tokens:", len(ids), ids)

text = getattr(model, "language_model", model)  # multimodal wrapper on 26B
m = text.model
h = m.embed_tokens(mx.array([ids])) * m.embed_scale
dump = {"embed": h}
cache = [None] * len(m.layers)
masks = m._make_masks(h, cache)
intermediates = [(None, None)] * len(m.layers)
for idx, (layer, c, mask, prev_idx) in enumerate(
    zip(m.layers, cache, masks, m.previous_kvs)
):
    kvs, offset = intermediates[prev_idx]
    h, kvs, offset = layer(
        h, mask, c, per_layer_input=None, shared_kv=kvs, offset=offset
    )
    intermediates[idx] = (kvs, offset)
    dump[f"layer_{idx:02d}"] = h
hn = m.norm(h)
dump["final_norm"] = hn
dump["logits"] = logit_softcap(
    text.final_logit_softcapping, m.embed_tokens.as_linear(hn)
)

# Drop the batch dim (the Rust side runs unbatched) and compare in f32.
dump = {k: v[0].astype(mx.float32) for k, v in dump.items()}
mx.eval(*dump.values())
mx.save_safetensors(dump_out, dump)
print("argmax last:", mx.argmax(dump["logits"][-1]).item())
print("dumped", len(dump), "checkpoints to", dump_out)
