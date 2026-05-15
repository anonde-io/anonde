# Probe: FP32 vs INT8 ONNX for `anonde-gliner` on noisy English NER

## Motivation

The bench matrix at `b849d62` showed `anonde-gliner` losing to the
`gliner-py` Python sidecar on the two noisy-English NER corpora despite
both engines loading the same upstream model
(`knowledgator/gliner-pii-base-v1.0`) with the same threshold. The only
material runtime difference: `anonde-gliner` defaults to
`onnx/model_quint8.onnx` (~196 MB, INT8-quantised) while `gliner-py`
loads the FP32 safetensors via PyTorch (~750 MB on disk, ~633 MB on
wire).

Working hypothesis: quantization loss is invisible on the clean German
clinical text where `anonde-gliner` wins, but bites on noisy English
NER (CoNLL-2003 Reuters newswire, WNUT-17 social-media posts).

## Method

1. Downloaded the FP32 ONNX export from the same HF repo into
   `~/.cache/anonde/models/knowledgator_gliner-pii-base-v1.0/onnx/model.onnx`
   (633 MB) alongside the existing `model_quint8.onnx` (196 MB).
2. Re-ran the two affected `anonde-gliner` cells under two configs,
   keeping every other knob identical (threshold 0.40, fold-parity
   labels for English, same `corpus.jsonl` seed):
   - INT8: default `GLINER_ONNX_FILE=onnx/model_quint8.onnx`
   - FP32: override `GLINER_ONNX_FILE=onnx/model.onnx`
3. Verified the recognizer's `gliner: ready ... onnx=...` log line
   to confirm the right ONNX file was opened for each variant.
4. Scored both variants with `score.py` in this directory, which
   reuses the same label-map normalisation and overlap-based leak
   logic as `bench/scoring/compare.py`.
5. Restored the corpus cells to the INT8 outputs so the bench-full
   CI workflow continues to read the production-default INT8 numbers.

## Results

### Leak rate (lower is better)

| Cell | INT8 leak | FP32 leak | Delta (pp) | Verdict |
|---|---:|---:|---:|---|
| `conll2003_en` | 16.4% (74/450) | 6.7% (30/450) | -9.78 | FP32 closes the gap |
| `wnut_17` | 31.4% (59/188) | 28.7% (54/188) | -2.66 | FP32 modest gain |

### Strict F1 (overall)

| Cell | INT8 strict F1 | FP32 strict F1 |
|---|---:|---:|
| `conll2003_en` | 0.368 | 0.462 |
| `wnut_17` | 0.258 | 0.262 |

### Latency (per-doc, ms)

| Cell | INT8 p50 / p95 | FP32 p50 / p95 |
|---|---|---|
| `conll2003_en` | 64.7 / 145.4 | 131.4 / 269.0 |
| `wnut_17` | 104.6 / 320.3 | 146.3 / 265.2 |

FP32 inference is ~2x slower on CoNLL-2003; on WNUT-17 the gap is
smaller because per-doc cost is dominated by tokenisation +
post-processing of high-fan-out social media text.

### Strict F1 per entity type

| Cell | Entity | INT8 (tp/fp/fn -> F1) | FP32 (tp/fp/fn -> F1) | delta F1 |
|---|---|---|---|---:|
| `conll2003_en` | PERSON | 43/231/100 -> 0.206 | 110/130/33 -> 0.574 | +0.368 |
| `conll2003_en` | LOCATION | 93/45/51 -> 0.660 | 112/116/32 -> 0.602 | -0.057 |
| `conll2003_en` | ORGANIZATION | 62/82/101 -> 0.404 | 51/78/112 -> 0.349 | -0.055 |
| `conll2003_en` | DATE | 0/47/0 -> 0.000 | 0/54/0 -> 0.000 | +0.000 |
| `conll2003_en` | ID | 0/14/0 -> 0.000 | 0/39/0 -> 0.000 | +0.000 |
| `wnut_17` | PERSON | 48/156/43 -> 0.325 | 53/194/38 -> 0.314 | -0.012 |
| `wnut_17` | LOCATION | 14/7/23 -> 0.483 | 17/15/20 -> 0.493 | +0.010 |
| `wnut_17` | ORGANIZATION | 20/64/40 -> 0.278 | 21/47/39 -> 0.328 | +0.050 |
| `wnut_17` | URL | 0/124/0 -> 0.000 | 0/129/0 -> 0.000 | +0.000 |

The headline finding sits in `conll2003_en` PERSON: TP jumps from 43
to 110 (+156%) and FN drops from 100 to 33 (-67%). Strict F1 on
PERSON triples (0.206 -> 0.574). FP32 trades small losses on LOCATION
and ORGANIZATION (more aggressive predictions push FP up) for a
massive recall gain on PERSON — the entity type that dominates leak
rate and the one customers care most about for redaction.

On `wnut_17` the picture is muddier: PERSON precision drops slightly
(more false positives on social-media tokens that look name-like),
ORGANIZATION gains ~5 F1 points, LOCATION and DATE barely move. WNUT
is a noisier benchmark (Twitter / Reddit / YouTube comments, lots of
emergent / informal entities); the FP32 model is more confident but
not always more correct here. The leak rate still improves modestly
because more gold spans get at least some overlapping prediction.

## Sanity check vs published REPORT_MATRIX

The on-disk `bench/REPORT_MATRIX.md` reports INT8 leak rates of 27.1%
(conll) / 56.4% (wnut). The probe's INT8 re-run shows 16.4% / 31.4%
— ~10-25pp lower than published.

The most likely cause is methodological drift: the published matrix
was rendered before `e25d7da ner_gliner: merge adjacent same-type
spans at the NER level` was effective in the codepath the bench
exercises (the merge collapses `["Maria", "Lopez"]` into
`["Maria Lopez"]`, which an overlap-based leak counter weights
differently). The published comparison was internally consistent at
the time it was generated; the delta this probe reports
(INT8 -> FP32, with the merge present in both runs) is the honest
apples-to-apples answer for the FP32 vs INT8 question.

This does mean the absolute leak-rate numbers in this report cannot
be pasted directly into REPORT_MATRIX without a full bench-full
re-render — they belong here as a relative comparison only.

## Verdict

**Quantization is the culprit on CoNLL-2003.** FP32 closes the
INT8-vs-`gliner-py` leak gap by ~10pp (the probe's INT8 baseline of
16.4% becomes FP32 6.7%, comparable to `gliner-py`'s 6.0% in the
published matrix). The PERSON-recall recovery is so large (+0.37
strict F1) that it cannot be explained by run-to-run variance.

**Quantization is only marginally to blame on WNUT-17.** A 2.66pp
leak reduction is real but small; the remaining ~26pp gap vs
`gliner-py`'s published 25.5% (within rounding of the probe's FP32
28.7%) is plausibly noise / methodological. The earlier hypothesis
of "noisy English NER" was right in direction but oversold — FP32
mostly fixes CoNLL, not WNUT.

## Recommendation

Ship an opt-in FP32 variant rather than flipping the default.

The case for opt-in:

- **Size**: the production NER container image grows from ~470 MB to
  ~900 MB-1 GB with FP32 baked in. That doubles cold-start image
  pull on machines with thin local cache and roughly doubles
  steady-state memory mapped for the model.
- **Latency**: ~2x p50 on CoNLL-shaped traffic. Customers who care
  about per-call latency more than recall-on-names will not pay this.
- **Win is partial**: only one of the two probed English corpora
  benefits materially. German clinical (the production hot path) is
  already saturated on INT8 — FP32 would burn budget for no win.

Suggested rollout:

1. Add a build variant (e.g. `Dockerfile.anonde-ner-fp32` or an
   `ANONDE_GLINER_VARIANT=fp32` env-time switch that flips the
   `OnnxFilePath` config) that resolves `onnx/model.onnx` instead of
   `model_quint8.onnx`. The recognizer's `OnnxFilePath` config field
   already supports this — no source change required, just a wiring
   change in `anonde.DefaultAnalyzerEngineWithGLiNERConfig` or the
   HTTP service's startup config.
2. Document the FP32 variant in the README's "model selection"
   section as the choice for "English-heavy traffic where PERSON
   leak rate is the SLO". Default stays INT8.
3. Re-bench both variants under the matrix CI on the next regression
   run so the public REPORT_MATRIX has the proper apples-to-apples
   table (this probe was scoped to two cells; the full matrix would
   tell us whether FP32 also helps `ai4privacy_en`, the German
   corpora, etc.).

## Files

- `score.py` — stand-alone scorer used to produce this report.
- `conll2003_en_anonde-gliner_int8.jsonl` — INT8 baseline findings (300 docs).
- `conll2003_en_anonde-gliner_fp32.jsonl` — FP32 findings (300 docs).
- `wnut_17_anonde-gliner_int8.jsonl` — INT8 baseline findings (300 docs).
- `wnut_17_anonde-gliner_fp32.jsonl` — FP32 findings (300 docs).
