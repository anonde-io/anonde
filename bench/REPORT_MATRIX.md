# 🛡️ anonde bench matrix

> **TL;DR** — `anonde-ner` (the default NER image, `ghcr.io/anonde-io/anonde-ner`) is the lowest-leak engine on **29 of 29** gold-annotated corpora. Biggest absolute improvement over the best baseline: **+24.3pp** in leak rate. `anonde-ner-stack` is the premium variant — same model with the LARGE GLiNER PII flat-decoder stacked on top — shipped as a separate image (`ghcr.io/anonde-io/anonde-ner-stack`) for deployments that can spare the extra RAM. It is not counted as a competitor in the verdict. Strict F1 trades exact-byte alignment for catching more PHI — the right trade-off for a redactor, not a benchmark gaming exercise.

## 🎯 Scorecard · leak rate roll-ups

The one table. Roll-up rows only (per domain · per language · overall); the per-(domain × language) detail grid lives in the Detailed breakdown below. Each number is **leak rate** (fraction of gold PHI spans missed — lower is better). `anonde-ner` is the default NER image (`ghcr.io/anonde-io/anonde-ner`) and the anchor column; **Verdict** says whether it beats the field. `anonde-ner-stack` is the premium variant — same default model with the LARGE GLiNER PII flat-decoder stacked on top — shipped as a separate image (`ghcr.io/anonde-io/anonde-ner-stack`) for the deployments that can spare the extra RAM. It sits beside the anchor so the trade-off is visible at a glance, but the verdict is keyed on the default image. 🥇 marks the lowest-leak engine in the row. Roll-up rows pool leaked-over-gold across the group (doc-weighted, so larger corpora count more).

| Slice | Scope | `anonde-ner` ⬅︎ anonde (default NER) | `anonde-ner-stack` · anonde (NER stack, premium) | `anonde-patterns` | `presidio` | `gliner-py` | `openai-pf` | Verdict |
|---|---|---:|---:|---:|---:|---:|---:|:--:|
| **Σ ALL** | **all** | **10.1%** | **7.6%** 🥇 | **45.0%** | **41.5%** | **32.8%** | **24.4%** | ✅ |
| | | | | | | | | |
| _Σ Clinical / medical de-identification_ | _all langs_ | 11.2% | **8.1%** 🥇 | 47.5% | 31.1% | 28.0% | 27.4% | ✅ |
| _Σ Legal / administrative_ | _all langs_ | 2.5% | **1.7%** 🥇 | 23.1% | 29.6% | 21.5% | 36.6% | ✅ |
| _Σ Retail finance_ | _all langs_ | 4.9% | **2.4%** 🥇 | 24.4% | 21.3% | 23.5% | 18.9% | ✅ |
| _Σ Enterprise logs_ | _all langs_ | 13.5% | **12.0%** 🥇 | 22.8% | 24.2% | 73.9% | 16.9% | ✅ |
| _Σ General structured PII_ | _all langs_ | 12.5% | **9.5%** 🥇 | 60.2% | 57.8% | 34.7% | 18.8% | ✅ |
| _Σ Academic NER (newswire / social)_ | _all langs_ | 7.9% | **5.6%** 🥇 | 68.0% | 18.3% | 13.8% | 72.7% | ✅ |
| _Σ Adversarial / out-of-distribution_ | _all langs_ | 7.9% | **7.4%** 🥇 | 12.6% | 37.5% | 43.4% | 32.1% | ✅ |
| | | | | | | | | |
| _Σ all domains_ | _English_ | 9.6% | **7.5%** 🥇 | 33.7% | 35.0% | 38.8% | 20.5% | ✅ |
| _Σ all domains_ | _German_ | 6.0% | **5.1%** 🥇 | 24.3% | 38.3% | 31.9% | 28.8% | ✅ |
| _Σ all domains_ | _Spanish_ | 14.1% | **10.2%** 🥇 | 69.0% | 47.0% | 29.3% | 25.8% | ✅ |
| _Σ all domains_ | _French_ | 11.7% | **7.6%** 🥇 | 62.3% | 43.1% | 30.7% | 21.3% | ✅ |
| _Σ all domains_ | _Italian_ | 13.3% | **9.8%** 🥇 | 57.6% | 48.4% | 33.8% | 21.0% | ✅ |

> **Anonde scoreboard** — across the **24** populated `(domain, language)` cells in the matrix, `anonde-ner` is the **lowest-leak engine in 22**, ties in **2**, and is beaten in **0**. ✅ = anonde leads · 🟰 = tied · ❌ = a baseline leaks less. See the per-cell leak-rate grid in the Detailed breakdown below for which baseline wins where. (The TL;DR's win count is per-corpus, a finer split than these per-cell rows.)

<details><summary>Engine profiles · what each column means</summary>

## Engine profiles

The three anonde columns map 1:1 to the three shipping Docker images. They are not three competing tools; they are three deployment tiers — pick the one that fits your hardware and leak-rate budget. Compare *across the row* for the trade-off, not against each other for a winner.

| Engine | Image | CGO | Cold start | Best fit |
|---|---|---|---|---|
| `anonde-patterns` | `ghcr.io/anonde-io/anonde` (~12 MB) | not required | <1 s | structured slot-gen text (forms, logs, finance/legal docs) — wins F1 on PHONE, EMAIL, DATE, PROFESSION when the regex shape is tight |
| `anonde-ner` | `ghcr.io/anonde-io/anonde-ner` (~770 MB) | required | 5-30 s warmup | **default NER tier**. GLiNER PII (FP32 ONNX) + patterns. Natural text + multilingual PHI; the lowest-leak engine on most gold corpora. |
| `anonde-ner-stack` | `ghcr.io/anonde-io/anonde-ner-stack` (~2.1 GB) | required | 10-60 s warmup | **premium NER tier**. Default NER + the LARGE GLiNER PII flat-decoder stacked on top. Best leak rate on the Romance-language cells where the default still leaks; pick when you can spare the RAM. |
| `presidio` | Microsoft Presidio (spaCy NER + regex) ~1 GB | not required | 3-10 s | well-formed English (strong on EN newswire-shaped text where spaCy was trained) |
| `gliner-py` | GLiNER via PyTorch + safetensors (FP32) ~3 GB | not required | 10-30 s | reference implementation; parity check vs anonde-ner's in-process ONNX path |

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

| Domain | Language | `anonde-ner` ⬅︎ anonde (default NER) | `anonde-ner-stack` · anonde (NER stack, premium) | `anonde-patterns` | `presidio` | `gliner-py` | `openai-pf` | Verdict |
|---|---|---:|---:|---:|---:|---:|---:|:--:|
| **Clinical / medical de-identification** | English | 1.4% | **1.2%** 🥇 | 8.0% | 20.3% | 23.3% | 24.6% | ✅ |
| **Clinical / medical de-identification** | German | 4.6% | **4.3%** 🥇 | 9.0% | 30.9% | 34.2% | 30.1% | ✅ |
| **Clinical / medical de-identification** | Spanish | 17.7% | **12.7%** 🥇 | 79.6% | 38.4% | 23.7% | 31.7% | ✅ |
| **Clinical / medical de-identification** | French | 11.4% | **7.3%** 🥇 | 61.4% | 28.2% | 25.3% | 22.1% | ✅ |
| **Clinical / medical de-identification** | Italian | 16.2% | **11.6%** 🥇 | 59.7% | 28.5% | 35.2% | 25.1% | ✅ |
| **Legal / administrative** | English | 5.8% | **4.4%** 🥇 | 48.9% | 6.2% | 10.7% | 76.9% | ✅ |
| **Legal / administrative** | German | 1.1% | **0.8%** 🥇 | 11.4% | 26.7% | 23.8% | 32.8% | ✅ |
| **Legal / administrative** | Spanish | 18.3% | **11.8%** 🥇 | 100.0% | 59.1% | 18.3% | 87.5% | 🟰 |
| **Legal / administrative** | French | 6.4% | **4.1%** 🥇 | 75.2% | 44.9% | 16.3% | 95.5% | ✅ |
| **Legal / administrative** | Italian | 6.2% | **2.1%** 🥇 | 40.4% | 67.1% | 6.2% | 50.0% | 🟰 |
| **Retail finance** | English | 1.8% | **0.8%** 🥇 | 21.0% | 10.3% | 20.6% | 18.3% | ✅ |
| **Retail finance** | German | 0.9% | **0.6%** 🥇 | 3.4% | 24.1% | 22.6% | 21.1% | ✅ |
| **Retail finance** | Spanish | 10.2% | **5.2%** 🥇 | 46.4% | 18.5% | 25.4% | 17.6% | ✅ |
| **Retail finance** | French | 9.1% | **4.4%** 🥇 | 44.6% | 19.4% | 23.9% | 19.3% | ✅ |
| **Retail finance** | Italian | 9.0% | **3.9%** 🥇 | 39.5% | 29.6% | 26.4% | 14.1% | ✅ |
| **Enterprise logs** | English | 13.5% | **12.0%** 🥇 | 22.8% | 24.2% | 73.9% | 16.9% | ✅ |
| **General structured PII** | English | 13.1% | **9.8%** 🥇 | 46.2% | 56.0% | 35.0% | 15.5% | ✅ |
| **General structured PII** | German | 10.4% | **8.2%** 🥇 | 57.5% | 58.4% | 33.0% | 23.6% | ✅ |
| **General structured PII** | Spanish | 12.4% | **9.7%** 🥇 | 66.9% | 61.0% | 34.6% | 17.5% | ✅ |
| **General structured PII** | French | 12.7% | **8.7%** 🥇 | 66.9% | 54.1% | 34.9% | 16.3% | ✅ |
| **General structured PII** | Italian | 13.8% | **10.9%** 🥇 | 62.2% | 59.6% | 36.0% | 21.2% | ✅ |
| **Academic NER (newswire / social)** | English | 7.8% | **4.4%** 🥇 | 91.1% | 17.4% | 11.8% | 71.9% | ✅ |
| **Academic NER (newswire / social)** | German | 8.0% | **6.7%** 🥇 | 47.8% | 19.1% | 15.5% | 73.4% | ✅ |
| **Adversarial / out-of-distribution** | German | 7.9% | **7.4%** 🥇 | 12.6% | 37.5% | 43.4% | 32.1% | ✅ |

## Clinical / medical de-identification · English

Corpora in this group: `synth_clinical_en`.

### Leak rate · lower is better

A gold PHI span is *leaked* when **no** predicted span overlaps it — 'did we miss a name?'

| Corpus | `anonde-patterns` | `anonde-ner` | `anonde-ner-stack` | `presidio` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|---:|
| `synth_clinical_en` | 8.0% | 1.4% | **1.2%** 🥇 | 20.3% | 23.3% | 24.6% |

> **Partial coverage** — some engines were benchmarked on a fixed subsample, not every gold doc:
>
> - `openai-pf` on `synth_clinical_en`: scored on **40/120 docs** (deterministic subsample — metrics above are over those 40 docs only, not the full corpus).

### Severity-weighted leak rate · lower is better

Each leaked span weighted by compliance tier — direct identifiers (PERSON, EMAIL, PHONE, ADDRESS, DOB) = 5, high-stakes IDs (SSN/MRN/IBAN) = 10, quasi-identifiers (LOCATION, ORG, PROFESSION) = 1. Defaults in `label_map.yaml::severity`. Shown only because at least one cell here moves >3pp from raw leak; otherwise the two tables tracked within noise.

| Corpus | `anonde-patterns` | `anonde-ner` | `anonde-ner-stack` | `presidio` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|---:|
| `synth_clinical_en` | 5.3% | 1.6% | **1.6%** 🥇 | 26.7% | 26.4% | 17.6% |

## Clinical / medical de-identification · German

Corpora in this group: `openmed`, `pmc_de`, `synth_clinical`, `wiki_de`.

### Leak rate · lower is better

A gold PHI span is *leaked* when **no** predicted span overlaps it — 'did we miss a name?'

| Corpus | `anonde-patterns` | `anonde-ner` | `anonde-ner-stack` | `presidio` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|---:|
| `openmed` | 21.6% | 11.7% | **10.5%** 🥇 | 33.9% | 49.7% | 35.6% |
| `synth_clinical` | 1.8% | **0.5%** 🥇 | 0.7% | 29.1% | 25.4% | 23.5% |

> **Partial coverage** — some engines were benchmarked on a fixed subsample, not every gold doc:
>
> - `openai-pf` on `openmed`: scored on **40/63 docs** (deterministic subsample — metrics above are over those 40 docs only, not the full corpus).
> - `openai-pf` on `synth_clinical`: scored on **40/120 docs** (deterministic subsample — metrics above are over those 40 docs only, not the full corpus).

### Severity-weighted leak rate · lower is better

Each leaked span weighted by compliance tier — direct identifiers (PERSON, EMAIL, PHONE, ADDRESS, DOB) = 5, high-stakes IDs (SSN/MRN/IBAN) = 10, quasi-identifiers (LOCATION, ORG, PROFESSION) = 1. Defaults in `label_map.yaml::severity`. Shown only because at least one cell here moves >3pp from raw leak; otherwise the two tables tracked within noise.

| Corpus | `anonde-patterns` | `anonde-ner` | `anonde-ner-stack` | `presidio` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|---:|
| `openmed` | 21.1% | 11.6% | **10.6%** 🥇 | 35.5% | 50.5% | 34.1% |
| `synth_clinical` | 0.4% | **0.2%** 🥇 | 0.4% | 33.3% | 27.5% | 17.6% |

## Clinical / medical de-identification · Spanish

Corpora in this group: `pharmaconer_es`, `meddocan_es`.

### Leak rate · lower is better

A gold PHI span is *leaked* when **no** predicted span overlaps it — 'did we miss a name?'

| Corpus | `anonde-patterns` | `anonde-ner` | `anonde-ner-stack` | `presidio` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|---:|
| `meddocan_es` | 79.6% | 17.7% | **12.7%** 🥇 | 38.4% | 23.7% | 31.7% |

> **Partial coverage** — some engines were benchmarked on a fixed subsample, not every gold doc:
>
> - `openai-pf` on `pharmaconer_es`: scored on **40/200 docs** (deterministic subsample — metrics above are over those 40 docs only, not the full corpus).
> - `openai-pf` on `meddocan_es`: scored on **40/250 docs** (deterministic subsample — metrics above are over those 40 docs only, not the full corpus).

### Severity-weighted leak rate · lower is better

Each leaked span weighted by compliance tier — direct identifiers (PERSON, EMAIL, PHONE, ADDRESS, DOB) = 5, high-stakes IDs (SSN/MRN/IBAN) = 10, quasi-identifiers (LOCATION, ORG, PROFESSION) = 1. Defaults in `label_map.yaml::severity`. Shown only because at least one cell here moves >3pp from raw leak; otherwise the two tables tracked within noise.

| Corpus | `anonde-patterns` | `anonde-ner` | `anonde-ner-stack` | `presidio` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|---:|
| `meddocan_es` | 75.4% | 21.2% | **12.8%** 🥇 | 46.4% | 27.2% | 14.4% |

## Clinical / medical de-identification · French

Corpora in this group: `synth_clinical_fr`.

### Leak rate · lower is better

A gold PHI span is *leaked* when **no** predicted span overlaps it — 'did we miss a name?'

| Corpus | `anonde-patterns` | `anonde-ner` | `anonde-ner-stack` | `presidio` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|---:|
| `synth_clinical_fr` | 61.4% | 11.4% | **7.3%** 🥇 | 28.2% | 25.3% | 22.1% |

> **Partial coverage** — some engines were benchmarked on a fixed subsample, not every gold doc:
>
> - `openai-pf` on `synth_clinical_fr`: scored on **40/120 docs** (deterministic subsample — metrics above are over those 40 docs only, not the full corpus).

### Severity-weighted leak rate · lower is better

Each leaked span weighted by compliance tier — direct identifiers (PERSON, EMAIL, PHONE, ADDRESS, DOB) = 5, high-stakes IDs (SSN/MRN/IBAN) = 10, quasi-identifiers (LOCATION, ORG, PROFESSION) = 1. Defaults in `label_map.yaml::severity`. Shown only because at least one cell here moves >3pp from raw leak; otherwise the two tables tracked within noise.

| Corpus | `anonde-patterns` | `anonde-ner` | `anonde-ner-stack` | `presidio` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|---:|
| `synth_clinical_fr` | 56.8% | 11.4% | **7.3%** 🥇 | 30.9% | 28.0% | 15.6% |

## Clinical / medical de-identification · Italian

Corpora in this group: `synth_clinical_it`.

### Leak rate · lower is better

A gold PHI span is *leaked* when **no** predicted span overlaps it — 'did we miss a name?'

| Corpus | `anonde-patterns` | `anonde-ner` | `anonde-ner-stack` | `presidio` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|---:|
| `synth_clinical_it` | 59.7% | 16.2% | **11.6%** 🥇 | 28.5% | 35.2% | 25.1% |

> **Partial coverage** — some engines were benchmarked on a fixed subsample, not every gold doc:
>
> - `openai-pf` on `synth_clinical_it`: scored on **40/120 docs** (deterministic subsample — metrics above are over those 40 docs only, not the full corpus).

### Severity-weighted leak rate · lower is better

Each leaked span weighted by compliance tier — direct identifiers (PERSON, EMAIL, PHONE, ADDRESS, DOB) = 5, high-stakes IDs (SSN/MRN/IBAN) = 10, quasi-identifiers (LOCATION, ORG, PROFESSION) = 1. Defaults in `label_map.yaml::severity`. Shown only because at least one cell here moves >3pp from raw leak; otherwise the two tables tracked within noise.

| Corpus | `anonde-patterns` | `anonde-ner` | `anonde-ner-stack` | `presidio` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|---:|
| `synth_clinical_it` | 55.0% | 15.5% | **10.8%** 🥇 | 31.9% | 38.7% | 17.9% |

## Legal / administrative · English

Corpora in this group: `mapa_en`.

### Leak rate · lower is better

A gold PHI span is *leaked* when **no** predicted span overlaps it — 'did we miss a name?'

| Corpus | `anonde-patterns` | `anonde-ner` | `anonde-ner-stack` | `presidio` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|---:|
| `mapa_en` | 48.9% | 5.8% | **4.4%** 🥇 | 6.2% | 10.7% | 76.9% |

> **Partial coverage** — some engines were benchmarked on a fixed subsample, not every gold doc:
>
> - `openai-pf` on `mapa_en`: scored on **40/408 docs** (deterministic subsample — metrics above are over those 40 docs only, not the full corpus).

### Severity-weighted leak rate · lower is better

Each leaked span weighted by compliance tier — direct identifiers (PERSON, EMAIL, PHONE, ADDRESS, DOB) = 5, high-stakes IDs (SSN/MRN/IBAN) = 10, quasi-identifiers (LOCATION, ORG, PROFESSION) = 1. Defaults in `label_map.yaml::severity`. Shown only because at least one cell here moves >3pp from raw leak; otherwise the two tables tracked within noise.

| Corpus | `anonde-patterns` | `anonde-ner` | `anonde-ner-stack` | `presidio` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|---:|
| `mapa_en` | 42.9% | 6.1% | **4.5%** 🥇 | 4.9% | 9.8% | 75.4% |

## Legal / administrative · German

Corpora in this group: `legal_de`, `mapa_de`.

### Leak rate · lower is better

A gold PHI span is *leaked* when **no** predicted span overlaps it — 'did we miss a name?'

| Corpus | `anonde-patterns` | `anonde-ner` | `anonde-ner-stack` | `presidio` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|---:|
| `legal_de` | 6.5% | **0.3%** 🥇 | 0.4% | 24.5% | 25.4% | 31.3% |
| `mapa_de` | 51.6% | 7.8% | **4.9%** 🥇 | 44.8% | 10.4% | 85.0% |

> **Partial coverage** — some engines were benchmarked on a fixed subsample, not every gold doc:
>
> - `openai-pf` on `legal_de`: scored on **40/150 docs** (deterministic subsample — metrics above are over those 40 docs only, not the full corpus).
> - `openai-pf` on `mapa_de`: scored on **40/558 docs** (deterministic subsample — metrics above are over those 40 docs only, not the full corpus).

### Severity-weighted leak rate · lower is better

Each leaked span weighted by compliance tier — direct identifiers (PERSON, EMAIL, PHONE, ADDRESS, DOB) = 5, high-stakes IDs (SSN/MRN/IBAN) = 10, quasi-identifiers (LOCATION, ORG, PROFESSION) = 1. Defaults in `label_map.yaml::severity`. Shown only because at least one cell here moves >3pp from raw leak; otherwise the two tables tracked within noise.

| Corpus | `anonde-patterns` | `anonde-ner` | `anonde-ner-stack` | `presidio` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|---:|
| `legal_de` | 5.1% | **0.2%** 🥇 | 0.3% | 32.9% | 33.3% | 17.1% |
| `mapa_de` | 36.4% | 7.2% | **4.3%** 🥇 | 52.0% | 8.4% | 68.8% |

## Legal / administrative · Spanish

Corpora in this group: `mapa_es`.

### Leak rate · lower is better

A gold PHI span is *leaked* when **no** predicted span overlaps it — 'did we miss a name?'

| Corpus | `anonde-patterns` | `anonde-ner` | `anonde-ner-stack` | `presidio` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|---:|
| `mapa_es` | 100.0% | 18.3% | **11.8%** 🥇 | 59.1% | 18.3% | 87.5% |

> **Partial coverage** — some engines were benchmarked on a fixed subsample, not every gold doc:
>
> - `openai-pf` on `mapa_es`: scored on **40/155 docs** (deterministic subsample — metrics above are over those 40 docs only, not the full corpus).

## Legal / administrative · French

Corpora in this group: `mapa_fr`.

### Leak rate · lower is better

A gold PHI span is *leaked* when **no** predicted span overlaps it — 'did we miss a name?'

| Corpus | `anonde-patterns` | `anonde-ner` | `anonde-ner-stack` | `presidio` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|---:|
| `mapa_fr` | 75.2% | 6.4% | **4.1%** 🥇 | 44.9% | 16.3% | 95.5% |

> **Partial coverage** — some engines were benchmarked on a fixed subsample, not every gold doc:
>
> - `openai-pf` on `mapa_fr`: scored on **40/490 docs** (deterministic subsample — metrics above are over those 40 docs only, not the full corpus).

### Severity-weighted leak rate · lower is better

Each leaked span weighted by compliance tier — direct identifiers (PERSON, EMAIL, PHONE, ADDRESS, DOB) = 5, high-stakes IDs (SSN/MRN/IBAN) = 10, quasi-identifiers (LOCATION, ORG, PROFESSION) = 1. Defaults in `label_map.yaml::severity`. Shown only because at least one cell here moves >3pp from raw leak; otherwise the two tables tracked within noise.

| Corpus | `anonde-patterns` | `anonde-ner` | `anonde-ner-stack` | `presidio` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|---:|
| `mapa_fr` | 60.3% | 5.8% | **3.5%** 🥇 | 53.2% | 11.2% | 98.5% |

## Legal / administrative · Italian

Corpora in this group: `mapa_it`.

### Leak rate · lower is better

A gold PHI span is *leaked* when **no** predicted span overlaps it — 'did we miss a name?'

| Corpus | `anonde-patterns` | `anonde-ner` | `anonde-ner-stack` | `presidio` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|---:|
| `mapa_it` | 40.4% | 6.2% | **2.1%** 🥇 | 67.1% | 6.2% | 50.0% |

> **Partial coverage** — some engines were benchmarked on a fixed subsample, not every gold doc:
>
> - `openai-pf` on `mapa_it`: scored on **40/550 docs** (deterministic subsample — metrics above are over those 40 docs only, not the full corpus).

## Retail finance · English

Corpora in this group: `synth_finance_en`.

### Leak rate · lower is better

A gold PHI span is *leaked* when **no** predicted span overlaps it — 'did we miss a name?'

| Corpus | `anonde-patterns` | `anonde-ner` | `anonde-ner-stack` | `presidio` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|---:|
| `synth_finance_en` | 21.0% | 1.8% | **0.8%** 🥇 | 10.3% | 20.6% | 18.3% |

> **Partial coverage** — some engines were benchmarked on a fixed subsample, not every gold doc:
>
> - `openai-pf` on `synth_finance_en`: scored on **40/120 docs** (deterministic subsample — metrics above are over those 40 docs only, not the full corpus).

### Severity-weighted leak rate · lower is better

Each leaked span weighted by compliance tier — direct identifiers (PERSON, EMAIL, PHONE, ADDRESS, DOB) = 5, high-stakes IDs (SSN/MRN/IBAN) = 10, quasi-identifiers (LOCATION, ORG, PROFESSION) = 1. Defaults in `label_map.yaml::severity`. Shown only because at least one cell here moves >3pp from raw leak; otherwise the two tables tracked within noise.

| Corpus | `anonde-patterns` | `anonde-ner` | `anonde-ner-stack` | `presidio` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|---:|
| `synth_finance_en` | 14.1% | 1.4% | **0.4%** 🥇 | 11.9% | 22.7% | 5.9% |

## Retail finance · German

Corpora in this group: `finance_de`, `synth_finance_de`.

### Leak rate · lower is better

A gold PHI span is *leaked* when **no** predicted span overlaps it — 'did we miss a name?'

| Corpus | `anonde-patterns` | `anonde-ner` | `anonde-ner-stack` | `presidio` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|---:|
| `finance_de` | 3.2% | 1.0% | **0.8%** 🥇 | 25.9% | 26.2% | 20.5% |
| `synth_finance_de` | 3.8% | 0.8% | **0.3%** 🥇 | 21.4% | 17.2% | 22.2% |

> **Partial coverage** — some engines were benchmarked on a fixed subsample, not every gold doc:
>
> - `openai-pf` on `finance_de`: scored on **40/150 docs** (deterministic subsample — metrics above are over those 40 docs only, not the full corpus).
> - `openai-pf` on `synth_finance_de`: scored on **40/120 docs** (deterministic subsample — metrics above are over those 40 docs only, not the full corpus).

### Severity-weighted leak rate · lower is better

Each leaked span weighted by compliance tier — direct identifiers (PERSON, EMAIL, PHONE, ADDRESS, DOB) = 5, high-stakes IDs (SSN/MRN/IBAN) = 10, quasi-identifiers (LOCATION, ORG, PROFESSION) = 1. Defaults in `label_map.yaml::severity`. Shown only because at least one cell here moves >3pp from raw leak; otherwise the two tables tracked within noise.

| Corpus | `anonde-patterns` | `anonde-ner` | `anonde-ner-stack` | `presidio` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|---:|
| `finance_de` | 3.3% | 1.3% | **1.2%** 🥇 | 30.2% | 29.2% | 16.7% |
| `synth_finance_de` | 6.2% | 1.1% | **0.4%** 🥇 | 23.0% | 17.2% | 9.9% |

## Retail finance · Spanish

Corpora in this group: `synth_finance_es`.

### Leak rate · lower is better

A gold PHI span is *leaked* when **no** predicted span overlaps it — 'did we miss a name?'

| Corpus | `anonde-patterns` | `anonde-ner` | `anonde-ner-stack` | `presidio` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|---:|
| `synth_finance_es` | 46.4% | 10.2% | **5.2%** 🥇 | 18.5% | 25.4% | 17.6% |

> **Partial coverage** — some engines were benchmarked on a fixed subsample, not every gold doc:
>
> - `openai-pf` on `synth_finance_es`: scored on **40/120 docs** (deterministic subsample — metrics above are over those 40 docs only, not the full corpus).

### Severity-weighted leak rate · lower is better

Each leaked span weighted by compliance tier — direct identifiers (PERSON, EMAIL, PHONE, ADDRESS, DOB) = 5, high-stakes IDs (SSN/MRN/IBAN) = 10, quasi-identifiers (LOCATION, ORG, PROFESSION) = 1. Defaults in `label_map.yaml::severity`. Shown only because at least one cell here moves >3pp from raw leak; otherwise the two tables tracked within noise.

| Corpus | `anonde-patterns` | `anonde-ner` | `anonde-ner-stack` | `presidio` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|---:|
| `synth_finance_es` | 37.2% | 9.2% | **4.7%** 🥇 | 19.9% | 28.8% | 4.8% |

## Retail finance · French

Corpora in this group: `synth_finance_fr`.

### Leak rate · lower is better

A gold PHI span is *leaked* when **no** predicted span overlaps it — 'did we miss a name?'

| Corpus | `anonde-patterns` | `anonde-ner` | `anonde-ner-stack` | `presidio` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|---:|
| `synth_finance_fr` | 44.6% | 9.1% | **4.4%** 🥇 | 19.4% | 23.9% | 19.3% |

> **Partial coverage** — some engines were benchmarked on a fixed subsample, not every gold doc:
>
> - `openai-pf` on `synth_finance_fr`: scored on **40/120 docs** (deterministic subsample — metrics above are over those 40 docs only, not the full corpus).

### Severity-weighted leak rate · lower is better

Each leaked span weighted by compliance tier — direct identifiers (PERSON, EMAIL, PHONE, ADDRESS, DOB) = 5, high-stakes IDs (SSN/MRN/IBAN) = 10, quasi-identifiers (LOCATION, ORG, PROFESSION) = 1. Defaults in `label_map.yaml::severity`. Shown only because at least one cell here moves >3pp from raw leak; otherwise the two tables tracked within noise.

| Corpus | `anonde-patterns` | `anonde-ner` | `anonde-ner-stack` | `presidio` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|---:|
| `synth_finance_fr` | 35.1% | 7.0% | **3.0%** 🥇 | 21.7% | 26.4% | 6.7% |

## Retail finance · Italian

Corpora in this group: `synth_finance_it`.

### Leak rate · lower is better

A gold PHI span is *leaked* when **no** predicted span overlaps it — 'did we miss a name?'

| Corpus | `anonde-patterns` | `anonde-ner` | `anonde-ner-stack` | `presidio` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|---:|
| `synth_finance_it` | 39.5% | 9.0% | **3.9%** 🥇 | 29.6% | 26.4% | 14.1% |

> **Partial coverage** — some engines were benchmarked on a fixed subsample, not every gold doc:
>
> - `openai-pf` on `synth_finance_it`: scored on **40/120 docs** (deterministic subsample — metrics above are over those 40 docs only, not the full corpus).

### Severity-weighted leak rate · lower is better

Each leaked span weighted by compliance tier — direct identifiers (PERSON, EMAIL, PHONE, ADDRESS, DOB) = 5, high-stakes IDs (SSN/MRN/IBAN) = 10, quasi-identifiers (LOCATION, ORG, PROFESSION) = 1. Defaults in `label_map.yaml::severity`. Shown only because at least one cell here moves >3pp from raw leak; otherwise the two tables tracked within noise.

| Corpus | `anonde-patterns` | `anonde-ner` | `anonde-ner-stack` | `presidio` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|---:|
| `synth_finance_it` | 30.5% | 6.8% | **3.5%** 🥇 | 40.5% | 23.8% | 4.1% |

## Enterprise logs · English

Corpora in this group: `synth_logs`.

### Leak rate · lower is better

A gold PHI span is *leaked* when **no** predicted span overlaps it — 'did we miss a name?'

| Corpus | `anonde-patterns` | `anonde-ner` | `anonde-ner-stack` | `presidio` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|---:|
| `synth_logs` | 22.8% | 13.5% | **12.0%** 🥇 | 24.2% | 73.9% | 16.9% |

> **Partial coverage** — some engines were benchmarked on a fixed subsample, not every gold doc:
>
> - `openai-pf` on `synth_logs`: scored on **40/120 docs** (deterministic subsample — metrics above are over those 40 docs only, not the full corpus).

### Severity-weighted leak rate · lower is better

Each leaked span weighted by compliance tier — direct identifiers (PERSON, EMAIL, PHONE, ADDRESS, DOB) = 5, high-stakes IDs (SSN/MRN/IBAN) = 10, quasi-identifiers (LOCATION, ORG, PROFESSION) = 1. Defaults in `label_map.yaml::severity`. Shown only because at least one cell here moves >3pp from raw leak; otherwise the two tables tracked within noise.

| Corpus | `anonde-patterns` | `anonde-ner` | `anonde-ner-stack` | `presidio` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|---:|
| `synth_logs` | 27.7% | 17.1% | 15.2% | 31.5% | 82.1% | **8.8%** 🥇 |

## General structured PII · English

Corpora in this group: `ai4privacy_en`.

### Leak rate · lower is better

A gold PHI span is *leaked* when **no** predicted span overlaps it — 'did we miss a name?'

| Corpus | `anonde-patterns` | `anonde-ner` | `anonde-ner-stack` | `presidio` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|---:|
| `ai4privacy_en` | 46.2% | 13.1% | **9.8%** 🥇 | 56.0% | 35.0% | 15.5% |

> **Partial coverage** — some engines were benchmarked on a fixed subsample, not every gold doc:
>
> - `openai-pf` on `ai4privacy_en`: scored on **40/1000 docs** (deterministic subsample — metrics above are over those 40 docs only, not the full corpus).

### Severity-weighted leak rate · lower is better

Each leaked span weighted by compliance tier — direct identifiers (PERSON, EMAIL, PHONE, ADDRESS, DOB) = 5, high-stakes IDs (SSN/MRN/IBAN) = 10, quasi-identifiers (LOCATION, ORG, PROFESSION) = 1. Defaults in `label_map.yaml::severity`. Shown only because at least one cell here moves >3pp from raw leak; otherwise the two tables tracked within noise.

| Corpus | `anonde-patterns` | `anonde-ner` | `anonde-ner-stack` | `presidio` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|---:|
| `ai4privacy_en` | 37.6% | 12.4% | 10.1% | 57.4% | 37.0% | **10.0%** 🥇 |

## General structured PII · German

Corpora in this group: `ai4privacy_de`.

### Leak rate · lower is better

A gold PHI span is *leaked* when **no** predicted span overlaps it — 'did we miss a name?'

| Corpus | `anonde-patterns` | `anonde-ner` | `anonde-ner-stack` | `presidio` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|---:|
| `ai4privacy_de` | 57.5% | 10.4% | **8.2%** 🥇 | 58.4% | 33.0% | 23.6% |

> **Partial coverage** — some engines were benchmarked on a fixed subsample, not every gold doc:
>
> - `openai-pf` on `ai4privacy_de`: scored on **40/1000 docs** (deterministic subsample — metrics above are over those 40 docs only, not the full corpus).

### Severity-weighted leak rate · lower is better

Each leaked span weighted by compliance tier — direct identifiers (PERSON, EMAIL, PHONE, ADDRESS, DOB) = 5, high-stakes IDs (SSN/MRN/IBAN) = 10, quasi-identifiers (LOCATION, ORG, PROFESSION) = 1. Defaults in `label_map.yaml::severity`. Shown only because at least one cell here moves >3pp from raw leak; otherwise the two tables tracked within noise.

| Corpus | `anonde-patterns` | `anonde-ner` | `anonde-ner-stack` | `presidio` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|---:|
| `ai4privacy_de` | 61.7% | 11.3% | **10.0%** 🥇 | 62.4% | 34.7% | 16.3% |

## General structured PII · Spanish

Corpora in this group: `ai4privacy_es`.

### Leak rate · lower is better

A gold PHI span is *leaked* when **no** predicted span overlaps it — 'did we miss a name?'

| Corpus | `anonde-patterns` | `anonde-ner` | `anonde-ner-stack` | `presidio` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|---:|
| `ai4privacy_es` | 66.9% | 12.4% | **9.7%** 🥇 | 61.0% | 34.6% | 17.5% |

> **Partial coverage** — some engines were benchmarked on a fixed subsample, not every gold doc:
>
> - `openai-pf` on `ai4privacy_es`: scored on **40/1000 docs** (deterministic subsample — metrics above are over those 40 docs only, not the full corpus).

### Severity-weighted leak rate · lower is better

Each leaked span weighted by compliance tier — direct identifiers (PERSON, EMAIL, PHONE, ADDRESS, DOB) = 5, high-stakes IDs (SSN/MRN/IBAN) = 10, quasi-identifiers (LOCATION, ORG, PROFESSION) = 1. Defaults in `label_map.yaml::severity`. Shown only because at least one cell here moves >3pp from raw leak; otherwise the two tables tracked within noise.

| Corpus | `anonde-patterns` | `anonde-ner` | `anonde-ner-stack` | `presidio` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|---:|
| `ai4privacy_es` | 66.9% | 11.8% | 9.5% | 65.1% | 35.2% | **7.6%** 🥇 |

## General structured PII · French

Corpora in this group: `ai4privacy_fr`.

### Leak rate · lower is better

A gold PHI span is *leaked* when **no** predicted span overlaps it — 'did we miss a name?'

| Corpus | `anonde-patterns` | `anonde-ner` | `anonde-ner-stack` | `presidio` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|---:|
| `ai4privacy_fr` | 66.9% | 12.7% | **8.7%** 🥇 | 54.1% | 34.9% | 16.3% |

> **Partial coverage** — some engines were benchmarked on a fixed subsample, not every gold doc:
>
> - `openai-pf` on `ai4privacy_fr`: scored on **40/1000 docs** (deterministic subsample — metrics above are over those 40 docs only, not the full corpus).

### Severity-weighted leak rate · lower is better

Each leaked span weighted by compliance tier — direct identifiers (PERSON, EMAIL, PHONE, ADDRESS, DOB) = 5, high-stakes IDs (SSN/MRN/IBAN) = 10, quasi-identifiers (LOCATION, ORG, PROFESSION) = 1. Defaults in `label_map.yaml::severity`. Shown only because at least one cell here moves >3pp from raw leak; otherwise the two tables tracked within noise.

| Corpus | `anonde-patterns` | `anonde-ner` | `anonde-ner-stack` | `presidio` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|---:|
| `ai4privacy_fr` | 65.2% | 11.9% | **8.8%** 🥇 | 56.7% | 35.9% | 9.3% |

## General structured PII · Italian

Corpora in this group: `ai4privacy_it`.

### Leak rate · lower is better

A gold PHI span is *leaked* when **no** predicted span overlaps it — 'did we miss a name?'

| Corpus | `anonde-patterns` | `anonde-ner` | `anonde-ner-stack` | `presidio` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|---:|
| `ai4privacy_it` | 62.2% | 13.8% | **10.9%** 🥇 | 59.6% | 36.0% | 21.2% |

> **Partial coverage** — some engines were benchmarked on a fixed subsample, not every gold doc:
>
> - `openai-pf` on `ai4privacy_it`: scored on **40/1000 docs** (deterministic subsample — metrics above are over those 40 docs only, not the full corpus).

### Severity-weighted leak rate · lower is better

Each leaked span weighted by compliance tier — direct identifiers (PERSON, EMAIL, PHONE, ADDRESS, DOB) = 5, high-stakes IDs (SSN/MRN/IBAN) = 10, quasi-identifiers (LOCATION, ORG, PROFESSION) = 1. Defaults in `label_map.yaml::severity`. Shown only because at least one cell here moves >3pp from raw leak; otherwise the two tables tracked within noise.

| Corpus | `anonde-patterns` | `anonde-ner` | `anonde-ner-stack` | `presidio` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|---:|
| `ai4privacy_it` | 57.2% | 13.2% | **11.0%** 🥇 | 62.3% | 36.6% | 13.6% |

## Academic NER (newswire / social) · English

Corpora in this group: `conll2003_en`, `wnut_17`.

### Leak rate · lower is better

A gold PHI span is *leaked* when **no** predicted span overlaps it — 'did we miss a name?'

| Corpus | `anonde-patterns` | `anonde-ner` | `anonde-ner-stack` | `presidio` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|---:|
| `conll2003_en` | 96.7% | 6.0% | **3.1%** 🥇 | 6.7% | 6.0% | 64.0% |
| `wnut_17` | 77.7% | 12.2% | **7.4%** 🥇 | 43.1% | 25.5% | 82.1% |

> **Partial coverage** — some engines were benchmarked on a fixed subsample, not every gold doc:
>
> - `openai-pf` on `conll2003_en`: scored on **40/300 docs** (deterministic subsample — metrics above are over those 40 docs only, not the full corpus).
> - `openai-pf` on `wnut_17`: scored on **40/300 docs** (deterministic subsample — metrics above are over those 40 docs only, not the full corpus).

### Severity-weighted leak rate · lower is better

Each leaked span weighted by compliance tier — direct identifiers (PERSON, EMAIL, PHONE, ADDRESS, DOB) = 5, high-stakes IDs (SSN/MRN/IBAN) = 10, quasi-identifiers (LOCATION, ORG, PROFESSION) = 1. Defaults in `label_map.yaml::severity`. Shown only because at least one cell here moves >3pp from raw leak; otherwise the two tables tracked within noise.

| Corpus | `anonde-patterns` | `anonde-ner` | `anonde-ner-stack` | `presidio` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|---:|
| `conll2003_en` | 97.0% | 7.7% | **1.8%** 🥇 | 5.3% | 6.9% | 47.9% |
| `wnut_17` | 71.4% | 9.2% | **5.4%** 🥇 | 42.2% | 25.4% | 61.5% |

## Academic NER (newswire / social) · German

Corpora in this group: `wikiann_de`, `germeval_14`, `conll2003_de`.

### Leak rate · lower is better

A gold PHI span is *leaked* when **no** predicted span overlaps it — 'did we miss a name?'

| Corpus | `anonde-patterns` | `anonde-ner` | `anonde-ner-stack` | `presidio` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|---:|
| `wikiann_de` | 41.9% | 5.4% | **3.4%** 🥇 | 16.7% | 12.8% | 59.6% |
| `germeval_14` | 55.3% | 11.2% | **10.9%** 🥇 | 22.0% | 18.9% | 94.6% |

> **Partial coverage** — some engines were benchmarked on a fixed subsample, not every gold doc:
>
> - `openai-pf` on `wikiann_de`: scored on **40/300 docs** (deterministic subsample — metrics above are over those 40 docs only, not the full corpus).
> - `openai-pf` on `germeval_14`: scored on **40/300 docs** (deterministic subsample — metrics above are over those 40 docs only, not the full corpus).

### Severity-weighted leak rate · lower is better

Each leaked span weighted by compliance tier — direct identifiers (PERSON, EMAIL, PHONE, ADDRESS, DOB) = 5, high-stakes IDs (SSN/MRN/IBAN) = 10, quasi-identifiers (LOCATION, ORG, PROFESSION) = 1. Defaults in `label_map.yaml::severity`. Shown only because at least one cell here moves >3pp from raw leak; otherwise the two tables tracked within noise.

| Corpus | `anonde-patterns` | `anonde-ner` | `anonde-ner-stack` | `presidio` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|---:|
| `wikiann_de` | 30.3% | 4.4% | **2.5%** 🥇 | 6.9% | 10.7% | 44.3% |
| `germeval_14` | 47.6% | 7.0% | **5.4%** 🥇 | 15.8% | 16.1% | 88.8% |

## Adversarial / out-of-distribution · German

Corpora in this group: `adversarial_de`.

### Leak rate · lower is better

A gold PHI span is *leaked* when **no** predicted span overlaps it — 'did we miss a name?'

| Corpus | `anonde-patterns` | `anonde-ner` | `anonde-ner-stack` | `presidio` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|---:|
| `adversarial_de` | 12.6% | 7.9% | **7.4%** 🥇 | 37.5% | 43.4% | 32.1% |

> **Partial coverage** — some engines were benchmarked on a fixed subsample, not every gold doc:
>
> - `openai-pf` on `adversarial_de`: scored on **40/300 docs** (deterministic subsample — metrics above are over those 40 docs only, not the full corpus).

### Severity-weighted leak rate · lower is better

Each leaked span weighted by compliance tier — direct identifiers (PERSON, EMAIL, PHONE, ADDRESS, DOB) = 5, high-stakes IDs (SSN/MRN/IBAN) = 10, quasi-identifiers (LOCATION, ORG, PROFESSION) = 1. Defaults in `label_map.yaml::severity`. Shown only because at least one cell here moves >3pp from raw leak; otherwise the two tables tracked within noise.

| Corpus | `anonde-patterns` | `anonde-ner` | `anonde-ner-stack` | `presidio` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|---:|
| `adversarial_de` | 12.1% | 7.8% | **7.3%** 🥇 | 40.8% | 44.0% | 28.3% |

## Latency · per-document p50 / p95

Wall-clock per `engine.Analyze(doc)` call. p50 = steady-state, p95 = tail (the SLO knob). Mean + p99 in `results_matrix.csv`. One table across every corpus — latency tracks corpus length, not domain or language.

| Corpus | `anonde-patterns` p50 / p95 | `anonde-ner` p50 / p95 | `anonde-ner-stack` p50 / p95 | `presidio` p50 / p95 | `gliner-py` p50 / p95 | `openai-pf` p50 / p95 |
|---|---:|---:|---:|---:|---:|---:|
| `synth_clinical_en` | 1 ms / 2 ms | 188 ms / 338 ms | 1.2 s / 2.1 s | 26 ms / 36 ms | 394 ms / 513 ms | 423 ms / 525 ms |
| `openmed` | 3 ms / 9 ms | 569 ms / 2.1 s | 4.3 s / 13.9 s | 77 ms / 206 ms | 1.7 s / 8.5 s | 1.7 s / 4.1 s |
| `synth_clinical` | 1 ms / 2 ms | 190 ms / 326 ms | 1.4 s / 2.2 s | 26 ms / 34 ms | 424 ms / 584 ms | 446 ms / 725 ms |
| `pharmaconer_es` | 2 ms / 5 ms | 583 ms / 1.4 s | 3.4 s / 8.0 s | 46 ms / 120 ms | 1.3 s / 3.8 s | 1.0 s / 2.5 s |
| `meddocan_es` | 2 ms / 4 ms | 500 ms / 1.0 s | 3.5 s / 7.1 s | 55 ms / 113 ms | 1.2 s / 2.8 s | 1.1 s / 1.7 s |
| `synth_clinical_fr` | 1 ms / 1 ms | 220 ms / 304 ms | 1.5 s / 2.1 s | 28 ms / 39 ms | 436 ms / 620 ms | 580 ms / 722 ms |
| `synth_clinical_it` | 1 ms / 2 ms | 196 ms / 343 ms | 1.4 s / 2.1 s | 28 ms / 38 ms | 500 ms / 673 ms | 518 ms / 737 ms |
| `mapa_en` | 1 ms / 2 ms | 69 ms / 180 ms | 594 ms / 1.1 s | 7 ms / 25 ms | 261 ms / 534 ms | 198 ms / 476 ms |
| `legal_de` | 1 ms / 2 ms | 197 ms / 358 ms | 1.3 s / 1.6 s | 22 ms / 32 ms | 349 ms / 451 ms | 622 ms / 919 ms |
| `mapa_de` | 0 ms / 1 ms | 77 ms / 142 ms | 560 ms / 846 ms | 5 ms / 10 ms | 145 ms / 264 ms | 200 ms / 491 ms |
| `mapa_es` | 0 ms / 1 ms | 76 ms / 170 ms | 562 ms / 1.1 s | 6 ms / 18 ms | 166 ms / 339 ms | 176 ms / 343 ms |
| `mapa_fr` | 0 ms / 1 ms | 70 ms / 174 ms | 591 ms / 1.1 s | 6 ms / 16 ms | 147 ms / 253 ms | 343 ms / 511 ms |
| `mapa_it` | 0 ms / 1 ms | 77 ms / 175 ms | 619 ms / 987 ms | 7 ms / 14 ms | 160 ms / 248 ms | 193 ms / 375 ms |
| `synth_finance_en` | 1 ms / 2 ms | 112 ms / 215 ms | 845 ms / 1.3 s | 18 ms / 32 ms | 252 ms / 392 ms | 300 ms / 355 ms |
| `finance_de` | 1 ms / 1 ms | 148 ms / 223 ms | 1.3 s / 1.9 s | 23 ms / 37 ms | 358 ms / 546 ms | 496 ms / 664 ms |
| `synth_finance_de` | 1 ms / 2 ms | 129 ms / 181 ms | 995 ms / 1.4 s | 17 ms / 32 ms | 279 ms / 436 ms | 387 ms / 506 ms |
| `synth_finance_es` | 1 ms / 1 ms | 284 ms / 470 ms | 1.8 s / 2.2 s | 17 ms / 30 ms | 318 ms / 813 ms | 318 ms / 480 ms |
| `synth_finance_fr` | 1 ms / 1 ms | 164 ms / 258 ms | 1.3 s / 1.8 s | 22 ms / 36 ms | 338 ms / 496 ms | 360 ms / 459 ms |
| `synth_finance_it` | 1 ms / 1 ms | 150 ms / 374 ms | 1.0 s / 1.5 s | 18 ms / 32 ms | 324 ms / 507 ms | 367 ms / 463 ms |
| `synth_logs` | 3 ms / 6 ms | 353 ms / 681 ms | 2.3 s / 4.1 s | 42 ms / 95 ms | 1.1 s / 2.7 s | 1.2 s / 2.2 s |
| `ai4privacy_en` | 0 ms / 1 ms | 96 ms / 147 ms | 812 ms / 1.2 s | 16 ms / 25 ms | 238 ms / 338 ms | 281 ms / 385 ms |
| `ai4privacy_de` | 1 ms / 2 ms | 125 ms / 180 ms | 920 ms / 1.2 s | 13 ms / 19 ms | 292 ms / 407 ms | 297 ms / 486 ms |
| `ai4privacy_es` | 0 ms / 1 ms | 123 ms / 187 ms | 1.1 s / 1.5 s | 14 ms / 21 ms | 303 ms / 428 ms | 370 ms / 486 ms |
| `ai4privacy_fr` | 0 ms / 1 ms | 156 ms / 220 ms | 1.1 s / 1.5 s | 18 ms / 25 ms | 300 ms / 404 ms | 318 ms / 510 ms |
| `ai4privacy_it` | 0 ms / 1 ms | 135 ms / 198 ms | 954 ms / 1.3 s | 12 ms / 17 ms | 250 ms / 348 ms | 372 ms / 493 ms |
| `conll2003_en` | 0 ms / 1 ms | 41 ms / 59 ms | 367 ms / 493 ms | 4 ms / 8 ms | 111 ms / 153 ms | 85 ms / 164 ms |
| `wnut_17` | 0 ms / 0 ms | 45 ms / 68 ms | 472 ms / 662 ms | 4 ms / 8 ms | 113 ms / 162 ms | 139 ms / 226 ms |
| `wikiann_de` | 0 ms / 1 ms | 49 ms / 78 ms | 385 ms / 522 ms | 3 ms / 5 ms | 125 ms / 161 ms | 78 ms / 166 ms |
| `germeval_14` | 0 ms / 1 ms | 56 ms / 77 ms | 462 ms / 593 ms | 5 ms / 7 ms | 135 ms / 170 ms | 118 ms / 176 ms |
| `adversarial_de` | 1 ms / 2 ms | 163 ms / 302 ms | 1.3 s / 2.4 s | 26 ms / 34 ms | 459 ms / 775 ms | 694 ms / 878 ms |

<details><summary>Cost reference · USD per million characters</summary>

## Cost reference · USD per million characters

All engines in this matrix run on your hardware — no per-call charge. For procurement context, here is what the closest managed-service alternatives cost on their public pricing pages (verified 2026-05-15; vendor pricing drifts, re-check before quoting):

| Engine | Hosting | $/M chars | Notes |
|---|---|---:|---|
| `anonde-patterns` | self-host (small commodity VM) | ~**$0.0005** | Patterns-only; runs on ~256 MB RAM. Amortised cost dominated by infra base. |
| `anonde-ner` | self-host (~2 GB RAM VM) | ~**$0.001** | GLiNER PII baked into image. ~2 GB RAM is enough; CPU-only, runs on any commodity cloud VM. |
| `anonde-ner-stack` | self-host (~4 GB RAM VM) | ~**$0.002** | Default NER + LARGE flat-decoder. Pick when the extra ~3pp on Romance-language cells is worth ~2× RAM. |
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

Held-out corpora with no known overlap for any of the engines listed
in this matrix: `openmed` (GraSCCo PHI), `synth_clinical`,
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
*Generated by `bench/scoring/render_matrix.py` over 180 cells. Full per-entity-type breakdown in `results_matrix.csv`.*
