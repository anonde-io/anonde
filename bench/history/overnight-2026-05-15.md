<!--
  Snapshot of bench/REPORT_MATRIX.md captured during the autonomous
  overnight bench-upgrade session.

  Label:    overnight-2026-05-15
  Captured: 2026-05-15
  Run:      local (M-series Mac, ORT_LIB .tokenlib/libonnxruntime.dylib)
  New corpora added in this batch:
    conll2003_en, conll2003_de (skipped, gated), germeval_14,
    wnut_17, pharmaconer_es, adversarial_de
  Engines run: anonde-patterns, anonde-gliner. gliner-py + presidio
    sidecars did not run on the local box (Python sidecar deps not
    installed); cells show as missing in coverage but CI runs them.
-->
# 🛡️ anonde bench matrix

> **TL;DR** — `anonde-gliner` (production) is the lowest-leak engine on **9 of 10** gold-annotated corpora. Biggest absolute improvement over the best baseline: **+69.8pp** in leak rate. Strict F1 trades exact-byte alignment for catching more PHI — the right trade-off for a redactor, not a benchmark gaming exercise.

## Per-corpus verdict

`🟢/🟡/🟠/🔴/💀` flags the production engine's leak severity on each corpus. `⚪` corpora produced text but no span-level gold, so F1/leak are not measurable.

- 🟠 **`openmed`** — `anonde-gliner` leaks **17.2%** vs the best baseline `anonde-patterns` at **22.8%** (↓ **5.7pp** better).
- ⚪ **`pmc_de`** — no gold annotations; precision-probe only (see `bench/corpora/pmc_de/README.md`).
- 🟡 **`synth_clinical`** — `anonde-gliner` leaks **11.1%** vs the best baseline `anonde-patterns` at **11.8%** (↓ **0.6pp** better).
- ⚪ **`wiki_de`** — no gold annotations; precision-probe only (see `bench/corpora/wiki_de/README.md`).
- 🟡 **`finance_de`** — `anonde-gliner` leaks **9.8%** vs the best baseline `anonde-patterns` at **18.1%** (↓ **8.3pp** better).
- 🟡 **`legal_de`** — `anonde-gliner` leaks **6.9%** vs the best baseline `anonde-patterns` at **9.4%** (↓ **2.5pp** better).
- 🟠 **`wikiann_de`** — `anonde-gliner` leaks **15.3%** vs the best baseline `gliner-py` at **12.8%** (↑ **2.5pp** worse).
- 🟠 **`germeval_14`** — `anonde-gliner` leaks **28.3%** vs the best baseline `anonde-patterns` at **51.6%** (↓ **23.3pp** better).
- ⚪ **`conll2003_de`** — no gold annotations; precision-probe only (see `bench/corpora/conll2003_de/README.md`).
- 🟠 **`adversarial_de`** — `anonde-gliner` leaks **28.2%** vs the best baseline `anonde-patterns` at **33.2%** (↓ **5.0pp** better).
- 🟠 **`ai4privacy_en`** — `anonde-gliner` leaks **25.0%** vs the best baseline `gliner-py` at **25.8%** (↓ **0.9pp** better).
- 🟠 **`conll2003_en`** — `anonde-gliner` leaks **27.1%** vs the best baseline `anonde-patterns` at **96.9%** (↓ **69.8pp** better).
- 🔴 **`wnut_17`** — `anonde-gliner` leaks **56.4%** vs the best baseline `anonde-patterns` at **96.8%** (↓ **40.4pp** better).
- ⚪ **`pharmaconer_es`** — no gold annotations; precision-probe only (see `bench/corpora/pharmaconer_es/README.md`).

## Leak rate · lower is better

A gold PHI span is *leaked* when **no** predicted span overlaps it. This is the metric that matters for redaction: 'did we miss a name?'

| Corpus | `anonde-patterns` | `anonde-gliner` | `presidio` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|
| `openmed` | 22.8% | **17.2%** 🥇 | – | 50.0% | 98.4% |
| `pmc_de` | – | – | – | – | – |
| `synth_clinical` | 11.8% | **11.1%** 🥇 | – | 25.4% | – |
| `wiki_de` | – | – | – | – | – |
| `finance_de` | 18.1% | **9.8%** 🥇 | – | 26.2% | – |
| `legal_de` | 9.4% | **6.9%** 🥇 | – | 25.4% | – |
| `wikiann_de` | 38.7% | 15.3% | – | **12.8%** 🥇 | – |
| `germeval_14` | 51.6% | **28.3%** 🥇 | – | – | – |
| `conll2003_de` | – | – | – | – | – |
| `adversarial_de` | 33.2% | **28.2%** 🥇 | – | – | – |
| `ai4privacy_en` | 55.4% | **25.0%** 🥇 | 28.4% | 25.8% | – |
| `conll2003_en` | 96.9% | **27.1%** 🥇 | – | – | – |
| `wnut_17` | 96.8% | **56.4%** 🥇 | – | – | – |
| `pharmaconer_es` | – | – | – | – | – |

## Latency · per-document p50 / p95

Wall-clock per `engine.Analyze(doc)` call. p50 = steady-state, p95 = tail. Mean + p99 in `results_matrix.csv`. For redaction services, p95 is the SLO knob — the latency a customer waiting on `/v1/ingest` actually feels.

| Corpus | `anonde-patterns` p50 / p95 | `anonde-gliner` p50 / p95 | `presidio` p50 / p95 | `gliner-py` p50 / p95 | `openai-pf` p50 / p95 |
|---|---:|---:|---:|---:|---:|
| `openmed` | 1 ms / 3 ms | 927 ms / 3.5 s | – | 593 ms / 3.7 s | 3.2 s / 8.4 s |
| `pmc_de` | 7 ms / 76 ms | 5.1 s / 50.9 s | – | 7.0 s / 14.0 s | – |
| `synth_clinical` | 0 ms / 1 ms | 141 ms / 796 ms | – | 183 ms / 235 ms | – |
| `wiki_de` | 2 ms / 12 ms | 958 ms / 17.8 s | – | 1.4 s / 7.8 s | – |
| `finance_de` | 0 ms / 1 ms | 109 ms / 190 ms | – | 187 ms / 303 ms | – |
| `legal_de` | 0 ms / 1 ms | 116 ms / 177 ms | – | 193 ms / 345 ms | – |
| `wikiann_de` | 0 ms / 0 ms | 41 ms / 87 ms | – | 69 ms / 105 ms | – |
| `germeval_14` | 0 ms / 0 ms | 41 ms / 68 ms | – | – | – |
| `conll2003_de` | – | – | – | – | – |
| `adversarial_de` | 0 ms / 1 ms | 119 ms / 230 ms | – | – | – |
| `ai4privacy_en` | 0 ms / 0 ms | 59 ms / 307 ms | 6 ms / 9 ms | 77 ms / 179 ms | – |
| `conll2003_en` | 0 ms / 0 ms | 35 ms / 52 ms | – | – | – |
| `wnut_17` | 0 ms / 0 ms | 42 ms / 72 ms | – | – | – |
| `pharmaconer_es` | 0 ms / 1 ms | 284 ms / 603 ms | – | – | – |

## Strict F1 · CoNLL exact span + type

Predicted span counts only if `(start, end, type)` matches gold exactly after label normalisation. The number every NER paper publishes; useful for direct comparison to academic baselines, less useful as a production metric (strict scoring penalises broader-or-narrower spans that still redact the PHI).

| Corpus | `anonde-patterns` | `anonde-gliner` | `presidio` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|
| `openmed` | **0.529** 🥇 | 0.463 | – | 0.413 | 0.000 |
| `pmc_de` | 0.000 | 0.000 | – | 0.000 | – |
| `synth_clinical` | 0.512 | 0.536 | – | **0.582** 🥇 | – |
| `wiki_de` | 0.000 | 0.000 | – | 0.000 | – |
| `finance_de` | **0.539** 🥇 | 0.512 | – | 0.510 | – |
| `legal_de` | **0.546** 🥇 | 0.509 | – | 0.439 | – |
| `wikiann_de` | **0.335** 🥇 | 0.249 | – | 0.198 | – |
| `germeval_14` | 0.172 | **0.255** 🥇 | – | – | – |
| `conll2003_de` | – | – | – | – | – |
| `adversarial_de` | 0.342 | **0.369** 🥇 | – | – | – |
| `ai4privacy_en` | 0.085 | 0.273 | 0.213 | **0.340** 🥇 | – |
| `conll2003_en` | 0.004 | **0.384** 🥇 | – | – | – |
| `wnut_17` | 0.006 | **0.247** 🥇 | – | – | – |
| `pharmaconer_es` | 0.000 | 0.000 | – | – | – |

## F1 reference · type-agnostic

Any predicted span overlapping a gold span counts as a hit, regardless of which entity-type label was assigned. The closest metric to 'did we cover the PHI?' that's also boundary-aware. Partial-overlap F1 is in `results_matrix.csv`.

| Corpus | `anonde-patterns` | `anonde-gliner` | `presidio` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|
| `openmed` | 0.654 | 0.626 | – | 0.625 | 0.050 |
| `pmc_de` | 0.000 | 0.000 | – | 0.000 | – |
| `synth_clinical` | 0.888 | 0.890 | – | 0.860 | – |
| `wiki_de` | 0.000 | 0.000 | – | 0.000 | – |
| `finance_de` | 0.812 | 0.855 | – | 0.834 | – |
| `legal_de` | 0.912 | 0.918 | – | 0.844 | – |
| `wikiann_de` | 0.743 | 0.862 | – | 0.842 | – |
| `germeval_14` | 0.540 | 0.617 | – | – | – |
| `conll2003_de` | – | – | – | – | – |
| `adversarial_de` | 0.771 | 0.801 | – | – | – |
| `ai4privacy_en` | 0.528 | 0.661 | 0.662 | 0.635 | – |
| `conll2003_en` | 0.061 | 0.732 | – | – | – |
| `wnut_17` | 0.036 | 0.372 | – | – | – |
| `pharmaconer_es` | 0.000 | 0.000 | – | – | – |

## Cost reference · USD per million characters

All engines in this matrix run on your hardware — no per-call charge. For procurement context, here is what the closest managed-service alternatives cost on their public pricing pages (verified 2026-05-15; vendor pricing drifts, re-check before quoting):

| Engine | Hosting | $/M chars | Notes |
|---|---|---:|---|
| `anonde-patterns` | self-host (~$5/mo Fly machine) | ~**$0.0005** | Patterns-only; fits on `shared-cpu-1x:256MB`. Amortised cost dominated by infra base. |
| `anonde-gliner` | self-host (~$5-15/mo Fly machine) | ~**$0.001** | GLiNER PII baked into image. `shared-cpu-1x:2048MB` suffices; CPU-only. |
| `presidio` | self-host (open-source) | **$0** marginal | Microsoft Presidio. spaCy backend, English-focused. |
| `gliner-py` | self-host (open-source) | **$0** marginal | Same GLiNER PII model via Python sidecar. |
| Google Cloud DLP (inspect) | managed | ~$1 / GB ≈ **$1.00** | 1st GB/mo free; cheapest managed option by far. [pricing](https://cloud.google.com/sensitive-data-protection/pricing) |
| Azure AI Language PII | managed | ~$1 / 1k records ≈ **~$1.00** | Record = 1 000 chars. 5 000 records/mo free. [pricing](https://azure.microsoft.com/en-us/pricing/details/language/) |
| AWS Comprehend Medical (DetectPHI) | managed | $0.01 / 100 chars = **$100** | Tier 1; drops at volume. PHI-grade NER, English only. [pricing](https://aws.amazon.com/comprehend/medical/pricing/) |

> Self-hosting anonde is **roughly 1 000–100 000× cheaper per million characters** than the managed alternatives — and the data never leaves your network. The leak-rate and F1 numbers in the tables above are how you tell if the quality tradeoff is acceptable.

## Cell coverage

Which engines actually produced output for which corpora. Empty cells mean the engine wasn't run (e.g. Presidio is English-only; openai-pf is excluded from corpora that take >1h at 80sec/doc).

| Corpus | `anonde-patterns` | `anonde-gliner` | `presidio` | `gliner-py` | `openai-pf` |
|---|:-:|:-:|:-:|:-:|:-:|
| `openmed` | ✓ | ✓ | – | ✓ | ✓ |
| `pmc_de` | ✓ | ✓ | – | ✓ | – |
| `synth_clinical` | ✓ | ✓ | – | ✓ | – |
| `wiki_de` | ✓ | ✓ | – | ✓ | – |
| `finance_de` | ✓ | ✓ | – | ✓ | – |
| `legal_de` | ✓ | ✓ | – | ✓ | – |
| `wikiann_de` | ✓ | ✓ | – | ✓ | – |
| `germeval_14` | ✓ | ✓ | – | – | – |
| `conll2003_de` | – | – | – | – | – |
| `adversarial_de` | ✓ | ✓ | – | – | – |
| `ai4privacy_en` | ✓ | ✓ | ✓ | ✓ | – |
| `conll2003_en` | ✓ | ✓ | – | – | – |
| `wnut_17` | ✓ | ✓ | – | – | – |
| `pharmaconer_es` | ✓ | ✓ | – | – | – |

## What does this mean?

- **Leak rate** = the fraction of gold PHI spans no predicted span overlaps. The single most
  important number for a PII redactor: each leaked span is a real piece of PHI we'd have
  missed in production.
- **Type-agnostic F1** = harmonic mean of precision and recall using overlap matching; ignores
  the entity-type label. Useful as a tie-breaker when leak rates are close.
- **Strict F1** = exact start, end, and type match against gold. The CoNLL-style metric every
  NER paper publishes; useful for direct academic comparison. Less useful as a redaction
  metric, since a span that's 11 chars vs gold's 5 still successfully tokenises (the
  cleartext is gone either way) — but every leaked span is one we'd have shipped in prod.
- **`–` cells** = engine not run on that corpus. Reasons: language mismatch (Presidio is EN
  only), per-doc cost too high (openai-pf at 80sec/doc on CPU), or corpus requires manual
  DUA registration (`ggponc_de`).
- **⚪ corpora** = precision-probe only (no span-level gold annotations). Useful for "does the
  engine over-redact ordinary prose?" checks, not for F1 / leak rate.

---
*Generated by `bench/scoring/render_matrix.py` over 36 cells. Full per-entity-type breakdown in `results_matrix.csv`.*
