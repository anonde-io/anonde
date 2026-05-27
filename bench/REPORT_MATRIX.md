# 🛡️ anonde bench matrix

> **TL;DR** — `anonde-ner` (the default NER image, `ghcr.io/anonde-io/anonde-ner`) is the lowest-leak engine on **25 of 29** gold-annotated corpora. Biggest absolute improvement over the best baseline: **+24.1pp** in leak rate. `anonde-ner-stack` is the premium variant — same model with the LARGE GLiNER PII flat-decoder stacked on top — shipped as a separate image (`ghcr.io/anonde-io/anonde-ner-stack`) for deployments that can spare the extra RAM. It is not counted as a competitor in the verdict. Strict F1 trades exact-byte alignment for catching more PHI — the right trade-off for a redactor, not a benchmark gaming exercise.

## 🎯 Scorecard · leak rate roll-ups

The one table. Roll-up rows only (per domain · per language · overall); the per-(domain × language) detail grid lives in the Detailed breakdown below. Each number is **leak rate** (fraction of gold PHI spans missed — lower is better). `anonde-ner` is the default NER image (`ghcr.io/anonde-io/anonde-ner`) and the anchor column; **Verdict** says whether it beats the field. `anonde-ner-stack` is the premium variant — same default model with the LARGE GLiNER PII flat-decoder stacked on top — shipped as a separate image (`ghcr.io/anonde-io/anonde-ner-stack`) for the deployments that can spare the extra RAM. It sits beside the anchor so the trade-off is visible at a glance, but the verdict is keyed on the default image. 🥇 marks the lowest-leak engine in the row. Roll-up rows pool leaked-over-gold across the group (doc-weighted, so larger corpora count more).

| Slice | Scope | `anonde-ner` ⬅︎ anonde (default NER) | `anonde-patterns` | `anonde-ner-stack` | `presidio` | `gliner-py` | `openai-pf` | Verdict |
|---|---|---:|---:|---:|---:|---:|---:|:--:|
| **Σ ALL** | **all** | **12.8%** | **45.0%** | **8.9%** 🥇 | **41.5%** | **32.9%** | **24.4%** | ✅ |
| | | | | | | | | |
| _Σ Clinical / medical de-identification_ | _all langs_ | 13.1% | 47.5% | **8.7%** 🥇 | 31.1% | 28.0% | 27.4% | ✅ |
| _Σ Legal / administrative_ | _all langs_ | 4.0% | 23.1% | **2.3%** 🥇 | 29.6% | 21.5% | 36.6% | ✅ |
| _Σ Retail finance_ | _all langs_ | 6.7% | 24.4% | **2.6%** 🥇 | 21.3% | 23.5% | 18.9% | ✅ |
| _Σ Enterprise logs_ | _all langs_ | 13.1% | 22.8% | **11.5%** 🥇 | 24.2% | 73.9% | 16.9% | ✅ |
| _Σ General structured PII_ | _all langs_ | 16.7% | 60.2% | **12.0%** 🥇 | 57.8% | 34.7% | 18.8% | ✅ |
| _Σ Academic NER (newswire / social)_ | _all langs_ | 10.0% | 68.0% | **5.7%** 🥇 | 18.3% | 13.8% | 72.7% | ✅ |
| _Σ Adversarial / out-of-distribution_ | _all langs_ | 8.3% | 12.6% | **7.7%** 🥇 | 37.4% | 43.5% | 32.1% | ✅ |
| | | | | | | | | |
| _Σ all domains_ | _English_ | 11.5% | 33.7% | **8.0%** 🥇 | 35.0% | 38.8% | 20.5% | ✅ |
| _Σ all domains_ | _German_ | 7.3% | 24.3% | **5.8%** 🥇 | 38.3% | 31.9% | 28.8% | ✅ |
| _Σ all domains_ | _Spanish_ | 18.1% | 69.0% | **12.0%** 🥇 | 47.0% | 29.3% | 25.8% | ✅ |
| _Σ all domains_ | _French_ | 15.1% | 62.3% | **9.1%** 🥇 | 43.1% | 30.7% | 21.3% | ✅ |
| _Σ all domains_ | _Italian_ | 17.9% | 57.6% | **12.6%** 🥇 | 48.4% | 33.8% | 21.0% | ✅ |

> **Anonde scoreboard** — across the **24** populated `(domain, language)` cells in the matrix, `anonde-ner` is the **lowest-leak engine in 19**, ties in **1**, and is beaten in **4**. ✅ = anonde leads · 🟰 = tied · ❌ = a baseline leaks less. See the per-cell leak-rate grid in the Detailed breakdown below for which baseline wins where. (The TL;DR's win count is per-corpus, a finer split than these per-cell rows.)

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

| Domain | Language | `anonde-ner` ⬅︎ anonde (default NER) | `anonde-patterns` | `anonde-ner-stack` | `presidio` | `gliner-py` | `openai-pf` | Verdict |
|---|---|---:|---:|---:|---:|---:|---:|:--:|
| **Clinical / medical de-identification** | English | 3.3% | 8.0% | **0.9%** 🥇 | 20.3% | 23.3% | 24.6% | ✅ |
| **Clinical / medical de-identification** | German | 5.2% | 9.0% | **4.6%** 🥇 | 30.9% | 34.2% | 30.1% | ✅ |
| **Clinical / medical de-identification** | Spanish | 21.0% | 79.6% | **13.9%** 🥇 | 38.4% | 23.7% | 31.7% | ✅ |
| **Clinical / medical de-identification** | French | 11.9% | 61.4% | **7.1%** 🥇 | 28.2% | 25.3% | 22.1% | ✅ |
| **Clinical / medical de-identification** | Italian | 18.2% | 59.7% | **12.9%** 🥇 | 28.5% | 35.2% | 25.1% | ✅ |
| **Legal / administrative** | English | 9.3% | 48.9% | **6.2%** 🥇 | **6.2%** 🥇 | 10.7% | 76.9% | ❌ |
| **Legal / administrative** | German | 1.3% | 11.4% | **1.0%** 🥇 | 26.7% | 23.8% | 32.8% | ✅ |
| **Legal / administrative** | Spanish | 32.3% | 100.0% | **17.2%** 🥇 | 59.1% | 18.3% | 87.5% | ❌ |
| **Legal / administrative** | French | 14.9% | 75.2% | **7.3%** 🥇 | 44.9% | 16.3% | 95.5% | ✅ |
| **Legal / administrative** | Italian | 6.2% | 40.4% | **1.4%** 🥇 | 67.1% | 6.2% | 50.0% | 🟰 |
| **Retail finance** | English | 5.9% | 21.0% | **1.2%** 🥇 | 10.3% | 20.6% | 18.3% | ✅ |
| **Retail finance** | German | 0.9% | 3.4% | **0.6%** 🥇 | 24.1% | 22.6% | 21.1% | ✅ |
| **Retail finance** | Spanish | 12.6% | 46.4% | **5.5%** 🥇 | 18.5% | 25.4% | 17.6% | ✅ |
| **Retail finance** | French | 11.0% | 44.6% | **4.3%** 🥇 | 19.4% | 23.9% | 19.3% | ✅ |
| **Retail finance** | Italian | 12.2% | 39.5% | **4.3%** 🥇 | 29.6% | 26.4% | 14.1% | ✅ |
| **Enterprise logs** | English | 13.1% | 22.8% | **11.5%** 🥇 | 24.2% | 73.9% | 16.9% | ✅ |
| **General structured PII** | English | 15.1% | 46.2% | **11.2%** 🥇 | 56.0% | 35.0% | 15.5% | ✅ |
| **General structured PII** | German | 14.2% | 57.5% | **10.4%** 🥇 | 58.4% | 33.0% | 23.6% | ✅ |
| **General structured PII** | Spanish | 17.3% | 66.9% | **12.4%** 🥇 | 61.0% | 34.6% | 17.5% | ✅ |
| **General structured PII** | French | 17.2% | 66.9% | **11.1%** 🥇 | 54.1% | 34.9% | 16.3% | ❌ |
| **General structured PII** | Italian | 19.6% | 62.2% | **15.0%** 🥇 | 59.6% | 36.0% | 21.2% | ✅ |
| **Academic NER (newswire / social)** | English | 12.7% | 91.1% | **4.9%** 🥇 | 17.4% | 11.8% | 71.9% | ❌ |
| **Academic NER (newswire / social)** | German | 7.6% | 47.8% | **6.5%** 🥇 | 19.1% | 15.5% | 73.4% | ✅ |
| **Adversarial / out-of-distribution** | German | 8.3% | 12.6% | **7.7%** 🥇 | 37.4% | 43.5% | 32.1% | ✅ |

## Clinical / medical de-identification · English

Corpora in this group: `synth_clinical_en`.

### Leak rate · lower is better

A gold PHI span is *leaked* when **no** predicted span overlaps it — 'did we miss a name?'

| Corpus | `anonde-patterns` | `anonde-ner` | `anonde-ner-stack` | `presidio` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|---:|
| `synth_clinical_en` | 8.0% | 3.3% | **0.9%** 🥇 | 20.3% | 23.3% | 24.6% |

> **Partial coverage** — some engines were benchmarked on a fixed subsample, not every gold doc:
>
> - `openai-pf` on `synth_clinical_en`: scored on **40/120 docs** (deterministic subsample — metrics above are over those 40 docs only, not the full corpus).

### Severity-weighted leak rate · lower is better

Each leaked span weighted by compliance tier — direct identifiers (PERSON, EMAIL, PHONE, ADDRESS, DOB) = 5, high-stakes IDs (SSN/MRN/IBAN) = 10, quasi-identifiers (LOCATION, ORG, PROFESSION) = 1. Defaults in `label_map.yaml::severity`. Shown only because at least one cell here moves >3pp from raw leak; otherwise the two tables tracked within noise.

| Corpus | `anonde-patterns` | `anonde-ner` | `anonde-ner-stack` | `presidio` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|---:|
| `synth_clinical_en` | 5.3% | 1.4% | **0.6%** 🥇 | 26.7% | 26.4% | 17.6% |

## Clinical / medical de-identification · German

Corpora in this group: `openmed`, `pmc_de`, `synth_clinical`, `wiki_de`.

### Leak rate · lower is better

A gold PHI span is *leaked* when **no** predicted span overlaps it — 'did we miss a name?'

| Corpus | `anonde-patterns` | `anonde-ner` | `anonde-ner-stack` | `presidio` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|---:|
| `openmed` | 21.6% | 13.3% | **11.7%** 🥇 | 33.9% | 49.7% | 35.6% |
| `synth_clinical` | 1.8% | 0.6% | **0.5%** 🥇 | 29.1% | 25.4% | 23.5% |

> **Partial coverage** — some engines were benchmarked on a fixed subsample, not every gold doc:
>
> - `openai-pf` on `openmed`: scored on **40/63 docs** (deterministic subsample — metrics above are over those 40 docs only, not the full corpus).
> - `openai-pf` on `synth_clinical`: scored on **40/120 docs** (deterministic subsample — metrics above are over those 40 docs only, not the full corpus).

### Severity-weighted leak rate · lower is better

Each leaked span weighted by compliance tier — direct identifiers (PERSON, EMAIL, PHONE, ADDRESS, DOB) = 5, high-stakes IDs (SSN/MRN/IBAN) = 10, quasi-identifiers (LOCATION, ORG, PROFESSION) = 1. Defaults in `label_map.yaml::severity`. Shown only because at least one cell here moves >3pp from raw leak; otherwise the two tables tracked within noise.

| Corpus | `anonde-patterns` | `anonde-ner` | `anonde-ner-stack` | `presidio` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|---:|
| `openmed` | 21.1% | 12.9% | **11.4%** 🥇 | 35.5% | 50.5% | 34.1% |
| `synth_clinical` | 0.4% | 0.2% | **0.2%** 🥇 | 33.3% | 27.5% | 17.6% |

## Clinical / medical de-identification · Spanish

Corpora in this group: `pharmaconer_es`, `meddocan_es`.

### Leak rate · lower is better

A gold PHI span is *leaked* when **no** predicted span overlaps it — 'did we miss a name?'

| Corpus | `anonde-patterns` | `anonde-ner` | `anonde-ner-stack` | `presidio` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|---:|
| `meddocan_es` | 79.6% | 21.0% | **13.9%** 🥇 | 38.4% | 23.7% | 31.7% |

> **Partial coverage** — some engines were benchmarked on a fixed subsample, not every gold doc:
>
> - `openai-pf` on `pharmaconer_es`: scored on **40/200 docs** (deterministic subsample — metrics above are over those 40 docs only, not the full corpus).
> - `openai-pf` on `meddocan_es`: scored on **40/250 docs** (deterministic subsample — metrics above are over those 40 docs only, not the full corpus).

### Severity-weighted leak rate · lower is better

Each leaked span weighted by compliance tier — direct identifiers (PERSON, EMAIL, PHONE, ADDRESS, DOB) = 5, high-stakes IDs (SSN/MRN/IBAN) = 10, quasi-identifiers (LOCATION, ORG, PROFESSION) = 1. Defaults in `label_map.yaml::severity`. Shown only because at least one cell here moves >3pp from raw leak; otherwise the two tables tracked within noise.

| Corpus | `anonde-patterns` | `anonde-ner` | `anonde-ner-stack` | `presidio` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|---:|
| `meddocan_es` | 75.4% | 23.4% | **13.6%** 🥇 | 46.4% | 27.2% | 14.4% |

## Clinical / medical de-identification · French

Corpora in this group: `synth_clinical_fr`.

### Leak rate · lower is better

A gold PHI span is *leaked* when **no** predicted span overlaps it — 'did we miss a name?'

| Corpus | `anonde-patterns` | `anonde-ner` | `anonde-ner-stack` | `presidio` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|---:|
| `synth_clinical_fr` | 61.4% | 11.9% | **7.1%** 🥇 | 28.2% | 25.3% | 22.1% |

> **Partial coverage** — some engines were benchmarked on a fixed subsample, not every gold doc:
>
> - `openai-pf` on `synth_clinical_fr`: scored on **40/120 docs** (deterministic subsample — metrics above are over those 40 docs only, not the full corpus).

### Severity-weighted leak rate · lower is better

Each leaked span weighted by compliance tier — direct identifiers (PERSON, EMAIL, PHONE, ADDRESS, DOB) = 5, high-stakes IDs (SSN/MRN/IBAN) = 10, quasi-identifiers (LOCATION, ORG, PROFESSION) = 1. Defaults in `label_map.yaml::severity`. Shown only because at least one cell here moves >3pp from raw leak; otherwise the two tables tracked within noise.

| Corpus | `anonde-patterns` | `anonde-ner` | `anonde-ner-stack` | `presidio` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|---:|
| `synth_clinical_fr` | 56.8% | 11.6% | **6.8%** 🥇 | 30.9% | 28.0% | 15.6% |

## Clinical / medical de-identification · Italian

Corpora in this group: `synth_clinical_it`.

### Leak rate · lower is better

A gold PHI span is *leaked* when **no** predicted span overlaps it — 'did we miss a name?'

| Corpus | `anonde-patterns` | `anonde-ner` | `anonde-ner-stack` | `presidio` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|---:|
| `synth_clinical_it` | 59.7% | 18.2% | **12.9%** 🥇 | 28.5% | 35.2% | 25.1% |

> **Partial coverage** — some engines were benchmarked on a fixed subsample, not every gold doc:
>
> - `openai-pf` on `synth_clinical_it`: scored on **40/120 docs** (deterministic subsample — metrics above are over those 40 docs only, not the full corpus).

### Severity-weighted leak rate · lower is better

Each leaked span weighted by compliance tier — direct identifiers (PERSON, EMAIL, PHONE, ADDRESS, DOB) = 5, high-stakes IDs (SSN/MRN/IBAN) = 10, quasi-identifiers (LOCATION, ORG, PROFESSION) = 1. Defaults in `label_map.yaml::severity`. Shown only because at least one cell here moves >3pp from raw leak; otherwise the two tables tracked within noise.

| Corpus | `anonde-patterns` | `anonde-ner` | `anonde-ner-stack` | `presidio` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|---:|
| `synth_clinical_it` | 55.0% | 17.5% | **12.1%** 🥇 | 31.9% | 38.7% | 17.9% |

## Legal / administrative · English

Corpora in this group: `mapa_en`.

### Leak rate · lower is better

A gold PHI span is *leaked* when **no** predicted span overlaps it — 'did we miss a name?'

| Corpus | `anonde-patterns` | `anonde-ner` | `anonde-ner-stack` | `presidio` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|---:|
| `mapa_en` | 48.9% | 9.3% | **6.2%** 🥇 | **6.2%** 🥇 | 10.7% | 76.9% |

> **Partial coverage** — some engines were benchmarked on a fixed subsample, not every gold doc:
>
> - `openai-pf` on `mapa_en`: scored on **40/408 docs** (deterministic subsample — metrics above are over those 40 docs only, not the full corpus).

### Severity-weighted leak rate · lower is better

Each leaked span weighted by compliance tier — direct identifiers (PERSON, EMAIL, PHONE, ADDRESS, DOB) = 5, high-stakes IDs (SSN/MRN/IBAN) = 10, quasi-identifiers (LOCATION, ORG, PROFESSION) = 1. Defaults in `label_map.yaml::severity`. Shown only because at least one cell here moves >3pp from raw leak; otherwise the two tables tracked within noise.

| Corpus | `anonde-patterns` | `anonde-ner` | `anonde-ner-stack` | `presidio` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|---:|
| `mapa_en` | 42.9% | 9.1% | 5.3% | **4.9%** 🥇 | 9.8% | 75.4% |

## Legal / administrative · German

Corpora in this group: `legal_de`, `mapa_de`.

### Leak rate · lower is better

A gold PHI span is *leaked* when **no** predicted span overlaps it — 'did we miss a name?'

| Corpus | `anonde-patterns` | `anonde-ner` | `anonde-ner-stack` | `presidio` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|---:|
| `legal_de` | 6.5% | **0.4%** 🥇 | 0.5% | 24.5% | 25.4% | 31.3% |
| `mapa_de` | 51.6% | 8.1% | **5.2%** 🥇 | 44.8% | 10.4% | 85.0% |

> **Partial coverage** — some engines were benchmarked on a fixed subsample, not every gold doc:
>
> - `openai-pf` on `legal_de`: scored on **40/150 docs** (deterministic subsample — metrics above are over those 40 docs only, not the full corpus).
> - `openai-pf` on `mapa_de`: scored on **40/558 docs** (deterministic subsample — metrics above are over those 40 docs only, not the full corpus).

### Severity-weighted leak rate · lower is better

Each leaked span weighted by compliance tier — direct identifiers (PERSON, EMAIL, PHONE, ADDRESS, DOB) = 5, high-stakes IDs (SSN/MRN/IBAN) = 10, quasi-identifiers (LOCATION, ORG, PROFESSION) = 1. Defaults in `label_map.yaml::severity`. Shown only because at least one cell here moves >3pp from raw leak; otherwise the two tables tracked within noise.

| Corpus | `anonde-patterns` | `anonde-ner` | `anonde-ner-stack` | `presidio` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|---:|
| `legal_de` | 5.1% | 0.5% | 0.4% | 32.9% | 33.3% | 17.1% |
| `mapa_de` | 36.4% | 7.7% | **4.8%** 🥇 | 52.0% | 8.4% | 68.8% |

## Legal / administrative · Spanish

Corpora in this group: `mapa_es`.

### Leak rate · lower is better

A gold PHI span is *leaked* when **no** predicted span overlaps it — 'did we miss a name?'

| Corpus | `anonde-patterns` | `anonde-ner` | `anonde-ner-stack` | `presidio` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|---:|
| `mapa_es` | 100.0% | 32.3% | **17.2%** 🥇 | 59.1% | 18.3% | 87.5% |

> **Partial coverage** — some engines were benchmarked on a fixed subsample, not every gold doc:
>
> - `openai-pf` on `mapa_es`: scored on **40/155 docs** (deterministic subsample — metrics above are over those 40 docs only, not the full corpus).

## Legal / administrative · French

Corpora in this group: `mapa_fr`.

### Leak rate · lower is better

A gold PHI span is *leaked* when **no** predicted span overlaps it — 'did we miss a name?'

| Corpus | `anonde-patterns` | `anonde-ner` | `anonde-ner-stack` | `presidio` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|---:|
| `mapa_fr` | 75.2% | 14.9% | **7.3%** 🥇 | 44.9% | 16.3% | 95.5% |

> **Partial coverage** — some engines were benchmarked on a fixed subsample, not every gold doc:
>
> - `openai-pf` on `mapa_fr`: scored on **40/490 docs** (deterministic subsample — metrics above are over those 40 docs only, not the full corpus).

### Severity-weighted leak rate · lower is better

Each leaked span weighted by compliance tier — direct identifiers (PERSON, EMAIL, PHONE, ADDRESS, DOB) = 5, high-stakes IDs (SSN/MRN/IBAN) = 10, quasi-identifiers (LOCATION, ORG, PROFESSION) = 1. Defaults in `label_map.yaml::severity`. Shown only because at least one cell here moves >3pp from raw leak; otherwise the two tables tracked within noise.

| Corpus | `anonde-patterns` | `anonde-ner` | `anonde-ner-stack` | `presidio` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|---:|
| `mapa_fr` | 60.3% | 11.9% | **6.1%** 🥇 | 53.2% | 11.2% | 98.5% |

## Legal / administrative · Italian

Corpora in this group: `mapa_it`.

### Leak rate · lower is better

A gold PHI span is *leaked* when **no** predicted span overlaps it — 'did we miss a name?'

| Corpus | `anonde-patterns` | `anonde-ner` | `anonde-ner-stack` | `presidio` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|---:|
| `mapa_it` | 40.4% | 6.2% | **1.4%** 🥇 | 67.1% | 6.2% | 50.0% |

> **Partial coverage** — some engines were benchmarked on a fixed subsample, not every gold doc:
>
> - `openai-pf` on `mapa_it`: scored on **40/550 docs** (deterministic subsample — metrics above are over those 40 docs only, not the full corpus).

## Retail finance · English

Corpora in this group: `synth_finance_en`.

### Leak rate · lower is better

A gold PHI span is *leaked* when **no** predicted span overlaps it — 'did we miss a name?'

| Corpus | `anonde-patterns` | `anonde-ner` | `anonde-ner-stack` | `presidio` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|---:|
| `synth_finance_en` | 21.0% | 5.9% | **1.2%** 🥇 | 10.3% | 20.6% | 18.3% |

> **Partial coverage** — some engines were benchmarked on a fixed subsample, not every gold doc:
>
> - `openai-pf` on `synth_finance_en`: scored on **40/120 docs** (deterministic subsample — metrics above are over those 40 docs only, not the full corpus).

### Severity-weighted leak rate · lower is better

Each leaked span weighted by compliance tier — direct identifiers (PERSON, EMAIL, PHONE, ADDRESS, DOB) = 5, high-stakes IDs (SSN/MRN/IBAN) = 10, quasi-identifiers (LOCATION, ORG, PROFESSION) = 1. Defaults in `label_map.yaml::severity`. Shown only because at least one cell here moves >3pp from raw leak; otherwise the two tables tracked within noise.

| Corpus | `anonde-patterns` | `anonde-ner` | `anonde-ner-stack` | `presidio` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|---:|
| `synth_finance_en` | 14.1% | 4.9% | **0.8%** 🥇 | 11.9% | 22.7% | 5.9% |

## Retail finance · German

Corpora in this group: `finance_de`, `synth_finance_de`.

### Leak rate · lower is better

A gold PHI span is *leaked* when **no** predicted span overlaps it — 'did we miss a name?'

| Corpus | `anonde-patterns` | `anonde-ner` | `anonde-ner-stack` | `presidio` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|---:|
| `finance_de` | 3.2% | 1.1% | **0.9%** 🥇 | 25.9% | 26.2% | 20.5% |
| `synth_finance_de` | 3.8% | 0.6% | **0.2%** 🥇 | 21.4% | 17.2% | 22.2% |

> **Partial coverage** — some engines were benchmarked on a fixed subsample, not every gold doc:
>
> - `openai-pf` on `finance_de`: scored on **40/150 docs** (deterministic subsample — metrics above are over those 40 docs only, not the full corpus).
> - `openai-pf` on `synth_finance_de`: scored on **40/120 docs** (deterministic subsample — metrics above are over those 40 docs only, not the full corpus).

### Severity-weighted leak rate · lower is better

Each leaked span weighted by compliance tier — direct identifiers (PERSON, EMAIL, PHONE, ADDRESS, DOB) = 5, high-stakes IDs (SSN/MRN/IBAN) = 10, quasi-identifiers (LOCATION, ORG, PROFESSION) = 1. Defaults in `label_map.yaml::severity`. Shown only because at least one cell here moves >3pp from raw leak; otherwise the two tables tracked within noise.

| Corpus | `anonde-patterns` | `anonde-ner` | `anonde-ner-stack` | `presidio` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|---:|
| `finance_de` | 3.3% | 1.5% | **1.2%** 🥇 | 30.2% | 29.2% | 16.7% |
| `synth_finance_de` | 6.2% | 0.5% | **0.2%** 🥇 | 23.0% | 17.2% | 9.9% |

## Retail finance · Spanish

Corpora in this group: `synth_finance_es`.

### Leak rate · lower is better

A gold PHI span is *leaked* when **no** predicted span overlaps it — 'did we miss a name?'

| Corpus | `anonde-patterns` | `anonde-ner` | `anonde-ner-stack` | `presidio` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|---:|
| `synth_finance_es` | 46.4% | 12.6% | **5.5%** 🥇 | 18.5% | 25.4% | 17.6% |

> **Partial coverage** — some engines were benchmarked on a fixed subsample, not every gold doc:
>
> - `openai-pf` on `synth_finance_es`: scored on **40/120 docs** (deterministic subsample — metrics above are over those 40 docs only, not the full corpus).

### Severity-weighted leak rate · lower is better

Each leaked span weighted by compliance tier — direct identifiers (PERSON, EMAIL, PHONE, ADDRESS, DOB) = 5, high-stakes IDs (SSN/MRN/IBAN) = 10, quasi-identifiers (LOCATION, ORG, PROFESSION) = 1. Defaults in `label_map.yaml::severity`. Shown only because at least one cell here moves >3pp from raw leak; otherwise the two tables tracked within noise.

| Corpus | `anonde-patterns` | `anonde-ner` | `anonde-ner-stack` | `presidio` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|---:|
| `synth_finance_es` | 37.2% | 10.9% | 4.9% | 19.9% | 28.8% | **4.8%** 🥇 |

## Retail finance · French

Corpora in this group: `synth_finance_fr`.

### Leak rate · lower is better

A gold PHI span is *leaked* when **no** predicted span overlaps it — 'did we miss a name?'

| Corpus | `anonde-patterns` | `anonde-ner` | `anonde-ner-stack` | `presidio` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|---:|
| `synth_finance_fr` | 44.6% | 11.0% | **4.3%** 🥇 | 19.4% | 23.9% | 19.3% |

> **Partial coverage** — some engines were benchmarked on a fixed subsample, not every gold doc:
>
> - `openai-pf` on `synth_finance_fr`: scored on **40/120 docs** (deterministic subsample — metrics above are over those 40 docs only, not the full corpus).

### Severity-weighted leak rate · lower is better

Each leaked span weighted by compliance tier — direct identifiers (PERSON, EMAIL, PHONE, ADDRESS, DOB) = 5, high-stakes IDs (SSN/MRN/IBAN) = 10, quasi-identifiers (LOCATION, ORG, PROFESSION) = 1. Defaults in `label_map.yaml::severity`. Shown only because at least one cell here moves >3pp from raw leak; otherwise the two tables tracked within noise.

| Corpus | `anonde-patterns` | `anonde-ner` | `anonde-ner-stack` | `presidio` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|---:|
| `synth_finance_fr` | 35.1% | 8.6% | **2.8%** 🥇 | 21.7% | 26.4% | 6.7% |

## Retail finance · Italian

Corpora in this group: `synth_finance_it`.

### Leak rate · lower is better

A gold PHI span is *leaked* when **no** predicted span overlaps it — 'did we miss a name?'

| Corpus | `anonde-patterns` | `anonde-ner` | `anonde-ner-stack` | `presidio` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|---:|
| `synth_finance_it` | 39.5% | 12.2% | **4.3%** 🥇 | 29.6% | 26.4% | 14.1% |

> **Partial coverage** — some engines were benchmarked on a fixed subsample, not every gold doc:
>
> - `openai-pf` on `synth_finance_it`: scored on **40/120 docs** (deterministic subsample — metrics above are over those 40 docs only, not the full corpus).

### Severity-weighted leak rate · lower is better

Each leaked span weighted by compliance tier — direct identifiers (PERSON, EMAIL, PHONE, ADDRESS, DOB) = 5, high-stakes IDs (SSN/MRN/IBAN) = 10, quasi-identifiers (LOCATION, ORG, PROFESSION) = 1. Defaults in `label_map.yaml::severity`. Shown only because at least one cell here moves >3pp from raw leak; otherwise the two tables tracked within noise.

| Corpus | `anonde-patterns` | `anonde-ner` | `anonde-ner-stack` | `presidio` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|---:|
| `synth_finance_it` | 30.5% | 9.5% | **3.7%** 🥇 | 40.5% | 23.8% | 4.1% |

## Enterprise logs · English

Corpora in this group: `synth_logs`.

### Leak rate · lower is better

A gold PHI span is *leaked* when **no** predicted span overlaps it — 'did we miss a name?'

| Corpus | `anonde-patterns` | `anonde-ner` | `anonde-ner-stack` | `presidio` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|---:|
| `synth_logs` | 22.8% | 13.1% | **11.5%** 🥇 | 24.2% | 73.9% | 16.9% |

> **Partial coverage** — some engines were benchmarked on a fixed subsample, not every gold doc:
>
> - `openai-pf` on `synth_logs`: scored on **40/120 docs** (deterministic subsample — metrics above are over those 40 docs only, not the full corpus).

### Severity-weighted leak rate · lower is better

Each leaked span weighted by compliance tier — direct identifiers (PERSON, EMAIL, PHONE, ADDRESS, DOB) = 5, high-stakes IDs (SSN/MRN/IBAN) = 10, quasi-identifiers (LOCATION, ORG, PROFESSION) = 1. Defaults in `label_map.yaml::severity`. Shown only because at least one cell here moves >3pp from raw leak; otherwise the two tables tracked within noise.

| Corpus | `anonde-patterns` | `anonde-ner` | `anonde-ner-stack` | `presidio` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|---:|
| `synth_logs` | 27.7% | 16.4% | 14.4% | 31.5% | 82.1% | **8.8%** 🥇 |

## General structured PII · English

Corpora in this group: `ai4privacy_en`.

### Leak rate · lower is better

A gold PHI span is *leaked* when **no** predicted span overlaps it — 'did we miss a name?'

| Corpus | `anonde-patterns` | `anonde-ner` | `anonde-ner-stack` | `presidio` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|---:|
| `ai4privacy_en` | 46.2% | 15.1% | **11.2%** 🥇 | 56.0% | 35.0% | 15.5% |

> **Partial coverage** — some engines were benchmarked on a fixed subsample, not every gold doc:
>
> - `openai-pf` on `ai4privacy_en`: scored on **40/1000 docs** (deterministic subsample — metrics above are over those 40 docs only, not the full corpus).

### Severity-weighted leak rate · lower is better

Each leaked span weighted by compliance tier — direct identifiers (PERSON, EMAIL, PHONE, ADDRESS, DOB) = 5, high-stakes IDs (SSN/MRN/IBAN) = 10, quasi-identifiers (LOCATION, ORG, PROFESSION) = 1. Defaults in `label_map.yaml::severity`. Shown only because at least one cell here moves >3pp from raw leak; otherwise the two tables tracked within noise.

| Corpus | `anonde-patterns` | `anonde-ner` | `anonde-ner-stack` | `presidio` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|---:|
| `ai4privacy_en` | 37.6% | 12.9% | 10.3% | 57.4% | 37.0% | **10.0%** 🥇 |

## General structured PII · German

Corpora in this group: `ai4privacy_de`.

### Leak rate · lower is better

A gold PHI span is *leaked* when **no** predicted span overlaps it — 'did we miss a name?'

| Corpus | `anonde-patterns` | `anonde-ner` | `anonde-ner-stack` | `presidio` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|---:|
| `ai4privacy_de` | 57.5% | 14.2% | **10.4%** 🥇 | 58.4% | 33.0% | 23.6% |

> **Partial coverage** — some engines were benchmarked on a fixed subsample, not every gold doc:
>
> - `openai-pf` on `ai4privacy_de`: scored on **40/1000 docs** (deterministic subsample — metrics above are over those 40 docs only, not the full corpus).

### Severity-weighted leak rate · lower is better

Each leaked span weighted by compliance tier — direct identifiers (PERSON, EMAIL, PHONE, ADDRESS, DOB) = 5, high-stakes IDs (SSN/MRN/IBAN) = 10, quasi-identifiers (LOCATION, ORG, PROFESSION) = 1. Defaults in `label_map.yaml::severity`. Shown only because at least one cell here moves >3pp from raw leak; otherwise the two tables tracked within noise.

| Corpus | `anonde-patterns` | `anonde-ner` | `anonde-ner-stack` | `presidio` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|---:|
| `ai4privacy_de` | 61.7% | 14.4% | **12.0%** 🥇 | 62.4% | 34.7% | 16.3% |

## General structured PII · Spanish

Corpora in this group: `ai4privacy_es`.

### Leak rate · lower is better

A gold PHI span is *leaked* when **no** predicted span overlaps it — 'did we miss a name?'

| Corpus | `anonde-patterns` | `anonde-ner` | `anonde-ner-stack` | `presidio` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|---:|
| `ai4privacy_es` | 66.9% | 17.3% | **12.4%** 🥇 | 61.0% | 34.6% | 17.5% |

> **Partial coverage** — some engines were benchmarked on a fixed subsample, not every gold doc:
>
> - `openai-pf` on `ai4privacy_es`: scored on **40/1000 docs** (deterministic subsample — metrics above are over those 40 docs only, not the full corpus).

### Severity-weighted leak rate · lower is better

Each leaked span weighted by compliance tier — direct identifiers (PERSON, EMAIL, PHONE, ADDRESS, DOB) = 5, high-stakes IDs (SSN/MRN/IBAN) = 10, quasi-identifiers (LOCATION, ORG, PROFESSION) = 1. Defaults in `label_map.yaml::severity`. Shown only because at least one cell here moves >3pp from raw leak; otherwise the two tables tracked within noise.

| Corpus | `anonde-patterns` | `anonde-ner` | `anonde-ner-stack` | `presidio` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|---:|
| `ai4privacy_es` | 66.9% | 14.4% | 11.0% | 65.1% | 35.2% | **7.6%** 🥇 |

## General structured PII · French

Corpora in this group: `ai4privacy_fr`.

### Leak rate · lower is better

A gold PHI span is *leaked* when **no** predicted span overlaps it — 'did we miss a name?'

| Corpus | `anonde-patterns` | `anonde-ner` | `anonde-ner-stack` | `presidio` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|---:|
| `ai4privacy_fr` | 66.9% | 17.2% | **11.1%** 🥇 | 54.1% | 34.9% | 16.3% |

> **Partial coverage** — some engines were benchmarked on a fixed subsample, not every gold doc:
>
> - `openai-pf` on `ai4privacy_fr`: scored on **40/1000 docs** (deterministic subsample — metrics above are over those 40 docs only, not the full corpus).

### Severity-weighted leak rate · lower is better

Each leaked span weighted by compliance tier — direct identifiers (PERSON, EMAIL, PHONE, ADDRESS, DOB) = 5, high-stakes IDs (SSN/MRN/IBAN) = 10, quasi-identifiers (LOCATION, ORG, PROFESSION) = 1. Defaults in `label_map.yaml::severity`. Shown only because at least one cell here moves >3pp from raw leak; otherwise the two tables tracked within noise.

| Corpus | `anonde-patterns` | `anonde-ner` | `anonde-ner-stack` | `presidio` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|---:|
| `ai4privacy_fr` | 65.2% | 15.2% | 10.8% | 56.7% | 35.9% | **9.3%** 🥇 |

## General structured PII · Italian

Corpora in this group: `ai4privacy_it`.

### Leak rate · lower is better

A gold PHI span is *leaked* when **no** predicted span overlaps it — 'did we miss a name?'

| Corpus | `anonde-patterns` | `anonde-ner` | `anonde-ner-stack` | `presidio` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|---:|
| `ai4privacy_it` | 62.2% | 19.6% | **15.0%** 🥇 | 59.6% | 36.0% | 21.2% |

> **Partial coverage** — some engines were benchmarked on a fixed subsample, not every gold doc:
>
> - `openai-pf` on `ai4privacy_it`: scored on **40/1000 docs** (deterministic subsample — metrics above are over those 40 docs only, not the full corpus).

### Severity-weighted leak rate · lower is better

Each leaked span weighted by compliance tier — direct identifiers (PERSON, EMAIL, PHONE, ADDRESS, DOB) = 5, high-stakes IDs (SSN/MRN/IBAN) = 10, quasi-identifiers (LOCATION, ORG, PROFESSION) = 1. Defaults in `label_map.yaml::severity`. Shown only because at least one cell here moves >3pp from raw leak; otherwise the two tables tracked within noise.

| Corpus | `anonde-patterns` | `anonde-ner` | `anonde-ner-stack` | `presidio` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|---:|
| `ai4privacy_it` | 57.2% | 15.9% | **13.0%** 🥇 | 62.3% | 36.6% | 13.6% |

## Academic NER (newswire / social) · English

Corpora in this group: `conll2003_en`, `wnut_17`.

### Leak rate · lower is better

A gold PHI span is *leaked* when **no** predicted span overlaps it — 'did we miss a name?'

| Corpus | `anonde-patterns` | `anonde-ner` | `anonde-ner-stack` | `presidio` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|---:|
| `conll2003_en` | 96.7% | 10.4% | **3.6%** 🥇 | 6.7% | 6.0% | 64.0% |
| `wnut_17` | 77.7% | 18.1% | **8.0%** 🥇 | 43.1% | 25.5% | 82.1% |

> **Partial coverage** — some engines were benchmarked on a fixed subsample, not every gold doc:
>
> - `openai-pf` on `conll2003_en`: scored on **40/300 docs** (deterministic subsample — metrics above are over those 40 docs only, not the full corpus).
> - `openai-pf` on `wnut_17`: scored on **40/300 docs** (deterministic subsample — metrics above are over those 40 docs only, not the full corpus).

### Severity-weighted leak rate · lower is better

Each leaked span weighted by compliance tier — direct identifiers (PERSON, EMAIL, PHONE, ADDRESS, DOB) = 5, high-stakes IDs (SSN/MRN/IBAN) = 10, quasi-identifiers (LOCATION, ORG, PROFESSION) = 1. Defaults in `label_map.yaml::severity`. Shown only because at least one cell here moves >3pp from raw leak; otherwise the two tables tracked within noise.

| Corpus | `anonde-patterns` | `anonde-ner` | `anonde-ner-stack` | `presidio` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|---:|
| `conll2003_en` | 97.0% | 15.2% | **2.3%** 🥇 | 5.3% | 6.9% | 47.9% |
| `wnut_17` | 71.4% | 12.7% | **4.9%** 🥇 | 42.2% | 25.4% | 61.5% |

## Academic NER (newswire / social) · German

Corpora in this group: `wikiann_de`, `germeval_14`, `conll2003_de`.

### Leak rate · lower is better

A gold PHI span is *leaked* when **no** predicted span overlaps it — 'did we miss a name?'

| Corpus | `anonde-patterns` | `anonde-ner` | `anonde-ner-stack` | `presidio` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|---:|
| `wikiann_de` | 41.9% | 5.4% | **3.0%** 🥇 | 16.7% | 12.8% | 59.6% |
| `germeval_14` | 55.3% | **10.2%** 🥇 | 10.9% | 22.0% | 18.9% | 94.6% |

> **Partial coverage** — some engines were benchmarked on a fixed subsample, not every gold doc:
>
> - `openai-pf` on `wikiann_de`: scored on **40/300 docs** (deterministic subsample — metrics above are over those 40 docs only, not the full corpus).
> - `openai-pf` on `germeval_14`: scored on **40/300 docs** (deterministic subsample — metrics above are over those 40 docs only, not the full corpus).

### Severity-weighted leak rate · lower is better

Each leaked span weighted by compliance tier — direct identifiers (PERSON, EMAIL, PHONE, ADDRESS, DOB) = 5, high-stakes IDs (SSN/MRN/IBAN) = 10, quasi-identifiers (LOCATION, ORG, PROFESSION) = 1. Defaults in `label_map.yaml::severity`. Shown only because at least one cell here moves >3pp from raw leak; otherwise the two tables tracked within noise.

| Corpus | `anonde-patterns` | `anonde-ner` | `anonde-ner-stack` | `presidio` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|---:|
| `wikiann_de` | 30.3% | 4.4% | **1.5%** 🥇 | 6.9% | 10.7% | 44.3% |
| `germeval_14` | 47.6% | 7.1% | **5.9%** 🥇 | 15.8% | 16.1% | 88.8% |

## Adversarial / out-of-distribution · German

Corpora in this group: `adversarial_de`.

### Leak rate · lower is better

A gold PHI span is *leaked* when **no** predicted span overlaps it — 'did we miss a name?'

| Corpus | `anonde-patterns` | `anonde-ner` | `anonde-ner-stack` | `presidio` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|---:|
| `adversarial_de` | 12.6% | 8.3% | **7.7%** 🥇 | 37.4% | 43.5% | 32.1% |

> **Partial coverage** — some engines were benchmarked on a fixed subsample, not every gold doc:
>
> - `openai-pf` on `adversarial_de`: scored on **40/300 docs** (deterministic subsample — metrics above are over those 40 docs only, not the full corpus).

### Severity-weighted leak rate · lower is better

Each leaked span weighted by compliance tier — direct identifiers (PERSON, EMAIL, PHONE, ADDRESS, DOB) = 5, high-stakes IDs (SSN/MRN/IBAN) = 10, quasi-identifiers (LOCATION, ORG, PROFESSION) = 1. Defaults in `label_map.yaml::severity`. Shown only because at least one cell here moves >3pp from raw leak; otherwise the two tables tracked within noise.

| Corpus | `anonde-patterns` | `anonde-ner` | `anonde-ner-stack` | `presidio` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|---:|
| `adversarial_de` | 12.1% | 8.2% | **7.5%** 🥇 | 40.7% | 44.1% | 28.3% |

## Latency · per-document p50 / p95

Wall-clock per `engine.Analyze(doc)` call. p50 = steady-state, p95 = tail (the SLO knob). Mean + p99 in `results_matrix.csv`. One table across every corpus — latency tracks corpus length, not domain or language.

| Corpus | `anonde-patterns` p50 / p95 | `anonde-ner` p50 / p95 | `anonde-ner-stack` p50 / p95 | `presidio` p50 / p95 | `gliner-py` p50 / p95 | `openai-pf` p50 / p95 |
|---|---:|---:|---:|---:|---:|---:|
| `synth_clinical_en` | 1 ms / 2 ms | 113 ms / 179 ms | 1.0 s / 1.7 s | 25 ms / 34 ms | 355 ms / 456 ms | 433 ms / 470 ms |
| `openmed` | 3 ms / 9 ms | 328 ms / 1.1 s | 4.0 s / 14.0 s | 77 ms / 209 ms | 1.7 s / 7.9 s | 1.5 s / 3.9 s |
| `synth_clinical` | 1 ms / 2 ms | 109 ms / 178 ms | 983 ms / 1.6 s | 26 ms / 33 ms | 410 ms / 551 ms | 418 ms / 500 ms |
| `pharmaconer_es` | 2 ms / 4 ms | 248 ms / 565 ms | 2.3 s / 5.3 s | 38 ms / 92 ms | 833 ms / 2.3 s | 798 ms / 1.7 s |
| `meddocan_es` | 2 ms / 3 ms | 340 ms / 677 ms | 3.3 s / 6.6 s | 54 ms / 107 ms | 1.2 s / 2.9 s | 1.1 s / 1.6 s |
| `synth_clinical_fr` | 1 ms / 2 ms | 199 ms / 258 ms | 1.6 s / 2.5 s | 28 ms / 38 ms | 441 ms / 654 ms | 535 ms / 730 ms |
| `synth_clinical_it` | 1 ms / 1 ms | 165 ms / 255 ms | 1.5 s / 2.4 s | 36 ms / 50 ms | 629 ms / 881 ms | 497 ms / 578 ms |
| `mapa_en` | 1 ms / 2 ms | 58 ms / 121 ms | 509 ms / 917 ms | 7 ms / 20 ms | 143 ms / 298 ms | 181 ms / 386 ms |
| `legal_de` | 1 ms / 2 ms | 164 ms / 186 ms | 1.5 s / 1.7 s | 31 ms / 52 ms | 365 ms / 483 ms | 605 ms / 806 ms |
| `mapa_de` | 0 ms / 1 ms | 49 ms / 91 ms | 671 ms / 1.1 s | 7 ms / 13 ms | 193 ms / 313 ms | 219 ms / 384 ms |
| `mapa_es` | 0 ms / 1 ms | 68 ms / 159 ms | 652 ms / 1.2 s | 9 ms / 28 ms | 207 ms / 413 ms | 165 ms / 323 ms |
| `mapa_fr` | 0 ms / 1 ms | 52 ms / 103 ms | 564 ms / 952 ms | 7 ms / 17 ms | 169 ms / 304 ms | 226 ms / 443 ms |
| `mapa_it` | 0 ms / 1 ms | 41 ms / 69 ms | 545 ms / 829 ms | 6 ms / 10 ms | 149 ms / 242 ms | 171 ms / 276 ms |
| `synth_finance_en` | 1 ms / 2 ms | 101 ms / 157 ms | 871 ms / 1.3 s | 18 ms / 31 ms | 266 ms / 396 ms | 289 ms / 411 ms |
| `finance_de` | 1 ms / 2 ms | 127 ms / 193 ms | 984 ms / 1.4 s | 24 ms / 37 ms | 354 ms / 535 ms | 501 ms / 556 ms |
| `synth_finance_de` | 1 ms / 1 ms | 92 ms / 121 ms | 888 ms / 1.2 s | 17 ms / 33 ms | 286 ms / 457 ms | 350 ms / 430 ms |
| `synth_finance_es` | 1 ms / 2 ms | 139 ms / 238 ms | 1.3 s / 1.9 s | 18 ms / 32 ms | 338 ms / 544 ms | 591 ms / 760 ms |
| `synth_finance_fr` | 1 ms / 2 ms | 116 ms / 176 ms | 922 ms / 1.2 s | 22 ms / 54 ms | 355 ms / 533 ms | 315 ms / 367 ms |
| `synth_finance_it` | 1 ms / 2 ms | 144 ms / 211 ms | 1.2 s / 1.7 s | 26 ms / 37 ms | 391 ms / 601 ms | 401 ms / 826 ms |
| `synth_logs` | 2 ms / 3 ms | 201 ms / 368 ms | 2.2 s / 4.0 s | 32 ms / 78 ms | 831 ms / 2.3 s | 1.2 s / 1.9 s |
| `ai4privacy_en` | 1 ms / 2 ms | 118 ms / 171 ms | 829 ms / 1.2 s | 19 ms / 29 ms | 302 ms / 416 ms | 561 ms / 716 ms |
| `ai4privacy_de` | 1 ms / 1 ms | 71 ms / 97 ms | 817 ms / 1.1 s | 12 ms / 17 ms | 225 ms / 308 ms | 260 ms / 297 ms |
| `ai4privacy_es` | 0 ms / 1 ms | 92 ms / 132 ms | 865 ms / 1.3 s | 11 ms / 15 ms | 247 ms / 331 ms | 285 ms / 601 ms |
| `ai4privacy_fr` | 0 ms / 1 ms | 69 ms / 91 ms | 787 ms / 1.0 s | 15 ms / 21 ms | 224 ms / 288 ms | 267 ms / 311 ms |
| `ai4privacy_it` | 0 ms / 1 ms | 79 ms / 124 ms | 741 ms / 1.0 s | 11 ms / 16 ms | 221 ms / 300 ms | 276 ms / 364 ms |
| `conll2003_en` | 0 ms / 0 ms | 27 ms / 40 ms | 331 ms / 420 ms | 4 ms / 8 ms | 107 ms / 155 ms | 83 ms / 162 ms |
| `wnut_17` | 0 ms / 1 ms | 32 ms / 49 ms | 384 ms / 526 ms | 4 ms / 8 ms | 115 ms / 163 ms | 128 ms / 192 ms |
| `wikiann_de` | 0 ms / 0 ms | 26 ms / 36 ms | 338 ms / 442 ms | 3 ms / 5 ms | 98 ms / 124 ms | 70 ms / 152 ms |
| `germeval_14` | 0 ms / 1 ms | 34 ms / 46 ms | 379 ms / 480 ms | 4 ms / 6 ms | 118 ms / 153 ms | 114 ms / 187 ms |
| `adversarial_de` | 1 ms / 2 ms | 137 ms / 216 ms | 1.3 s / 2.2 s | 34 ms / 54 ms | 558 ms / 989 ms | 747 ms / 899 ms |

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
Held-out corpora with no known overlap for any of the engines
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
*Generated by `bench/scoring/render_matrix.py` over 240 cells. Full per-entity-type breakdown in `results_matrix.csv`.*
