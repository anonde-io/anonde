# 🛡️ anonde bench matrix

> **TL;DR** — `anonde-gliner-fp32` (production, FP32 ONNX) is the lowest-leak engine on **27 of 29** gold-annotated corpora. Biggest absolute improvement over the best baseline: **+28.2pp** in leak rate. The `anonde-gliner` column is the same model at INT8 (legacy / memory-constrained reference); `anonde-gliner-large` is the 3-4x-larger GLiNER PII variant at FP32 (scaling probe). Neither is counted as a competitor — both are anonde reference columns. Strict F1 trades exact-byte alignment for catching more PHI — the right trade-off for a redactor, not a benchmark gaming exercise.

## 🎯 Scorecard · leak rate roll-ups

The one table. Roll-up rows only (per domain · per language · overall); the per-(domain × language) detail grid lives in the Detailed breakdown below. Each number is **leak rate** (fraction of gold PHI spans missed — lower is better). `anonde-gliner-fp32` is the anonde production engine (FP32 ONNX) and the anchor column; **Verdict** says whether it beats the field. `anonde-gliner` is the same model at INT8 quantization (kept as a legacy / memory-constrained reference); `anonde-gliner-large` is the 3-4x-larger GLiNER PII variant at FP32 (probing whether scale closes the remaining Romance-language cells). Both sit beside the anchor so the quantization and scale tradeoffs are visible at a glance, but the verdict is keyed on production. 🥇 marks the lowest-leak engine in the row. Roll-up rows pool leaked-over-gold across the group (doc-weighted, so larger corpora count more).

| Slice | Scope | `anonde-gliner-fp32` ⬅︎ anonde (FP32, prod) | `anonde-gliner` · anonde (INT8, legacy) | `anonde-gliner-large` · anonde (LARGE) | `anonde-patterns` | `anonde-gliner-stack` | `presidio` | `gliner-py` | `openai-pf` | Verdict |
|---|---|---:|---:|---:|---:|---:|---:|---:|---:|:--:|
| **Σ ALL** | **all** | **12.3%** 🥇 | **31.5%** | **71.6%** | **71.6%** | – | **50.0%** | **33.9%** | – | ✅ |
| | | | | | | | | | | |
| _Σ Clinical / medical de-identification_ | _all langs_ | **19.8%** 🥇 | 27.4% | 62.7% | 62.7% | – | 31.1% | 28.2% | – | ✅ |
| _Σ Legal / administrative_ | _all langs_ | **3.3%** 🥇 | 13.5% | 29.7% | 29.7% | – | 29.6% | 21.5% | – | ✅ |
| _Σ Retail finance_ | _all langs_ | **7.7%** 🥇 | 14.2% | 40.6% | 40.6% | – | 21.3% | 24.0% | – | ✅ |
| _Σ Enterprise logs_ | _all langs_ | **22.4%** 🥇 | 26.3% | 41.3% | 41.3% | – | 24.2% | 73.9% | – | ✅ |
| _Σ General structured PII_ | _all langs_ | **12.0%** 🥇 | 34.2% | 78.3% | 78.3% | – | 56.2% | 34.6% | – | ✅ |
| _Σ Academic NER (newswire / social)_ | _all langs_ | **7.9%** 🥇 | 17.2% | 68.9% | 68.9% | – | 18.3% | 14.0% | – | ✅ |
| _Σ Adversarial / out-of-distribution_ | _all langs_ | **9.2%** 🥇 | 28.2% | 33.2% | 33.2% | – | 37.5% | 40.5% | – | ✅ |
| | | | | | | | | | | |
| _Σ all domains_ | _English_ | **8.7%** 🥇 | 22.0% | 49.0% | 49.0% | – | 23.9% | 34.9% | – | ✅ |
| _Σ all domains_ | _German_ | **8.4%** 🥇 | 23.5% | 54.0% | 54.0% | – | 48.5% | 33.2% | – | ✅ |
| _Σ all domains_ | _Spanish_ | **13.8%** 🥇 | 35.9% | 84.5% | 84.5% | – | 56.6% | 33.0% | – | ✅ |
| _Σ all domains_ | _French_ | **13.8%** 🥇 | 35.1% | 83.0% | 83.0% | – | 50.7% | 33.2% | – | ✅ |
| _Σ all domains_ | _Italian_ | **15.9%** 🥇 | 38.2% | 80.0% | 80.0% | – | 56.2% | 36.3% | – | ✅ |

> **Anonde scoreboard** — across the **24** populated `(domain, language)` cells in the matrix, `anonde-gliner-fp32` is the **lowest-leak engine in 20**, ties in **2**, and is beaten in **2**. ✅ = anonde leads · 🟰 = tied · ❌ = a baseline leaks less. See the per-cell leak-rate grid in the Detailed breakdown below for which baseline wins where. (The TL;DR's win count is per-corpus, a finer split than these per-cell rows.)

<details><summary>Engine profiles · what each column means</summary>

## Engine profiles

Engines below are not all competitors. `anonde-patterns` and `anonde-gliner-fp32` are two deployment tiers of the same anonde binary; compare *across the row* for the trade-off, not against each other for a winner.

| Engine | Profile | Image | CGO | Cold start | Best fit |
|---|---|---|---|---|---|
| `anonde-patterns` | regex / no-ML baseline (anonde tier 1) | ~12 MB | not required | <1 s | structured slot-gen text (forms, logs, finance/legal docs) — wins F1 on PHONE, EMAIL, DATE, PROFESSION when the regex shape is tight |
| `anonde-gliner-fp32` | GLiNER PII (FP32 ONNX, `model.onnx`) + patterns (anonde tier 2, **production**) | ~770 MB | required | 5-30 s warmup | natural text + multilingual PHI; the lowest-leak engine on most gold corpora. Ships the FP32 ONNX. |
| `anonde-gliner` | same GLiNER PII model, INT8 ONNX (`model_quint8.onnx`) — legacy / memory-constrained reference | ~530 MB | required | 5-30 s warmup | not a competitor: kept so the INT8-vs-FP32 quantization regression stays tracked. INT8 depresses GLiNER's sigmoid logits ~0.18, costing recall on multilingual legal/clinical text — this column quantifies it. |
| `anonde-gliner-large` | larger GLiNER PII variant (`knowledgator/gliner-pii-large-v1.0`, FP32) — reference column, not a separate tier | ~1.4 GB | required | 10-60 s warmup | not a competitor: scaling probe (3-4x parameters vs the production base) for the remaining Romance-language cells. |
| `presidio` | Microsoft Presidio (spaCy NER + regex) | ~1 GB | not required | 3-10 s | well-formed English (strong on EN newswire-shaped text where spaCy was trained) |
| `gliner-py` | GLiNER via PyTorch + safetensors (FP32) | ~3 GB | not required | 10-30 s | reference implementation; parity check vs anonde-gliner-fp32's ONNX path |

</details>

## Coverage map · domain × language

Which corpora populate each `(domain, language)` cell. `·` = no corpus wired for that combination yet. The metric sections below are grouped on these same two axes.

| Domain | English | German | Spanish | French | Italian |
|---|---|---|---|---|---|
| **Clinical / medical de-identification** | `synth_clinical_en` | `openmed`, `pmc_de`, `synth_clinical`, `wiki_de` | `pharmaconer_es`, `meddocan_es` | `synth_clinical_fr` | `synth_clinical_it` |
| **Legal / administrative** | `mapa_en` | `legal_de`, `mapa_de` | `mapa_es` | `mapa_fr` | `mapa_it` |
| **Retail finance** | `synth_finance_en` | `finance_de`, `synth_finance_de` | `synth_finance_es` | `synth_finance_fr` | `synth_finance_it` |
| **Enterprise logs** | `synth_logs` | · | · | · | · |
| **General structured PII** | `ai4privacy_en` | `ai4privacy_de` | `ai4privacy_es` | `ai4privacy_fr` | `ai4privacy_it` |
| **Academic NER (newswire / social)** | `conll2003_en`, `wnut_17` | `wikiann_de`, `germeval_14`, `conll2003_de` | · | · | · |
| **Adversarial / out-of-distribution** | · | `adversarial_de` | · | · | · |

# Detailed breakdown

Everything below is reference detail behind the scorecard. The per-cell grid first (the detail demoted off the 13-row scorecard), then each `(domain × language)` section with its raw leak-rate grid (and severity-weighted leak only when it actually diverges >3pp from raw — otherwise the two tracked within noise). One global latency table follows. Strict-F1 and per-entity-type breakdowns live in `results_matrix.csv`. The scorecard above is the answer; these tables are the working.

## Per-cell leak rate · domain × language

Detail behind the scorecard roll-ups: one row per populated `(domain, language)` cell. Same columns, same anchor, same verdict glyph — read this to see *which* baseline wins where. Pooled leak rate across the cell's corpora.

| Domain | Language | `anonde-gliner-fp32` ⬅︎ anonde (FP32, prod) | `anonde-gliner` · anonde (INT8, legacy) | `anonde-gliner-large` · anonde (LARGE) | `anonde-patterns` | `anonde-gliner-stack` | `presidio` | `gliner-py` | `openai-pf` | Verdict |
|---|---|---:|---:|---:|---:|---:|---:|---:|---:|:--:|
| **Clinical / medical de-identification** | English | **14.5%** 🥇 | 15.4% | 27.9% | 27.9% | – | 20.3% | 23.3% | – | ✅ |
| **Clinical / medical de-identification** | German | **5.8%** 🥇 | 11.6% | 15.8% | 15.8% | – | 30.9% | 34.3% | – | ✅ |
| **Clinical / medical de-identification** | Spanish | **24.4%** 🥇 | 36.3% | 92.1% | 92.1% | – | 38.4% | **24.4%** 🥇 | – | 🟰 |
| **Clinical / medical de-identification** | French | **24.9%** 🥇 | 33.2% | 83.3% | 83.3% | – | 28.2% | 25.3% | – | ✅ |
| **Clinical / medical de-identification** | Italian | 30.9% | 37.5% | 82.6% | 82.6% | – | **28.5%** 🥇 | 35.2% | – | ❌ |
| **Legal / administrative** | English | **5.8%** 🥇 | 12.4% | 50.2% | 50.2% | – | 6.2% | 11.6% | – | ✅ |
| **Legal / administrative** | German | **1.1%** 🥇 | 6.5% | 13.7% | 13.7% | – | 26.7% | 23.8% | – | ✅ |
| **Legal / administrative** | Spanish | **18.3%** 🥇 | 45.2% | 100.0% | 100.0% | – | 59.1% | **18.3%** 🥇 | – | 🟰 |
| **Legal / administrative** | French | **7.9%** 🥇 | 43.7% | 100.0% | 100.0% | – | 44.9% | 16.0% | – | ✅ |
| **Legal / administrative** | Italian | 21.2% | 61.0% | 100.0% | 100.0% | – | 67.1% | **6.8%** 🥇 | – | ❌ |
| **Retail finance** | English | **1.8%** 🥇 | 9.0% | 40.2% | 40.2% | – | 10.3% | – | – | ✅ |
| **Retail finance** | German | **1.4%** 🥇 | 6.1% | 17.8% | 17.8% | – | 24.1% | 22.6% | – | ✅ |
| **Retail finance** | Spanish | **15.0%** 🥇 | 23.1% | 60.5% | 60.5% | – | 18.5% | 25.4% | – | ✅ |
| **Retail finance** | French | **14.5%** 🥇 | 22.8% | 60.3% | 60.3% | – | 19.4% | 23.9% | – | ✅ |
| **Retail finance** | Italian | **15.7%** 🥇 | 22.9% | 59.9% | 59.9% | – | 29.6% | 26.4% | – | ✅ |
| **Enterprise logs** | English | **22.4%** 🥇 | 26.3% | 41.3% | 41.3% | – | 24.2% | 73.9% | – | ✅ |
| **General structured PII** | English | **4.0%** 🥇 | 25.0% | 55.4% | 55.4% | – | 28.4% | 25.8% | – | ✅ |
| **General structured PII** | German | **10.1%** 🥇 | 28.0% | 69.7% | 69.7% | – | 57.9% | 34.5% | – | ✅ |
| **General structured PII** | Spanish | **12.2%** 🥇 | 36.6% | 84.7% | 84.7% | – | 61.5% | 34.7% | – | ✅ |
| **General structured PII** | French | **13.1%** 🥇 | 35.8% | 84.1% | 84.1% | – | 53.9% | 34.4% | – | ✅ |
| **General structured PII** | Italian | **14.9%** 🥇 | 39.0% | 80.9% | 80.9% | – | 59.5% | 37.0% | – | ✅ |
| **Academic NER (newswire / social)** | English | **7.8%** 🥇 | 20.8% | 96.9% | 96.9% | – | 17.4% | 12.2% | – | ✅ |
| **Academic NER (newswire / social)** | German | **8.0%** 🥇 | 14.0% | 44.4% | 44.4% | – | 19.1% | 15.5% | – | ✅ |
| **Adversarial / out-of-distribution** | German | **9.2%** 🥇 | 28.2% | 33.2% | 33.2% | – | 37.5% | 40.5% | – | ✅ |

## Clinical / medical de-identification · English

Corpora in this group: `synth_clinical_en`.

### Leak rate · lower is better

A gold PHI span is *leaked* when **no** predicted span overlaps it — 'did we miss a name?'

| Corpus | `anonde-patterns` | `anonde-gliner` | `anonde-gliner-fp32` | `anonde-gliner-large` | `anonde-gliner-stack` | `presidio` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|---:|---:|---:|
| `synth_clinical_en` | 27.9% | 15.4% | **14.5%** 🥇 | 27.9% | – | 20.3% | 23.3% | – |

### Severity-weighted leak rate · lower is better

Each leaked span weighted by compliance tier — direct identifiers (PERSON, EMAIL, PHONE, ADDRESS, DOB) = 5, high-stakes IDs (SSN/MRN/IBAN) = 10, quasi-identifiers (LOCATION, ORG, PROFESSION) = 1. Defaults in `label_map.yaml::severity`. Shown only because at least one cell here moves >3pp from raw leak; otherwise the two tables tracked within noise.

| Corpus | `anonde-patterns` | `anonde-gliner` | `anonde-gliner-fp32` | `anonde-gliner-large` | `anonde-gliner-stack` | `presidio` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|---:|---:|---:|
| `synth_clinical_en` | 27.6% | **15.5%** 🥇 | 16.2% | 27.6% | – | 26.7% | 26.4% | – |

## Clinical / medical de-identification · German

Corpora in this group: `openmed`, `pmc_de`, `synth_clinical`, `wiki_de`.

### Leak rate · lower is better

A gold PHI span is *leaked* when **no** predicted span overlaps it — 'did we miss a name?'

| Corpus | `anonde-patterns` | `anonde-gliner` | `anonde-gliner-fp32` | `anonde-gliner-large` | `anonde-gliner-stack` | `presidio` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|---:|---:|---:|
| `openmed` | 22.8% | 15.0% | **12.0%** 🥇 | 22.8% | – | 33.9% | 50.0% | – |
| `synth_clinical` | 11.8% | 9.6% | **2.3%** 🥇 | 11.8% | – | 29.1% | 25.4% | – |

> **Partial coverage** — some engines were benchmarked on a fixed subsample, not every gold doc:
>
> - `presidio` on `pmc_de`: scored on **106/136 docs** (deterministic subsample — metrics above are over those 106 docs only, not the full corpus).
> - `anonde-gliner` on `wiki_de`: scored on **7/45 docs** (deterministic subsample — metrics above are over those 7 docs only, not the full corpus).

### Severity-weighted leak rate · lower is better

Each leaked span weighted by compliance tier — direct identifiers (PERSON, EMAIL, PHONE, ADDRESS, DOB) = 5, high-stakes IDs (SSN/MRN/IBAN) = 10, quasi-identifiers (LOCATION, ORG, PROFESSION) = 1. Defaults in `label_map.yaml::severity`. Shown only because at least one cell here moves >3pp from raw leak; otherwise the two tables tracked within noise.

| Corpus | `anonde-patterns` | `anonde-gliner` | `anonde-gliner-fp32` | `anonde-gliner-large` | `anonde-gliner-stack` | `presidio` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|---:|---:|---:|
| `openmed` | 22.6% | 14.9% | **11.9%** 🥇 | 22.6% | – | 35.5% | 50.7% | – |
| `synth_clinical` | 10.9% | 10.2% | **2.1%** 🥇 | 10.9% | – | 33.3% | 27.5% | – |

## Clinical / medical de-identification · Spanish

Corpora in this group: `pharmaconer_es`, `meddocan_es`.

### Leak rate · lower is better

A gold PHI span is *leaked* when **no** predicted span overlaps it — 'did we miss a name?'

| Corpus | `anonde-patterns` | `anonde-gliner` | `anonde-gliner-fp32` | `anonde-gliner-large` | `anonde-gliner-stack` | `presidio` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|---:|---:|---:|
| `meddocan_es` | 92.1% | 36.3% | **24.4%** 🥇 | 92.1% | – | 38.4% | **24.4%** 🥇 | – |

### Severity-weighted leak rate · lower is better

Each leaked span weighted by compliance tier — direct identifiers (PERSON, EMAIL, PHONE, ADDRESS, DOB) = 5, high-stakes IDs (SSN/MRN/IBAN) = 10, quasi-identifiers (LOCATION, ORG, PROFESSION) = 1. Defaults in `label_map.yaml::severity`. Shown only because at least one cell here moves >3pp from raw leak; otherwise the two tables tracked within noise.

| Corpus | `anonde-patterns` | `anonde-gliner` | `anonde-gliner-fp32` | `anonde-gliner-large` | `anonde-gliner-stack` | `presidio` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|---:|---:|---:|
| `meddocan_es` | 90.5% | 36.6% | 29.4% | 90.5% | – | 46.4% | **28.7%** 🥇 | – |

## Clinical / medical de-identification · French

Corpora in this group: `synth_clinical_fr`.

### Leak rate · lower is better

A gold PHI span is *leaked* when **no** predicted span overlaps it — 'did we miss a name?'

| Corpus | `anonde-patterns` | `anonde-gliner` | `anonde-gliner-fp32` | `anonde-gliner-large` | `anonde-gliner-stack` | `presidio` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|---:|---:|---:|
| `synth_clinical_fr` | 83.3% | 33.2% | **24.9%** 🥇 | 83.3% | – | 28.2% | 25.3% | – |

## Clinical / medical de-identification · Italian

Corpora in this group: `synth_clinical_it`.

### Leak rate · lower is better

A gold PHI span is *leaked* when **no** predicted span overlaps it — 'did we miss a name?'

| Corpus | `anonde-patterns` | `anonde-gliner` | `anonde-gliner-fp32` | `anonde-gliner-large` | `anonde-gliner-stack` | `presidio` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|---:|---:|---:|
| `synth_clinical_it` | 82.6% | 37.5% | 30.9% | 82.6% | – | **28.5%** 🥇 | 35.2% | – |

### Severity-weighted leak rate · lower is better

Each leaked span weighted by compliance tier — direct identifiers (PERSON, EMAIL, PHONE, ADDRESS, DOB) = 5, high-stakes IDs (SSN/MRN/IBAN) = 10, quasi-identifiers (LOCATION, ORG, PROFESSION) = 1. Defaults in `label_map.yaml::severity`. Shown only because at least one cell here moves >3pp from raw leak; otherwise the two tables tracked within noise.

| Corpus | `anonde-patterns` | `anonde-gliner` | `anonde-gliner-fp32` | `anonde-gliner-large` | `anonde-gliner-stack` | `presidio` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|---:|---:|---:|
| `synth_clinical_it` | 80.6% | 36.6% | **31.9%** 🥇 | 80.6% | – | 31.9% | 38.7% | – |

## Legal / administrative · English

Corpora in this group: `mapa_en`.

### Leak rate · lower is better

A gold PHI span is *leaked* when **no** predicted span overlaps it — 'did we miss a name?'

| Corpus | `anonde-patterns` | `anonde-gliner` | `anonde-gliner-fp32` | `anonde-gliner-large` | `anonde-gliner-stack` | `presidio` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|---:|---:|---:|
| `mapa_en` | 50.2% | 12.4% | **5.8%** 🥇 | 50.2% | – | 6.2% | 11.6% | – |

### Severity-weighted leak rate · lower is better

Each leaked span weighted by compliance tier — direct identifiers (PERSON, EMAIL, PHONE, ADDRESS, DOB) = 5, high-stakes IDs (SSN/MRN/IBAN) = 10, quasi-identifiers (LOCATION, ORG, PROFESSION) = 1. Defaults in `label_map.yaml::severity`. Shown only because at least one cell here moves >3pp from raw leak; otherwise the two tables tracked within noise.

| Corpus | `anonde-patterns` | `anonde-gliner` | `anonde-gliner-fp32` | `anonde-gliner-large` | `anonde-gliner-stack` | `presidio` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|---:|---:|---:|
| `mapa_en` | 44.5% | 12.0% | 6.1% | 44.5% | – | **4.9%** 🥇 | 10.9% | – |

## Legal / administrative · German

Corpora in this group: `legal_de`, `mapa_de`.

### Leak rate · lower is better

A gold PHI span is *leaked* when **no** predicted span overlaps it — 'did we miss a name?'

| Corpus | `anonde-patterns` | `anonde-gliner` | `anonde-gliner-fp32` | `anonde-gliner-large` | `anonde-gliner-stack` | `presidio` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|---:|---:|---:|
| `legal_de` | 9.4% | 6.0% | **0.3%** 🥇 | 9.4% | – | 24.5% | 25.4% | – |
| `mapa_de` | 49.0% | 10.1% | **7.8%** 🥇 | 49.0% | – | 44.8% | 10.7% | – |

### Severity-weighted leak rate · lower is better

Each leaked span weighted by compliance tier — direct identifiers (PERSON, EMAIL, PHONE, ADDRESS, DOB) = 5, high-stakes IDs (SSN/MRN/IBAN) = 10, quasi-identifiers (LOCATION, ORG, PROFESSION) = 1. Defaults in `label_map.yaml::severity`. Shown only because at least one cell here moves >3pp from raw leak; otherwise the two tables tracked within noise.

| Corpus | `anonde-patterns` | `anonde-gliner` | `anonde-gliner-fp32` | `anonde-gliner-large` | `anonde-gliner-stack` | `presidio` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|---:|---:|---:|
| `legal_de` | 18.4% | 12.7% | **0.2%** 🥇 | 18.4% | – | 32.9% | 33.3% | – |
| `mapa_de` | 32.0% | 8.3% | **7.2%** 🥇 | 32.0% | – | 52.0% | 8.5% | – |

## Legal / administrative · Spanish

Corpora in this group: `mapa_es`.

### Leak rate · lower is better

A gold PHI span is *leaked* when **no** predicted span overlaps it — 'did we miss a name?'

| Corpus | `anonde-patterns` | `anonde-gliner` | `anonde-gliner-fp32` | `anonde-gliner-large` | `anonde-gliner-stack` | `presidio` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|---:|---:|---:|
| `mapa_es` | 100.0% | 45.2% | **18.3%** 🥇 | 100.0% | – | 59.1% | **18.3%** 🥇 | – |

## Legal / administrative · French

Corpora in this group: `mapa_fr`.

### Leak rate · lower is better

A gold PHI span is *leaked* when **no** predicted span overlaps it — 'did we miss a name?'

| Corpus | `anonde-patterns` | `anonde-gliner` | `anonde-gliner-fp32` | `anonde-gliner-large` | `anonde-gliner-stack` | `presidio` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|---:|---:|---:|
| `mapa_fr` | 100.0% | 43.7% | **7.9%** 🥇 | 100.0% | – | 44.9% | 16.0% | – |

### Severity-weighted leak rate · lower is better

Each leaked span weighted by compliance tier — direct identifiers (PERSON, EMAIL, PHONE, ADDRESS, DOB) = 5, high-stakes IDs (SSN/MRN/IBAN) = 10, quasi-identifiers (LOCATION, ORG, PROFESSION) = 1. Defaults in `label_map.yaml::severity`. Shown only because at least one cell here moves >3pp from raw leak; otherwise the two tables tracked within noise.

| Corpus | `anonde-patterns` | `anonde-gliner` | `anonde-gliner-fp32` | `anonde-gliner-large` | `anonde-gliner-stack` | `presidio` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|---:|---:|---:|
| `mapa_fr` | 100.0% | 44.3% | **8.1%** 🥇 | 100.0% | – | 53.2% | 10.4% | – |

## Legal / administrative · Italian

Corpora in this group: `mapa_it`.

### Leak rate · lower is better

A gold PHI span is *leaked* when **no** predicted span overlaps it — 'did we miss a name?'

| Corpus | `anonde-patterns` | `anonde-gliner` | `anonde-gliner-fp32` | `anonde-gliner-large` | `anonde-gliner-stack` | `presidio` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|---:|---:|---:|
| `mapa_it` | 100.0% | 61.0% | 21.2% | 100.0% | – | 67.1% | **6.8%** 🥇 | – |

## Retail finance · English

Corpora in this group: `synth_finance_en`.

### Leak rate · lower is better

A gold PHI span is *leaked* when **no** predicted span overlaps it — 'did we miss a name?'

| Corpus | `anonde-patterns` | `anonde-gliner` | `anonde-gliner-fp32` | `anonde-gliner-large` | `anonde-gliner-stack` | `presidio` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|---:|---:|---:|
| `synth_finance_en` | 40.2% | 9.0% | **1.8%** 🥇 | 40.2% | – | 10.3% | – | – |

### Severity-weighted leak rate · lower is better

Each leaked span weighted by compliance tier — direct identifiers (PERSON, EMAIL, PHONE, ADDRESS, DOB) = 5, high-stakes IDs (SSN/MRN/IBAN) = 10, quasi-identifiers (LOCATION, ORG, PROFESSION) = 1. Defaults in `label_map.yaml::severity`. Shown only because at least one cell here moves >3pp from raw leak; otherwise the two tables tracked within noise.

| Corpus | `anonde-patterns` | `anonde-gliner` | `anonde-gliner-fp32` | `anonde-gliner-large` | `anonde-gliner-stack` | `presidio` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|---:|---:|---:|
| `synth_finance_en` | 29.3% | 7.8% | **1.4%** 🥇 | 29.3% | – | 11.9% | – | – |

## Retail finance · German

Corpora in this group: `finance_de`, `synth_finance_de`.

### Leak rate · lower is better

A gold PHI span is *leaked* when **no** predicted span overlaps it — 'did we miss a name?'

| Corpus | `anonde-patterns` | `anonde-gliner` | `anonde-gliner-fp32` | `anonde-gliner-large` | `anonde-gliner-stack` | `presidio` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|---:|---:|---:|
| `finance_de` | 18.1% | 7.5% | **1.0%** 🥇 | 18.1% | – | 25.9% | 26.2% | – |
| `synth_finance_de` | 17.4% | 3.9% | **2.0%** 🥇 | 17.4% | – | 21.4% | 17.2% | – |

### Severity-weighted leak rate · lower is better

Each leaked span weighted by compliance tier — direct identifiers (PERSON, EMAIL, PHONE, ADDRESS, DOB) = 5, high-stakes IDs (SSN/MRN/IBAN) = 10, quasi-identifiers (LOCATION, ORG, PROFESSION) = 1. Defaults in `label_map.yaml::severity`. Shown only because at least one cell here moves >3pp from raw leak; otherwise the two tables tracked within noise.

| Corpus | `anonde-patterns` | `anonde-gliner` | `anonde-gliner-fp32` | `anonde-gliner-large` | `anonde-gliner-stack` | `presidio` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|---:|---:|---:|
| `finance_de` | 23.0% | 11.0% | **1.3%** 🥇 | 23.0% | – | 30.2% | 29.2% | – |
| `synth_finance_de` | 13.7% | 2.7% | **2.1%** 🥇 | 13.7% | – | 23.0% | 17.2% | – |

## Retail finance · Spanish

Corpora in this group: `synth_finance_es`.

### Leak rate · lower is better

A gold PHI span is *leaked* when **no** predicted span overlaps it — 'did we miss a name?'

| Corpus | `anonde-patterns` | `anonde-gliner` | `anonde-gliner-fp32` | `anonde-gliner-large` | `anonde-gliner-stack` | `presidio` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|---:|---:|---:|
| `synth_finance_es` | 60.5% | 23.1% | **15.0%** 🥇 | 60.5% | – | 18.5% | 25.4% | – |

### Severity-weighted leak rate · lower is better

Each leaked span weighted by compliance tier — direct identifiers (PERSON, EMAIL, PHONE, ADDRESS, DOB) = 5, high-stakes IDs (SSN/MRN/IBAN) = 10, quasi-identifiers (LOCATION, ORG, PROFESSION) = 1. Defaults in `label_map.yaml::severity`. Shown only because at least one cell here moves >3pp from raw leak; otherwise the two tables tracked within noise.

| Corpus | `anonde-patterns` | `anonde-gliner` | `anonde-gliner-fp32` | `anonde-gliner-large` | `anonde-gliner-stack` | `presidio` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|---:|---:|---:|
| `synth_finance_es` | 49.7% | 19.6% | **13.4%** 🥇 | 49.7% | – | 19.9% | 28.8% | – |

## Retail finance · French

Corpora in this group: `synth_finance_fr`.

### Leak rate · lower is better

A gold PHI span is *leaked* when **no** predicted span overlaps it — 'did we miss a name?'

| Corpus | `anonde-patterns` | `anonde-gliner` | `anonde-gliner-fp32` | `anonde-gliner-large` | `anonde-gliner-stack` | `presidio` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|---:|---:|---:|
| `synth_finance_fr` | 60.3% | 22.8% | **14.5%** 🥇 | 60.3% | – | 19.4% | 23.9% | – |

### Severity-weighted leak rate · lower is better

Each leaked span weighted by compliance tier — direct identifiers (PERSON, EMAIL, PHONE, ADDRESS, DOB) = 5, high-stakes IDs (SSN/MRN/IBAN) = 10, quasi-identifiers (LOCATION, ORG, PROFESSION) = 1. Defaults in `label_map.yaml::severity`. Shown only because at least one cell here moves >3pp from raw leak; otherwise the two tables tracked within noise.

| Corpus | `anonde-patterns` | `anonde-gliner` | `anonde-gliner-fp32` | `anonde-gliner-large` | `anonde-gliner-stack` | `presidio` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|---:|---:|---:|
| `synth_finance_fr` | 49.0% | 18.5% | **11.8%** 🥇 | 49.0% | – | 21.7% | 26.4% | – |

## Retail finance · Italian

Corpora in this group: `synth_finance_it`.

### Leak rate · lower is better

A gold PHI span is *leaked* when **no** predicted span overlaps it — 'did we miss a name?'

| Corpus | `anonde-patterns` | `anonde-gliner` | `anonde-gliner-fp32` | `anonde-gliner-large` | `anonde-gliner-stack` | `presidio` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|---:|---:|---:|
| `synth_finance_it` | 59.9% | 22.9% | **15.7%** 🥇 | 59.9% | – | 29.6% | 26.4% | – |

### Severity-weighted leak rate · lower is better

Each leaked span weighted by compliance tier — direct identifiers (PERSON, EMAIL, PHONE, ADDRESS, DOB) = 5, high-stakes IDs (SSN/MRN/IBAN) = 10, quasi-identifiers (LOCATION, ORG, PROFESSION) = 1. Defaults in `label_map.yaml::severity`. Shown only because at least one cell here moves >3pp from raw leak; otherwise the two tables tracked within noise.

| Corpus | `anonde-patterns` | `anonde-gliner` | `anonde-gliner-fp32` | `anonde-gliner-large` | `anonde-gliner-stack` | `presidio` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|---:|---:|---:|
| `synth_finance_it` | 48.6% | 18.4% | **12.7%** 🥇 | 48.6% | – | 40.5% | 23.8% | – |

## Enterprise logs · English

Corpora in this group: `synth_logs`.

### Leak rate · lower is better

A gold PHI span is *leaked* when **no** predicted span overlaps it — 'did we miss a name?'

| Corpus | `anonde-patterns` | `anonde-gliner` | `anonde-gliner-fp32` | `anonde-gliner-large` | `anonde-gliner-stack` | `presidio` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|---:|---:|---:|
| `synth_logs` | 41.3% | 26.3% | **22.4%** 🥇 | 41.3% | – | 24.2% | 73.9% | – |

### Severity-weighted leak rate · lower is better

Each leaked span weighted by compliance tier — direct identifiers (PERSON, EMAIL, PHONE, ADDRESS, DOB) = 5, high-stakes IDs (SSN/MRN/IBAN) = 10, quasi-identifiers (LOCATION, ORG, PROFESSION) = 1. Defaults in `label_map.yaml::severity`. Shown only because at least one cell here moves >3pp from raw leak; otherwise the two tables tracked within noise.

| Corpus | `anonde-patterns` | `anonde-gliner` | `anonde-gliner-fp32` | `anonde-gliner-large` | `anonde-gliner-stack` | `presidio` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|---:|---:|---:|
| `synth_logs` | 49.9% | 32.6% | **29.1%** 🥇 | 49.9% | – | 31.5% | 82.1% | – |

## General structured PII · English

Corpora in this group: `ai4privacy_en`.

### Leak rate · lower is better

A gold PHI span is *leaked* when **no** predicted span overlaps it — 'did we miss a name?'

| Corpus | `anonde-patterns` | `anonde-gliner` | `anonde-gliner-fp32` | `anonde-gliner-large` | `anonde-gliner-stack` | `presidio` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|---:|---:|---:|
| `ai4privacy_en` | 55.4% | 25.0% | **4.0%** 🥇 | 55.4% | – | 28.4% | 25.8% | – |

### Severity-weighted leak rate · lower is better

Each leaked span weighted by compliance tier — direct identifiers (PERSON, EMAIL, PHONE, ADDRESS, DOB) = 5, high-stakes IDs (SSN/MRN/IBAN) = 10, quasi-identifiers (LOCATION, ORG, PROFESSION) = 1. Defaults in `label_map.yaml::severity`. Shown only because at least one cell here moves >3pp from raw leak; otherwise the two tables tracked within noise.

| Corpus | `anonde-patterns` | `anonde-gliner` | `anonde-gliner-fp32` | `anonde-gliner-large` | `anonde-gliner-stack` | `presidio` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|---:|---:|---:|
| `ai4privacy_en` | 39.4% | 16.4% | **1.6%** 🥇 | 39.4% | – | 25.3% | 31.5% | – |

## General structured PII · German

Corpora in this group: `ai4privacy_de`.

### Leak rate · lower is better

A gold PHI span is *leaked* when **no** predicted span overlaps it — 'did we miss a name?'

| Corpus | `anonde-patterns` | `anonde-gliner` | `anonde-gliner-fp32` | `anonde-gliner-large` | `anonde-gliner-stack` | `presidio` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|---:|---:|---:|
| `ai4privacy_de` | 69.7% | 28.0% | **10.1%** 🥇 | 69.7% | – | 57.9% | 34.5% | – |

### Severity-weighted leak rate · lower is better

Each leaked span weighted by compliance tier — direct identifiers (PERSON, EMAIL, PHONE, ADDRESS, DOB) = 5, high-stakes IDs (SSN/MRN/IBAN) = 10, quasi-identifiers (LOCATION, ORG, PROFESSION) = 1. Defaults in `label_map.yaml::severity`. Shown only because at least one cell here moves >3pp from raw leak; otherwise the two tables tracked within noise.

| Corpus | `anonde-patterns` | `anonde-gliner` | `anonde-gliner-fp32` | `anonde-gliner-large` | `anonde-gliner-stack` | `presidio` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|---:|---:|---:|
| `ai4privacy_de` | 71.9% | 25.4% | **10.5%** 🥇 | 71.9% | – | 61.0% | 36.0% | – |

## General structured PII · Spanish

Corpora in this group: `ai4privacy_es`.

### Leak rate · lower is better

A gold PHI span is *leaked* when **no** predicted span overlaps it — 'did we miss a name?'

| Corpus | `anonde-patterns` | `anonde-gliner` | `anonde-gliner-fp32` | `anonde-gliner-large` | `anonde-gliner-stack` | `presidio` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|---:|---:|---:|
| `ai4privacy_es` | 84.7% | 36.6% | **12.2%** 🥇 | 84.7% | – | 61.5% | 34.7% | – |

### Severity-weighted leak rate · lower is better

Each leaked span weighted by compliance tier — direct identifiers (PERSON, EMAIL, PHONE, ADDRESS, DOB) = 5, high-stakes IDs (SSN/MRN/IBAN) = 10, quasi-identifiers (LOCATION, ORG, PROFESSION) = 1. Defaults in `label_map.yaml::severity`. Shown only because at least one cell here moves >3pp from raw leak; otherwise the two tables tracked within noise.

| Corpus | `anonde-patterns` | `anonde-gliner` | `anonde-gliner-fp32` | `anonde-gliner-large` | `anonde-gliner-stack` | `presidio` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|---:|---:|---:|
| `ai4privacy_es` | 81.7% | 29.2% | **12.1%** 🥇 | 81.7% | – | 65.1% | 35.4% | – |

## General structured PII · French

Corpora in this group: `ai4privacy_fr`.

### Leak rate · lower is better

A gold PHI span is *leaked* when **no** predicted span overlaps it — 'did we miss a name?'

| Corpus | `anonde-patterns` | `anonde-gliner` | `anonde-gliner-fp32` | `anonde-gliner-large` | `anonde-gliner-stack` | `presidio` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|---:|---:|---:|
| `ai4privacy_fr` | 84.1% | 35.8% | **13.1%** 🥇 | 84.1% | – | 53.9% | 34.4% | – |

### Severity-weighted leak rate · lower is better

Each leaked span weighted by compliance tier — direct identifiers (PERSON, EMAIL, PHONE, ADDRESS, DOB) = 5, high-stakes IDs (SSN/MRN/IBAN) = 10, quasi-identifiers (LOCATION, ORG, PROFESSION) = 1. Defaults in `label_map.yaml::severity`. Shown only because at least one cell here moves >3pp from raw leak; otherwise the two tables tracked within noise.

| Corpus | `anonde-patterns` | `anonde-gliner` | `anonde-gliner-fp32` | `anonde-gliner-large` | `anonde-gliner-stack` | `presidio` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|---:|---:|---:|
| `ai4privacy_fr` | 79.5% | 28.4% | **12.4%** 🥇 | 79.5% | – | 56.6% | 35.9% | – |

## General structured PII · Italian

Corpora in this group: `ai4privacy_it`.

### Leak rate · lower is better

A gold PHI span is *leaked* when **no** predicted span overlaps it — 'did we miss a name?'

| Corpus | `anonde-patterns` | `anonde-gliner` | `anonde-gliner-fp32` | `anonde-gliner-large` | `anonde-gliner-stack` | `presidio` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|---:|---:|---:|
| `ai4privacy_it` | 80.9% | 39.0% | **14.9%** 🥇 | 80.9% | – | 59.5% | 37.0% | – |

### Severity-weighted leak rate · lower is better

Each leaked span weighted by compliance tier — direct identifiers (PERSON, EMAIL, PHONE, ADDRESS, DOB) = 5, high-stakes IDs (SSN/MRN/IBAN) = 10, quasi-identifiers (LOCATION, ORG, PROFESSION) = 1. Defaults in `label_map.yaml::severity`. Shown only because at least one cell here moves >3pp from raw leak; otherwise the two tables tracked within noise.

| Corpus | `anonde-patterns` | `anonde-gliner` | `anonde-gliner-fp32` | `anonde-gliner-large` | `anonde-gliner-stack` | `presidio` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|---:|---:|---:|
| `ai4privacy_it` | 74.6% | 31.4% | **14.2%** 🥇 | 74.6% | – | 62.7% | 38.0% | – |

## Academic NER (newswire / social) · English

Corpora in this group: `conll2003_en`, `wnut_17`.

### Leak rate · lower is better

A gold PHI span is *leaked* when **no** predicted span overlaps it — 'did we miss a name?'

| Corpus | `anonde-patterns` | `anonde-gliner` | `anonde-gliner-fp32` | `anonde-gliner-large` | `anonde-gliner-stack` | `presidio` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|---:|---:|---:|
| `conll2003_en` | 96.9% | 16.4% | **6.0%** 🥇 | 96.9% | – | 6.7% | **6.0%** 🥇 | – |
| `wnut_17` | 96.8% | 31.4% | **12.2%** 🥇 | 96.8% | – | 43.1% | 27.1% | – |

### Severity-weighted leak rate · lower is better

Each leaked span weighted by compliance tier — direct identifiers (PERSON, EMAIL, PHONE, ADDRESS, DOB) = 5, high-stakes IDs (SSN/MRN/IBAN) = 10, quasi-identifiers (LOCATION, ORG, PROFESSION) = 1. Defaults in `label_map.yaml::severity`. Shown only because at least one cell here moves >3pp from raw leak; otherwise the two tables tracked within noise.

| Corpus | `anonde-patterns` | `anonde-gliner` | `anonde-gliner-fp32` | `anonde-gliner-large` | `anonde-gliner-stack` | `presidio` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|---:|---:|---:|
| `conll2003_en` | 97.5% | 22.5% | 7.7% | 97.5% | – | **5.3%** 🥇 | 6.9% | – |
| `wnut_17` | 96.0% | 31.0% | **9.2%** 🥇 | 96.0% | – | 42.2% | 25.9% | – |

## Academic NER (newswire / social) · German

Corpora in this group: `wikiann_de`, `germeval_14`, `conll2003_de`.

### Leak rate · lower is better

A gold PHI span is *leaked* when **no** predicted span overlaps it — 'did we miss a name?'

| Corpus | `anonde-patterns` | `anonde-gliner` | `anonde-gliner-fp32` | `anonde-gliner-large` | `anonde-gliner-stack` | `presidio` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|---:|---:|---:|
| `wikiann_de` | 38.7% | 10.3% | **5.4%** 🥇 | 38.7% | – | 16.7% | 12.8% | – |
| `germeval_14` | 51.6% | 18.6% | **11.2%** 🥇 | 51.6% | – | 22.0% | 18.9% | – |

### Severity-weighted leak rate · lower is better

Each leaked span weighted by compliance tier — direct identifiers (PERSON, EMAIL, PHONE, ADDRESS, DOB) = 5, high-stakes IDs (SSN/MRN/IBAN) = 10, quasi-identifiers (LOCATION, ORG, PROFESSION) = 1. Defaults in `label_map.yaml::severity`. Shown only because at least one cell here moves >3pp from raw leak; otherwise the two tables tracked within noise.

| Corpus | `anonde-patterns` | `anonde-gliner` | `anonde-gliner-fp32` | `anonde-gliner-large` | `anonde-gliner-stack` | `presidio` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|---:|---:|---:|
| `wikiann_de` | 29.0% | 7.8% | **4.4%** 🥇 | 29.0% | – | 6.9% | 10.7% | – |
| `germeval_14` | 46.1% | 13.5% | **7.0%** 🥇 | 46.1% | – | 15.8% | 16.1% | – |

## Adversarial / out-of-distribution · German

Corpora in this group: `adversarial_de`.

### Leak rate · lower is better

A gold PHI span is *leaked* when **no** predicted span overlaps it — 'did we miss a name?'

| Corpus | `anonde-patterns` | `anonde-gliner` | `anonde-gliner-fp32` | `anonde-gliner-large` | `anonde-gliner-stack` | `presidio` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|---:|---:|---:|
| `adversarial_de` | 33.2% | 28.2% | **9.2%** 🥇 | 33.2% | – | 37.5% | 40.5% | – |

### Severity-weighted leak rate · lower is better

Each leaked span weighted by compliance tier — direct identifiers (PERSON, EMAIL, PHONE, ADDRESS, DOB) = 5, high-stakes IDs (SSN/MRN/IBAN) = 10, quasi-identifiers (LOCATION, ORG, PROFESSION) = 1. Defaults in `label_map.yaml::severity`. Shown only because at least one cell here moves >3pp from raw leak; otherwise the two tables tracked within noise.

| Corpus | `anonde-patterns` | `anonde-gliner` | `anonde-gliner-fp32` | `anonde-gliner-large` | `anonde-gliner-stack` | `presidio` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|---:|---:|---:|
| `adversarial_de` | 31.4% | 25.3% | **9.3%** 🥇 | 31.4% | – | 40.7% | 40.2% | – |

## Latency · per-document p50 / p95

Wall-clock per `engine.Analyze(doc)` call. p50 = steady-state, p95 = tail (the SLO knob). Mean + p99 in `results_matrix.csv`. One table across every corpus — latency tracks corpus length, not domain or language.

| Corpus | `anonde-patterns` p50 / p95 | `anonde-gliner` p50 / p95 | `anonde-gliner-fp32` p50 / p95 | `anonde-gliner-large` p50 / p95 | `anonde-gliner-stack` p50 / p95 | `presidio` p50 / p95 | `gliner-py` p50 / p95 | `openai-pf` p50 / p95 |
|---|---:|---:|---:|---:|---:|---:|---:|---:|
| `synth_clinical_en` | 0 ms / 2 ms | 64 ms / 103 ms | 131 ms / 206 ms | 0 ms / 1 ms | – | 33 ms / 45 ms | 131 ms / 171 ms | – |
| `openmed` | 1 ms / 3 ms | 187 ms / 736 ms | 373 ms / 1.3 s | 1 ms / 2 ms | – | 99 ms / 256 ms | 453 ms / 2.5 s | – |
| `pmc_de` | 7 ms / 76 ms | 5.1 s / 50.9 s | 3.1 s / 7.1 s | 5 ms / 98 ms | – | 568 ms / 3.1 s | 7.0 s / 14.0 s | – |
| `synth_clinical` | 0 ms / 1 ms | 64 ms / 114 ms | 121 ms / 194 ms | 0 ms / 1 ms | – | 33 ms / 44 ms | 162 ms / 207 ms | – |
| `wiki_de` | 2 ms / 12 ms | 958 ms / 17.8 s | 628 ms / 3.3 s | 1 ms / 8 ms | – | 125 ms / 603 ms | 1.4 s / 7.8 s | – |
| `pharmaconer_es` | 0 ms / 1 ms | 284 ms / 603 ms | 288 ms / 629 ms | 0 ms / 1 ms | – | 49 ms / 114 ms | 319 ms / 781 ms | – |
| `meddocan_es` | 0 ms / 1 ms | 201 ms / 412 ms | 352 ms / 674 ms | 0 ms / 1 ms | – | 71 ms / 140 ms | 391 ms / 887 ms | – |
| `synth_clinical_fr` | 0 ms / 0 ms | 93 ms / 156 ms | 180 ms / 246 ms | 0 ms / 0 ms | – | 36 ms / 50 ms | 169 ms / 228 ms | – |
| `synth_clinical_it` | 0 ms / 0 ms | 73 ms / 113 ms | 127 ms / 220 ms | 0 ms / 1 ms | – | 33 ms / 45 ms | 159 ms / 282 ms | – |
| `mapa_en` | 0 ms / 0 ms | 29 ms / 61 ms | 41 ms / 92 ms | 0 ms / 0 ms | – | 8 ms / 23 ms | 69 ms / 148 ms | – |
| `legal_de` | 0 ms / 1 ms | 217 ms / 607 ms | 112 ms / 124 ms | 0 ms / 0 ms | – | 27 ms / 42 ms | 193 ms / 345 ms | – |
| `mapa_de` | 0 ms / 0 ms | 30 ms / 52 ms | 43 ms / 74 ms | 0 ms / 0 ms | – | 7 ms / 14 ms | 68 ms / 103 ms | – |
| `mapa_es` | 0 ms / 0 ms | 32 ms / 64 ms | 46 ms / 105 ms | 0 ms / 0 ms | – | 8 ms / 22 ms | 72 ms / 148 ms | – |
| `mapa_fr` | 0 ms / 0 ms | 30 ms / 54 ms | 49 ms / 114 ms | 0 ms / 0 ms | – | 8 ms / 21 ms | 79 ms / 134 ms | – |
| `mapa_it` | 0 ms / 0 ms | 28 ms / 45 ms | 45 ms / 77 ms | 0 ms / 0 ms | – | 7 ms / 13 ms | 62 ms / 93 ms | – |
| `synth_finance_en` | 0 ms / 1 ms | 55 ms / 77 ms | 87 ms / 126 ms | 0 ms / 1 ms | – | 26 ms / 55 ms | – | – |
| `finance_de` | 0 ms / 1 ms | 157 ms / 369 ms | 113 ms / 175 ms | 0 ms / 1 ms | – | 28 ms / 45 ms | 187 ms / 303 ms | – |
| `synth_finance_de` | 0 ms / 1 ms | 59 ms / 74 ms | 96 ms / 125 ms | 0 ms / 0 ms | – | 21 ms / 40 ms | 132 ms / 246 ms | – |
| `synth_finance_es` | 0 ms / 1 ms | 77 ms / 145 ms | 102 ms / 138 ms | 0 ms / 0 ms | – | 22 ms / 38 ms | 128 ms / 175 ms | – |
| `synth_finance_fr` | 0 ms / 0 ms | 61 ms / 80 ms | 117 ms / 168 ms | 0 ms / 1 ms | – | 23 ms / 45 ms | 137 ms / 184 ms | – |
| `synth_finance_it` | 0 ms / 0 ms | 57 ms / 77 ms | 112 ms / 171 ms | 0 ms / 1 ms | – | 21 ms / 38 ms | 120 ms / 181 ms | – |
| `synth_logs` | 1 ms / 1 ms | 125 ms / 317 ms | 203 ms / 375 ms | 1 ms / 2 ms | – | 43 ms / 95 ms | 338 ms / 859 ms | – |
| `ai4privacy_en` | 0 ms / 0 ms | 59 ms / 307 ms | 42 ms / 58 ms | 0 ms / 0 ms | – | 6 ms / 9 ms | 77 ms / 179 ms | – |
| `ai4privacy_de` | 0 ms / 0 ms | 44 ms / 59 ms | 79 ms / 120 ms | 0 ms / 0 ms | – | 15 ms / 23 ms | 102 ms / 152 ms | – |
| `ai4privacy_es` | 0 ms / 0 ms | 47 ms / 66 ms | 81 ms / 126 ms | 0 ms / 0 ms | – | 14 ms / 21 ms | 111 ms / 181 ms | – |
| `ai4privacy_fr` | 0 ms / 0 ms | 50 ms / 88 ms | 84 ms / 124 ms | 0 ms / 0 ms | – | 16 ms / 24 ms | 107 ms / 153 ms | – |
| `ai4privacy_it` | 0 ms / 0 ms | 45 ms / 61 ms | 92 ms / 138 ms | 0 ms / 0 ms | – | 14 ms / 20 ms | 91 ms / 120 ms | – |
| `conll2003_en` | 0 ms / 0 ms | 65 ms / 145 ms | 30 ms / 44 ms | 0 ms / 0 ms | – | 4 ms / 9 ms | 54 ms / 64 ms | – |
| `wnut_17` | 0 ms / 0 ms | 104 ms / 320 ms | 35 ms / 54 ms | 0 ms / 0 ms | – | 5 ms / 10 ms | 69 ms / 110 ms | – |
| `wikiann_de` | 0 ms / 0 ms | 42 ms / 66 ms | 30 ms / 40 ms | 0 ms / 0 ms | – | 4 ms / 8 ms | 69 ms / 105 ms | – |
| `germeval_14` | 0 ms / 0 ms | 49 ms / 89 ms | 35 ms / 48 ms | 0 ms / 0 ms | – | 5 ms / 8 ms | 55 ms / 65 ms | – |
| `adversarial_de` | 0 ms / 1 ms | 119 ms / 230 ms | 120 ms / 195 ms | 0 ms / 1 ms | – | 33 ms / 45 ms | 176 ms / 299 ms | – |

<details><summary>Cost reference · USD per million characters</summary>

## Cost reference · USD per million characters

All engines in this matrix run on your hardware — no per-call charge. For procurement context, here is what the closest managed-service alternatives cost on their public pricing pages (verified 2026-05-15; vendor pricing drifts, re-check before quoting):

| Engine | Hosting | $/M chars | Notes |
|---|---|---:|---|
| `anonde-patterns` | self-host (small commodity VM) | ~**$0.0005** | Patterns-only; runs on ~256 MB RAM. Amortised cost dominated by infra base. |
| `anonde-gliner` | self-host (~2 GB RAM VM) | ~**$0.001** | GLiNER PII baked into image. ~2 GB RAM is enough; CPU-only, runs on any commodity cloud VM. |
| `presidio` | self-host (open-source) | **$0** marginal | Microsoft Presidio. spaCy backend, English-focused. |
| `gliner-py` | self-host (open-source) | **$0** marginal | Same GLiNER PII model via Python sidecar. |
| Google Cloud DLP (inspect) | managed | ~$1 / GB ≈ **$1.00** | 1st GB/mo free; cheapest managed option by far. [pricing](https://cloud.google.com/sensitive-data-protection/pricing) |
| Azure AI Language PII | managed | ~$1 / 1k records ≈ **~$1.00** | Record = 1 000 chars. 5 000 records/mo free. [pricing](https://azure.microsoft.com/en-us/pricing/details/language/) |
| AWS Comprehend Medical (DetectPHI) | managed | $0.01 / 100 chars = **$100** | Tier 1; drops at volume. PHI-grade NER, English only. [pricing](https://aws.amazon.com/comprehend/medical/pricing/) |

> Self-hosting anonde is **roughly 1 000–100 000× cheaper per million characters** than the managed alternatives — and the data never leaves your network. The leak-rate and F1 numbers in the tables above are how you tell if the quality tradeoff is acceptable.

</details>

<details><summary>Caveats — training-data overlap</summary>

## Caveats — training-data overlap

A "win" on a corpus an engine was trained on (or trained near) is
weaker evidence than a win on a held-out one. Known overlaps in this
matrix:

- **`conll2003_en` × `presidio`** — Presidio's NER backend is spaCy's
  `en_core_web_lg`, trained on OntoNotes 5.0 with annotation
  guidelines derived from CoNLL-2003. The CoNLL-2003 EN test split is
  essentially home turf; Presidio's strict-F1 numbers here should be
  read as a *ceiling* on the model's accuracy, not as portable
  evidence that Presidio outperforms on EN PHI more broadly.
- **`germeval_14` / `wikiann_de` × `presidio`** — Same pattern in
  reverse: spaCy's `de_core_news_lg` is trained partly on TIGER and
  GermEval data. A high Presidio score here similarly reflects
  training-data adjacency.
- **`conll2003_en` / `wnut_17` × `anonde-gliner` (INT8 legacy)** — the
  INT8-quantised ONNX (`model_quint8.onnx`, ~196 MB) consistently leaks
  more PII than the FP32 export (`model.onnx`) loaded by production
  `anonde-gliner-fp32` and the Python `gliner-py` reference. Quantization
  bites on noisy English NER and on multilingual legal / clinical text;
  the gap between the `anonde-gliner-fp32` and `anonde-gliner` columns
  isolates the INT8-quantization cost from everything else. The
  `anonde-gliner-large` column probes the orthogonal question: does
  scaling to a 3-4x-larger GLiNER PII variant (still FP32) close the
  remaining cells where production loses to a baseline.

Held-out corpora with no known overlap for any of the four engines
listed in this matrix: `openmed` (GraSCCo PHI), `synth_clinical`,
`finance_de`, `legal_de`, `adversarial_de`, `ai4privacy_en`,
`pharmaconer_es`. Numbers there transfer most cleanly.

</details>

<details><summary>What does this mean? (glossary)</summary>

## What does this mean?

- **Leak rate** = the fraction of gold PHI spans no predicted span overlaps. The single most
  important number for a PII redactor: each leaked span is a real piece of PHI we'd have
  missed in production.
- **Severity-weighted leak rate** = the same metric, but each leaked span contributes its
  compliance-impact weight (defaults: 10 for IDs / IBAN, 5 for PERSON / contact / DOB / street,
  1 for LOCATION / ORG / PROFESSION / generic URL, 0 to drop entirely). Use this when
  comparing tools for a procurement / compliance decision — flat leak rate over-rewards
  catching the easy quasi-identifiers and under-counts missing the hard ones.
- **Strict F1** = exact start, end, and type match against gold. The CoNLL-style metric every
  NER paper publishes; useful for direct academic comparison. Less useful as a redaction
  metric, since a span that's 11 chars vs gold's 5 still successfully tokenises (the
  cleartext is gone either way) — but every leaked span is one we'd have shipped in prod.
  Per-entity-type strict F1 and partial / type-agnostic F1 views are in `results_matrix.csv`.
- **`–` cells** = engine not run on that corpus. Reasons: the matching spaCy / model assets
  weren't installed on the runner, or the corpus requires manual DUA registration (`ggponc_de`)
  or is loader-gated (`conll2003_de`).
- **Partial coverage** = an engine scored on a deterministic subsample, not the full corpus.
  `openai-pf` is ~80 s/doc on CPU, so it is benchmarked on the first N docs (sorted by id) —
  see the per-section "Partial coverage" footnote under the leak-rate grid. Its metrics are
  computed over only the docs it scored, so they are comparable in *kind* but not on the same
  doc population.
- **⚪ corpora** = precision-probe only (no span-level gold annotations). Useful for "does the
  engine over-redact ordinary prose?" checks, not for F1 / leak rate.
- **Domain / language grouping** = the report is organised by domain (clinical, legal, finance,
  logs, general PII, academic NER, adversarial) and language within each. The mapping lives in
  `bench/scoring/corpora.yaml`; a corpus missing from it renders under an `uncategorized` group.

</details>

---
*Generated by `bench/scoring/render_matrix.py` over 191 cells. Full per-entity-type breakdown in `results_matrix.csv`.*
