# 🛡️ anonde bench matrix

**`anonde-ner` — the default NER image — is the lowest-leak PII redactor in this benchmark: it misses just 11.1% of gold PII vs raw GLiNER 33.1% / OpenAI Privacy Filter 24.0%, across 29 gold-annotated corpora and 5 languages.** Tuned recall-first — it catches more PII than the precision-optimised tools, at the cost of more over-redaction (quantified two lines down).

| Language | `anonde-ner` ⬅ ours | `presidio` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|
| **English** | **10.0%** 🥇 | – | 39.8% | 19.3% |
| **German** | **6.4%** 🥇 | – | 31.9% | 28.8% |
| **Spanish** | **15.8%** 🥇 | – | 29.3% | 25.7% |
| **French** | **13.9%** 🥇 | – | 30.7% | 21.1% |
| **Italian** | **14.6%** 🥇 | – | 33.8% | 21.0% |
| **All** | **11.1%** 🥇 | – | **33.1%** | **24.0%** |

*The one table. **Leak rate** = fraction of gold PII spans **missed** — lower is better; 🥇 = lowest-leak engine in the row. Columns are the default NER image `anonde-ner` vs the competing field (anonde's own patterns / stack tiers and the per-domain roll-ups are under **Details**). `presidio-transformer` is EN-only (`–` elsewhere, by design); `openai-pf` is scored on a fixed per-corpus subsample. Full method, precision, and every slice are in **Details** below.*

| Language | `anonde-ner` ⬅ ours | `presidio` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|
| **English** | **0.627** 🥇 | – | 0.441 | 0.002 |
| **German** | **0.597** 🥇 | – | 0.480 | 0.006 |
| **Spanish** | **0.555** 🥇 | – | 0.511 | 0.000 |
| **French** | **0.683** 🥇 | – | 0.536 | 0.003 |
| **Italian** | **0.634** 🥇 | – | 0.480 | 0.004 |
| **All** | **0.612** 🥇 | – | **0.487** | **0.003** |

*The twin. **Strict F1** = exact span **and** type match (CoNLL) — higher is better, 🥇 = best in row. It reproduces the standard scorer (`nervaluate`) *exactly* (Δ≈0 in `verify_official.py`), so it is the citable accuracy metric alongside leak rate. It is precision-inclusive, so it also reflects over-redaction: an over-redacting tool can rank lower here than on leak rate, where a precision-first rival edges ahead. The lenient overlap view and full method are under **Details** / `METHODOLOGY.md`.*

> **The trade — over-redaction (1 − precision, lower is better):** `anonde-ner` 30.1% vs raw GLiNER 20.8%. anonde deliberately over-redacts more so it leaks less — the recall-first dial, not a defect. Full precision scorecard (with the zero-gold exclusion rule) is under Details.

---

## Details

The full working behind the scorecard above — leak-rate and precision roll-ups (per domain · per language · overall), the per-cell grids, latency, cost, caveats, and the glossary, in the order they were already in. The hero table is the answer; everything here is how it is computed.

> **TL;DR** — `anonde-ner` (the default NER image, `ghcr.io/anonde-io/anonde-ner`) is the lowest-leak engine on **29 of 29** gold-annotated corpora. Biggest absolute improvement over the best baseline: **+25.0pp** in leak rate. Strict F1 trades exact-byte alignment for catching more PHI — the right trade-off for a redactor, not a benchmark gaming exercise. The inverse cost — over-redaction / false positives — now has its own **Precision scorecard** directly below this leak-rate one (partial, overlap-based precision per engine, roll-up rows + a per-cell grid in the Detailed breakdown); read it to compare how much each engine over-redacts.

## 🎯 Scorecard · leak rate roll-ups

The one table. Roll-up rows only (per domain · per language · overall); the per-(domain × language) detail grid lives in the Detailed breakdown below. Each number is **leak rate** (fraction of gold PHI spans missed — lower is better). `anonde-ner` is the default NER image (`ghcr.io/anonde-io/anonde-ner`) and the anchor column; **Verdict** says whether it beats the field. 🥇 marks the lowest-leak engine in the row. Roll-up rows pool leaked-over-gold across the group (doc-weighted, so larger corpora count more).

| Slice | Scope | `anonde-ner` ⬅︎ anonde (default NER) | `anonde-patterns` | `presidio` | `gliner-py` | `openai-pf` | Verdict |
|---|---|---:|---:|---:|---:|---:|:--:|
| **Σ ALL** | **all** | **11.1%** 🥇 | **46.8%** | – | **33.1%** | **24.0%** | ✅ |
| | | | | | | | |
| _Σ Clinical / medical de-identification_ | _all langs_ | **11.3%** 🥇 | 48.3% | – | 28.0% | 27.3% | ✅ |
| _Σ Legal / administrative_ | _all langs_ | **2.8%** 🥇 | 23.1% | – | 21.5% | 36.7% | ✅ |
| _Σ Retail finance_ | _all langs_ | **8.1%** 🥇 | 25.7% | – | 23.5% | 19.0% | ✅ |
| _Σ Enterprise logs_ | _all langs_ | **13.3%** 🥇 | 29.5% | – | 73.2% | 15.1% | ✅ |
| _Σ General structured PII_ | _all langs_ | **13.5%** 🥇 | 62.8% | – | 34.7% | 18.8% | ✅ |
| _Σ Academic NER (newswire / social)_ | _all langs_ | **5.9%** 🥇 | 68.4% | – | 13.8% | 72.7% | ✅ |
| _Σ Adversarial / out-of-distribution_ | _all langs_ | **8.1%** 🥇 | 12.7% | – | 43.4% | 32.0% | ✅ |
| | | | | | | | |
| _Σ all domains_ | _English_ | **10.0%** 🥇 | 39.2% | – | 39.8% | 19.3% | ✅ |
| _Σ all domains_ | _German_ | **6.4%** 🥇 | 25.0% | – | 31.9% | 28.8% | ✅ |
| _Σ all domains_ | _Spanish_ | **15.8%** 🥇 | 69.9% | – | 29.3% | 25.7% | ✅ |
| _Σ all domains_ | _French_ | **13.9%** 🥇 | 63.6% | – | 30.7% | 21.1% | ✅ |
| _Σ all domains_ | _Italian_ | **14.6%** 🥇 | 58.6% | – | 33.8% | 21.0% | ✅ |

> **Anonde scoreboard** — across the **24** populated `(domain, language)` cells in the matrix, `anonde-ner` is the **lowest-leak engine in 24**, ties in **0**, and is beaten in **0**. ✅ = anonde leads · 🟰 = tied · ❌ = a baseline leaks less. See the per-cell leak-rate grid in the Detailed breakdown below for which baseline wins where. (The TL;DR's win count is per-corpus, a finer split than these per-cell rows.)

## 🎯 Precision scorecard · false-positive rate roll-ups

Partial precision = fraction of redacted spans that overlap a real PII span; the inverse (**1 − precision**) is the over-redaction / false-positive rate. **Higher is better.** This is the overlap-based *partial* view (a predicted span counts as a true positive if it overlaps **any** gold span), NOT the strict byte-exact view — strict punishes a one-char offset as a full false positive and reads misleadingly low (~0.1–0.3) for every engine including the baselines, so it is the wrong headline for a redactor. The leak-rate scorecard above answers recall ("did we miss real PII?"); this one answers the inverse cost — over-redaction. Same structure as the leak scorecard: roll-up rows only (per domain · per language · overall), `anonde-ner` anchored first. 🥇 marks the highest-precision engine in the row. Each cell pools tp/(tp+fp) across the group (micro-average, doc-weighted) and annotates the pooled raw FP count — precision can look fine while absolute false-positive volume is high.

| Slice | Scope | `anonde-ner` ⬅︎ anonde (default NER) | `anonde-patterns` | `presidio` | `gliner-py` | `openai-pf` |
|---|---|---:|---:|---:|---:|---:|
| **Σ ALL** | **all** | **69.9% (27966 fp)** | **83.5% (8005 fp)** 🥇 | – | **79.2% (12754 fp)** | **75.2% (4691 fp)** |
| | | | | | | |
| _Σ Clinical / medical de-identification_ | _all langs_ | 64.0% (7474 fp) | 80.0% (2048 fp) | – | **88.5% (1491 fp)** 🥇 | 88.4% (735 fp) |
| _Σ Legal / administrative_ | _all langs_ | 42.8% (4162 fp) | **81.5% (582 fp)** 🥇 | – | 45.5% (3315 fp) | 75.9% (229 fp) |
| _Σ Retail finance_ | _all langs_ | 87.5% (1616 fp) | 90.7% (937 fp) | – | **92.0% (957 fp)** 🥇 | 83.5% (875 fp) |
| _Σ Enterprise logs_ | _all langs_ | 73.8% (1003 fp) | **84.3% (476 fp)** 🥇 | – | 75.9% (243 fp) | 30.2% (2495 fp) |
| _Σ General structured PII_ | _all langs_ | 71.6% (11174 fp) | 81.8% (2811 fp) | – | 78.3% (5131 fp) | **93.1% (113 fp)** 🥇 |
| _Σ Academic NER (newswire / social)_ | _all langs_ | 38.6% (1472 fp) | 41.7% (329 fp) | – | 57.9% (829 fp) | **69.4% (37 fp)** 🥇 |
| _Σ Adversarial / out-of-distribution_ | _all langs_ | 83.4% (1065 fp) | **86.4% (822 fp)** 🥇 | – | 80.0% (788 fp) | 80.0% (207 fp) |
| | | | | | | |
| _Σ all domains_ | _English_ | 69.0% (5624 fp) | 77.8% (2514 fp) | – | **79.4% (2240 fp)** 🥇 | 53.5% (2693 fp) |
| _Σ all domains_ | _German_ | 68.9% (10109 fp) | 79.9% (4627 fp) | – | 79.3% (4337 fp) | **85.7% (935 fp)** 🥇 |
| _Σ all domains_ | _Spanish_ | 68.0% (5187 fp) | **97.1% (127 fp)** 🥇 | – | 80.9% (2131 fp) | 76.5% (504 fp) |
| _Σ all domains_ | _French_ | 75.9% (3080 fp) | **95.8% (191 fp)** 🥇 | – | 81.0% (1782 fp) | 87.5% (270 fp) |
| _Σ all domains_ | _Italian_ | 70.3% (3966 fp) | **89.7% (546 fp)** 🥇 | – | 75.1% (2264 fp) | 87.5% (289 fp) |

> **Reading this table** — a cell of `92.0% (40 fp)` means 92% of the spans that engine redacted overlapped real PII; the remaining 8% (40 absolute spans) were over-redaction. Recall (leak rate) is in the scorecard above; this is the other half of the trade-off.

> **Why some predictions are not counted** — a `(corpus, entity-type)` cell where the gold annotates **zero** spans of that type (`tp + fn == 0`) is *unscoreable for precision*: with no gold of that type present, every prediction there is mechanically a false positive against absent gold — a **schema gap** (e.g. a corpus that annotates PERSON but not LOCATION), not real over-redaction. Such cells are **excluded** from the precision pool above, and an empty-gold corpus (every type zero-gold) drops out entirely. This is a scorecard-aggregation choice only — the raw per-type counts stay intact in `results_matrix.csv`, and **leak-rate / recall are untouched** (they score against the full gold). Excluded here: **112 (corpus, type) cells** across **1 empty-gold corpora**. For full transparency, the raw Σ ALL precision *including* every zero-gold cell: `anonde-ner` 62.5% (39004 fp) · `anonde-patterns` 80.0% (10153 fp) · `gliner-py` 67.5% (23464 fp) · `openai-pf` 69.6% (6214 fp).

<details><summary>Engine profiles · what each column means</summary>

## Engine profiles

The three anonde columns map 1:1 to the three shipping Docker images. They are not three competing tools; they are three deployment tiers — pick the one that fits your hardware and leak-rate budget. Compare *across the row* for the trade-off, not against each other for a winner.

| Engine | Image | CGO | Cold start | Best fit |
|---|---|---|---|---|
| `anonde-patterns` | `ghcr.io/anonde-io/anonde` (~12 MB) | not required | <1 s | structured slot-gen text (forms, logs, finance/legal docs) — wins F1 on PHONE, EMAIL, DATE, PROFESSION when the regex shape is tight |
| `anonde-ner` | `ghcr.io/anonde-io/anonde-ner` (~770 MB) | required | 5-30 s warmup | **default NER tier**. GLiNER PII (FP32 ONNX) + patterns. Natural text + multilingual PHI; the lowest-leak engine on most gold corpora. |
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

| Domain | Language | `anonde-ner` ⬅︎ anonde (default NER) | `anonde-patterns` | `presidio` | `gliner-py` | `openai-pf` | Verdict |
|---|---|---:|---:|---:|---:|---:|:--:|
| **Clinical / medical de-identification** | English | **1.7%** 🥇 | 13.4% | – | 23.3% | 24.6% | ✅ |
| **Clinical / medical de-identification** | German | **4.6%** 🥇 | 9.0% | – | 34.2% | 30.0% | ✅ |
| **Clinical / medical de-identification** | Spanish | **17.8%** 🥇 | 79.6% | – | 23.7% | 31.6% | ✅ |
| **Clinical / medical de-identification** | French | **11.5%** 🥇 | 61.4% | – | 25.3% | 21.8% | ✅ |
| **Clinical / medical de-identification** | Italian | **16.2%** 🥇 | 59.7% | – | 35.2% | 25.1% | ✅ |
| **Legal / administrative** | English | **8.4%** 🥇 | 48.9% | – | 10.7% | 76.9% | ✅ |
| **Legal / administrative** | German | **1.1%** 🥇 | 11.4% | – | 23.8% | 32.8% | ✅ |
| **Legal / administrative** | Spanish | **6.5%** 🥇 | 100.0% | – | 18.3% | 93.8% | ✅ |
| **Legal / administrative** | French | **11.4%** 🥇 | 75.2% | – | 16.3% | 95.5% | ✅ |
| **Legal / administrative** | Italian | **5.5%** 🥇 | 40.4% | – | 6.2% | 50.0% | ✅ |
| **Retail finance** | English | **2.9%** 🥇 | 30.0% | – | 20.6% | 18.5% | ✅ |
| **Retail finance** | German | **2.4%** 🥇 | 3.4% | – | 22.6% | 21.3% | ✅ |
| **Retail finance** | Spanish | **16.4%** 🥇 | 46.4% | – | 25.4% | 17.4% | ✅ |
| **Retail finance** | French | **15.9%** 🥇 | 44.6% | – | 23.9% | 19.3% | ✅ |
| **Retail finance** | Italian | **11.7%** 🥇 | 39.5% | – | 26.4% | 14.1% | ✅ |
| **Enterprise logs** | English | **13.3%** 🥇 | 29.5% | – | 73.2% | 15.1% | ✅ |
| **General structured PII** | English | **13.5%** 🥇 | 51.4% | – | 35.0% | 15.5% | ✅ |
| **General structured PII** | German | **10.7%** 🥇 | 59.8% | – | 33.0% | 23.6% | ✅ |
| **General structured PII** | Spanish | **14.2%** 🥇 | 68.9% | – | 34.6% | 17.5% | ✅ |
| **General structured PII** | French | **14.2%** 🥇 | 68.9% | – | 34.9% | 16.3% | ✅ |
| **General structured PII** | Italian | **15.0%** 🥇 | 63.9% | – | 36.0% | 21.2% | ✅ |
| **Academic NER (newswire / social)** | English | **6.6%** 🥇 | 92.0% | – | 11.8% | 71.9% | ✅ |
| **Academic NER (newswire / social)** | German | **5.2%** 🥇 | 47.7% | – | 15.5% | 73.4% | ✅ |
| **Adversarial / out-of-distribution** | German | **8.1%** 🥇 | 12.7% | – | 43.4% | 32.0% | ✅ |

## Per-cell precision · domain × language

Detail behind the precision scorecard: one row per populated `(domain, language)` cell, partial precision pooled across the cell's corpora (raw FP count annotated). Higher is better; 🥇 marks the highest-precision engine in the row. Strict byte-exact precision per entity type stays in `results_matrix.csv`.

| Domain | Language | `anonde-ner` ⬅︎ anonde (default NER) | `anonde-patterns` | `presidio` | `gliner-py` | `openai-pf` |
|---|---|---:|---:|---:|---:|---:|
| **Clinical / medical de-identification** | English | 69.0% (991 fp) | 71.7% (780 fp) | – | 93.9% (126 fp) | **99.1% (9 fp)** 🥇 |
| **Clinical / medical de-identification** | German | 53.3% (2896 fp) | 74.2% (1158 fp) | – | **87.7% (334 fp)** 🥇 | 87.6% (272 fp) |
| **Clinical / medical de-identification** | Spanish | 58.2% (2759 fp) | **98.5% (16 fp)** 🥇 | – | 82.1% (806 fp) | 69.8% (313 fp) |
| **Clinical / medical de-identification** | French | 82.8% (421 fp) | **97.4% (24 fp)** 🥇 | – | 97.0% (58 fp) | 92.1% (83 fp) |
| **Clinical / medical de-identification** | Italian | 82.4% (407 fp) | 92.9% (70 fp) | – | 90.5% (167 fp) | **94.5% (58 fp)** 🥇 |
| **Legal / administrative** | English | 17.1% (763 fp) | 60.5% (62 fp) | – | 21.7% (637 fp) | **100.0% (0 fp)** 🥇 |
| **Legal / administrative** | German | 65.4% (1357 fp) | **81.7% (518 fp)** 🥇 | – | 65.8% (1135 fp) | 78.9% (189 fp) |
| **Legal / administrative** | Spanish | 18.3% (259 fp) | – | – | **23.6% (194 fp)** 🥇 | 11.1% (16 fp) |
| **Legal / administrative** | French | 22.2% (809 fp) | **98.8% (1 fp)** 🥇 | – | 28.0% (582 fp) | 0.0% (8 fp) |
| **Legal / administrative** | Italian | 10.0% (974 fp) | **98.8% (1 fp)** 🥇 | – | 13.2% (767 fp) | 27.3% (16 fp) |
| **Retail finance** | English | **92.7% (147 fp)** 🥇 | 91.8% (120 fp) | – | 91.7% (161 fp) | 82.1% (137 fp) |
| **Retail finance** | German | 80.1% (1122 fp) | 85.1% (786 fp) | – | **90.0% (460 fp)** 🥇 | 88.6% (238 fp) |
| **Retail finance** | Spanish | 93.9% (102 fp) | **99.2% (8 fp)** 🥇 | – | 94.5% (96 fp) | 80.1% (159 fp) |
| **Retail finance** | French | 94.8% (92 fp) | **100.0% (0 fp)** 🥇 | – | 93.5% (122 fp) | 80.3% (161 fp) |
| **Retail finance** | Italian | 91.8% (153 fp) | **98.1% (23 fp)** 🥇 | – | 93.2% (118 fp) | 78.7% (180 fp) |
| **Enterprise logs** | English | 73.8% (1003 fp) | **84.3% (476 fp)** 🥇 | – | 75.9% (243 fp) | 30.2% (2495 fp) |
| **General structured PII** | English | 70.7% (2071 fp) | 74.4% (971 fp) | – | 83.4% (696 fp) | **92.3% (30 fp)** 🥇 |
| **General structured PII** | German | 68.5% (2846 fp) | 71.6% (1119 fp) | – | 77.8% (1168 fp) | **94.6% (14 fp)** 🥇 |
| **General structured PII** | Spanish | 72.9% (2067 fp) | **95.5% (103 fp)** 🥇 | – | 77.8% (1035 fp) | 94.5% (16 fp) |
| **General structured PII** | French | 76.7% (1758 fp) | 93.1% (166 fp) | – | 78.7% (1020 fp) | **93.6% (18 fp)** 🥇 |
| **General structured PII** | Italian | 69.9% (2432 fp) | 84.9% (452 fp) | – | 74.2% (1212 fp) | **91.3% (35 fp)** 🥇 |
| **Academic NER (newswire / social)** | English | 40.6% (649 fp) | 19.8% (105 fp) | – | 56.6% (377 fp) | **62.7% (22 fp)** 🥇 |
| **Academic NER (newswire / social)** | German | 37.0% (823 fp) | 48.3% (224 fp) | – | 58.9% (452 fp) | **75.8% (15 fp)** 🥇 |
| **Adversarial / out-of-distribution** | German | 83.4% (1065 fp) | **86.4% (822 fp)** 🥇 | – | 80.0% (788 fp) | 80.0% (207 fp) |

## Clinical / medical de-identification · English

Corpora in this group: `synth_clinical_en`.

### Leak rate · lower is better

A gold PHI span is *leaked* when **no** predicted span overlaps it — 'did we miss a name?'

| Corpus | `anonde-patterns` | `anonde-ner` | `presidio` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|
| `synth_clinical_en` | 13.4% | **1.7%** 🥇 | – | 23.3% | 24.6% |

> **Partial coverage** — some engines were benchmarked on a fixed subsample, not every gold doc:
>
> - `openai-pf` on `synth_clinical_en`: scored on **40/120 docs** (deterministic subsample — metrics above are over those 40 docs only, not the full corpus).

### Severity-weighted leak rate · lower is better

Each leaked span weighted by compliance tier — direct identifiers (PERSON, EMAIL, PHONE, ADDRESS, DOB) = 5, high-stakes IDs (SSN/MRN/IBAN) = 10, quasi-identifiers (LOCATION, ORG, PROFESSION) = 1. Defaults in `label_map.yaml::severity`. Shown only because at least one cell here moves >3pp from raw leak; otherwise the two tables tracked within noise.

| Corpus | `anonde-patterns` | `anonde-ner` | `presidio` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|
| `synth_clinical_en` | 8.9% | **1.7%** 🥇 | – | 26.4% | 17.6% |

## Clinical / medical de-identification · German

Corpora in this group: `openmed`, `pmc_de`, `synth_clinical`, `wiki_de`.

### Leak rate · lower is better

A gold PHI span is *leaked* when **no** predicted span overlaps it — 'did we miss a name?'

| Corpus | `anonde-patterns` | `anonde-ner` | `presidio` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|
| `openmed` | 21.6% | **11.7%** 🥇 | – | 49.7% | 35.5% |
| `synth_clinical` | 1.8% | **0.5%** 🥇 | – | 25.4% | 23.5% |

> **Partial coverage** — some engines were benchmarked on a fixed subsample, not every gold doc:
>
> - `openai-pf` on `openmed`: scored on **40/63 docs** (deterministic subsample — metrics above are over those 40 docs only, not the full corpus).
> - `openai-pf` on `synth_clinical`: scored on **40/120 docs** (deterministic subsample — metrics above are over those 40 docs only, not the full corpus).

### Severity-weighted leak rate · lower is better

Each leaked span weighted by compliance tier — direct identifiers (PERSON, EMAIL, PHONE, ADDRESS, DOB) = 5, high-stakes IDs (SSN/MRN/IBAN) = 10, quasi-identifiers (LOCATION, ORG, PROFESSION) = 1. Defaults in `label_map.yaml::severity`. Shown only because at least one cell here moves >3pp from raw leak; otherwise the two tables tracked within noise.

| Corpus | `anonde-patterns` | `anonde-ner` | `presidio` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|
| `openmed` | 21.1% | **11.6%** 🥇 | – | 50.5% | 33.9% |
| `synth_clinical` | 0.4% | **0.1%** 🥇 | – | 27.5% | 17.6% |

## Clinical / medical de-identification · Spanish

Corpora in this group: `pharmaconer_es`, `meddocan_es`.

### Leak rate · lower is better

A gold PHI span is *leaked* when **no** predicted span overlaps it — 'did we miss a name?'

| Corpus | `anonde-patterns` | `anonde-ner` | `presidio` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|
| `meddocan_es` | 79.6% | **17.8%** 🥇 | – | 23.7% | 31.6% |

> **Partial coverage** — some engines were benchmarked on a fixed subsample, not every gold doc:
>
> - `openai-pf` on `pharmaconer_es`: scored on **40/200 docs** (deterministic subsample — metrics above are over those 40 docs only, not the full corpus).
> - `openai-pf` on `meddocan_es`: scored on **40/250 docs** (deterministic subsample — metrics above are over those 40 docs only, not the full corpus).

### Severity-weighted leak rate · lower is better

Each leaked span weighted by compliance tier — direct identifiers (PERSON, EMAIL, PHONE, ADDRESS, DOB) = 5, high-stakes IDs (SSN/MRN/IBAN) = 10, quasi-identifiers (LOCATION, ORG, PROFESSION) = 1. Defaults in `label_map.yaml::severity`. Shown only because at least one cell here moves >3pp from raw leak; otherwise the two tables tracked within noise.

| Corpus | `anonde-patterns` | `anonde-ner` | `presidio` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|
| `meddocan_es` | 75.4% | 21.2% | – | 27.2% | **14.1%** 🥇 |

## Clinical / medical de-identification · French

Corpora in this group: `synth_clinical_fr`.

### Leak rate · lower is better

A gold PHI span is *leaked* when **no** predicted span overlaps it — 'did we miss a name?'

| Corpus | `anonde-patterns` | `anonde-ner` | `presidio` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|
| `synth_clinical_fr` | 61.4% | **11.5%** 🥇 | – | 25.3% | 21.8% |

> **Partial coverage** — some engines were benchmarked on a fixed subsample, not every gold doc:
>
> - `openai-pf` on `synth_clinical_fr`: scored on **40/120 docs** (deterministic subsample — metrics above are over those 40 docs only, not the full corpus).

### Severity-weighted leak rate · lower is better

Each leaked span weighted by compliance tier — direct identifiers (PERSON, EMAIL, PHONE, ADDRESS, DOB) = 5, high-stakes IDs (SSN/MRN/IBAN) = 10, quasi-identifiers (LOCATION, ORG, PROFESSION) = 1. Defaults in `label_map.yaml::severity`. Shown only because at least one cell here moves >3pp from raw leak; otherwise the two tables tracked within noise.

| Corpus | `anonde-patterns` | `anonde-ner` | `presidio` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|
| `synth_clinical_fr` | 56.8% | **11.6%** 🥇 | – | 28.0% | 15.3% |

## Clinical / medical de-identification · Italian

Corpora in this group: `synth_clinical_it`.

### Leak rate · lower is better

A gold PHI span is *leaked* when **no** predicted span overlaps it — 'did we miss a name?'

| Corpus | `anonde-patterns` | `anonde-ner` | `presidio` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|
| `synth_clinical_it` | 59.7% | **16.2%** 🥇 | – | 35.2% | 25.1% |

> **Partial coverage** — some engines were benchmarked on a fixed subsample, not every gold doc:
>
> - `openai-pf` on `synth_clinical_it`: scored on **40/120 docs** (deterministic subsample — metrics above are over those 40 docs only, not the full corpus).

### Severity-weighted leak rate · lower is better

Each leaked span weighted by compliance tier — direct identifiers (PERSON, EMAIL, PHONE, ADDRESS, DOB) = 5, high-stakes IDs (SSN/MRN/IBAN) = 10, quasi-identifiers (LOCATION, ORG, PROFESSION) = 1. Defaults in `label_map.yaml::severity`. Shown only because at least one cell here moves >3pp from raw leak; otherwise the two tables tracked within noise.

| Corpus | `anonde-patterns` | `anonde-ner` | `presidio` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|
| `synth_clinical_it` | 55.0% | **15.5%** 🥇 | – | 38.7% | 17.9% |

## Legal / administrative · English

Corpora in this group: `mapa_en`.

### Leak rate · lower is better

A gold PHI span is *leaked* when **no** predicted span overlaps it — 'did we miss a name?'

| Corpus | `anonde-patterns` | `anonde-ner` | `presidio` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|
| `mapa_en` | 48.9% | **8.4%** 🥇 | – | 10.7% | 76.9% |

> **Partial coverage** — some engines were benchmarked on a fixed subsample, not every gold doc:
>
> - `openai-pf` on `mapa_en`: scored on **40/408 docs** (deterministic subsample — metrics above are over those 40 docs only, not the full corpus).

### Severity-weighted leak rate · lower is better

Each leaked span weighted by compliance tier — direct identifiers (PERSON, EMAIL, PHONE, ADDRESS, DOB) = 5, high-stakes IDs (SSN/MRN/IBAN) = 10, quasi-identifiers (LOCATION, ORG, PROFESSION) = 1. Defaults in `label_map.yaml::severity`. Shown only because at least one cell here moves >3pp from raw leak; otherwise the two tables tracked within noise.

| Corpus | `anonde-patterns` | `anonde-ner` | `presidio` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|
| `mapa_en` | 42.9% | **8.4%** 🥇 | – | 9.8% | 75.4% |

## Legal / administrative · German

Corpora in this group: `legal_de`, `mapa_de`.

### Leak rate · lower is better

A gold PHI span is *leaked* when **no** predicted span overlaps it — 'did we miss a name?'

| Corpus | `anonde-patterns` | `anonde-ner` | `presidio` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|
| `legal_de` | 6.5% | **0.5%** 🥇 | – | 25.4% | 31.3% |
| `mapa_de` | 51.6% | **6.2%** 🥇 | – | 10.4% | 85.0% |

> **Partial coverage** — some engines were benchmarked on a fixed subsample, not every gold doc:
>
> - `openai-pf` on `legal_de`: scored on **40/150 docs** (deterministic subsample — metrics above are over those 40 docs only, not the full corpus).
> - `openai-pf` on `mapa_de`: scored on **40/558 docs** (deterministic subsample — metrics above are over those 40 docs only, not the full corpus).

### Severity-weighted leak rate · lower is better

Each leaked span weighted by compliance tier — direct identifiers (PERSON, EMAIL, PHONE, ADDRESS, DOB) = 5, high-stakes IDs (SSN/MRN/IBAN) = 10, quasi-identifiers (LOCATION, ORG, PROFESSION) = 1. Defaults in `label_map.yaml::severity`. Shown only because at least one cell here moves >3pp from raw leak; otherwise the two tables tracked within noise.

| Corpus | `anonde-patterns` | `anonde-ner` | `presidio` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|
| `legal_de` | 5.1% | **0.5%** 🥇 | – | 33.3% | 17.1% |
| `mapa_de` | 36.4% | **5.5%** 🥇 | – | 8.4% | 68.8% |

## Legal / administrative · Spanish

Corpora in this group: `mapa_es`.

### Leak rate · lower is better

A gold PHI span is *leaked* when **no** predicted span overlaps it — 'did we miss a name?'

| Corpus | `anonde-patterns` | `anonde-ner` | `presidio` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|
| `mapa_es` | 100.0% | **6.5%** 🥇 | – | 18.3% | 93.8% |

> **Partial coverage** — some engines were benchmarked on a fixed subsample, not every gold doc:
>
> - `openai-pf` on `mapa_es`: scored on **40/155 docs** (deterministic subsample — metrics above are over those 40 docs only, not the full corpus).

## Legal / administrative · French

Corpora in this group: `mapa_fr`.

### Leak rate · lower is better

A gold PHI span is *leaked* when **no** predicted span overlaps it — 'did we miss a name?'

| Corpus | `anonde-patterns` | `anonde-ner` | `presidio` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|
| `mapa_fr` | 75.2% | **11.4%** 🥇 | – | 16.3% | 95.5% |

> **Partial coverage** — some engines were benchmarked on a fixed subsample, not every gold doc:
>
> - `openai-pf` on `mapa_fr`: scored on **40/490 docs** (deterministic subsample — metrics above are over those 40 docs only, not the full corpus).

### Severity-weighted leak rate · lower is better

Each leaked span weighted by compliance tier — direct identifiers (PERSON, EMAIL, PHONE, ADDRESS, DOB) = 5, high-stakes IDs (SSN/MRN/IBAN) = 10, quasi-identifiers (LOCATION, ORG, PROFESSION) = 1. Defaults in `label_map.yaml::severity`. Shown only because at least one cell here moves >3pp from raw leak; otherwise the two tables tracked within noise.

| Corpus | `anonde-patterns` | `anonde-ner` | `presidio` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|
| `mapa_fr` | 60.3% | **8.9%** 🥇 | – | 11.2% | 98.5% |

## Legal / administrative · Italian

Corpora in this group: `mapa_it`.

### Leak rate · lower is better

A gold PHI span is *leaked* when **no** predicted span overlaps it — 'did we miss a name?'

| Corpus | `anonde-patterns` | `anonde-ner` | `presidio` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|
| `mapa_it` | 40.4% | **5.5%** 🥇 | – | 6.2% | 50.0% |

> **Partial coverage** — some engines were benchmarked on a fixed subsample, not every gold doc:
>
> - `openai-pf` on `mapa_it`: scored on **40/550 docs** (deterministic subsample — metrics above are over those 40 docs only, not the full corpus).

## Retail finance · English

Corpora in this group: `synth_finance_en`.

### Leak rate · lower is better

A gold PHI span is *leaked* when **no** predicted span overlaps it — 'did we miss a name?'

| Corpus | `anonde-patterns` | `anonde-ner` | `presidio` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|
| `synth_finance_en` | 30.0% | **2.9%** 🥇 | – | 20.6% | 18.5% |

> **Partial coverage** — some engines were benchmarked on a fixed subsample, not every gold doc:
>
> - `openai-pf` on `synth_finance_en`: scored on **40/120 docs** (deterministic subsample — metrics above are over those 40 docs only, not the full corpus).

### Severity-weighted leak rate · lower is better

Each leaked span weighted by compliance tier — direct identifiers (PERSON, EMAIL, PHONE, ADDRESS, DOB) = 5, high-stakes IDs (SSN/MRN/IBAN) = 10, quasi-identifiers (LOCATION, ORG, PROFESSION) = 1. Defaults in `label_map.yaml::severity`. Shown only because at least one cell here moves >3pp from raw leak; otherwise the two tables tracked within noise.

| Corpus | `anonde-patterns` | `anonde-ner` | `presidio` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|
| `synth_finance_en` | 18.9% | **2.1%** 🥇 | – | 22.7% | 6.0% |

## Retail finance · German

Corpora in this group: `finance_de`, `synth_finance_de`.

### Leak rate · lower is better

A gold PHI span is *leaked* when **no** predicted span overlaps it — 'did we miss a name?'

| Corpus | `anonde-patterns` | `anonde-ner` | `presidio` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|
| `finance_de` | 3.2% | **1.2%** 🥇 | – | 26.2% | 20.7% |
| `synth_finance_de` | **3.8%** 🥇 | 4.2% | – | 17.2% | 22.2% |

> **Partial coverage** — some engines were benchmarked on a fixed subsample, not every gold doc:
>
> - `openai-pf` on `finance_de`: scored on **40/150 docs** (deterministic subsample — metrics above are over those 40 docs only, not the full corpus).
> - `openai-pf` on `synth_finance_de`: scored on **40/120 docs** (deterministic subsample — metrics above are over those 40 docs only, not the full corpus).

### Severity-weighted leak rate · lower is better

Each leaked span weighted by compliance tier — direct identifiers (PERSON, EMAIL, PHONE, ADDRESS, DOB) = 5, high-stakes IDs (SSN/MRN/IBAN) = 10, quasi-identifiers (LOCATION, ORG, PROFESSION) = 1. Defaults in `label_map.yaml::severity`. Shown only because at least one cell here moves >3pp from raw leak; otherwise the two tables tracked within noise.

| Corpus | `anonde-patterns` | `anonde-ner` | `presidio` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|
| `finance_de` | 3.3% | **0.8%** 🥇 | – | 29.2% | 17.1% |
| `synth_finance_de` | 6.2% | **4.9%** 🥇 | – | 17.2% | 9.9% |

## Retail finance · Spanish

Corpora in this group: `synth_finance_es`.

### Leak rate · lower is better

A gold PHI span is *leaked* when **no** predicted span overlaps it — 'did we miss a name?'

| Corpus | `anonde-patterns` | `anonde-ner` | `presidio` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|
| `synth_finance_es` | 46.4% | **16.4%** 🥇 | – | 25.4% | 17.4% |

> **Partial coverage** — some engines were benchmarked on a fixed subsample, not every gold doc:
>
> - `openai-pf` on `synth_finance_es`: scored on **40/120 docs** (deterministic subsample — metrics above are over those 40 docs only, not the full corpus).

### Severity-weighted leak rate · lower is better

Each leaked span weighted by compliance tier — direct identifiers (PERSON, EMAIL, PHONE, ADDRESS, DOB) = 5, high-stakes IDs (SSN/MRN/IBAN) = 10, quasi-identifiers (LOCATION, ORG, PROFESSION) = 1. Defaults in `label_map.yaml::severity`. Shown only because at least one cell here moves >3pp from raw leak; otherwise the two tables tracked within noise.

| Corpus | `anonde-patterns` | `anonde-ner` | `presidio` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|
| `synth_finance_es` | 37.2% | 16.2% | – | 28.8% | **4.7%** 🥇 |

## Retail finance · French

Corpora in this group: `synth_finance_fr`.

### Leak rate · lower is better

A gold PHI span is *leaked* when **no** predicted span overlaps it — 'did we miss a name?'

| Corpus | `anonde-patterns` | `anonde-ner` | `presidio` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|
| `synth_finance_fr` | 44.6% | **15.9%** 🥇 | – | 23.9% | 19.3% |

> **Partial coverage** — some engines were benchmarked on a fixed subsample, not every gold doc:
>
> - `openai-pf` on `synth_finance_fr`: scored on **40/120 docs** (deterministic subsample — metrics above are over those 40 docs only, not the full corpus).

### Severity-weighted leak rate · lower is better

Each leaked span weighted by compliance tier — direct identifiers (PERSON, EMAIL, PHONE, ADDRESS, DOB) = 5, high-stakes IDs (SSN/MRN/IBAN) = 10, quasi-identifiers (LOCATION, ORG, PROFESSION) = 1. Defaults in `label_map.yaml::severity`. Shown only because at least one cell here moves >3pp from raw leak; otherwise the two tables tracked within noise.

| Corpus | `anonde-patterns` | `anonde-ner` | `presidio` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|
| `synth_finance_fr` | 35.1% | 13.5% | – | 26.4% | **6.7%** 🥇 |

## Retail finance · Italian

Corpora in this group: `synth_finance_it`.

### Leak rate · lower is better

A gold PHI span is *leaked* when **no** predicted span overlaps it — 'did we miss a name?'

| Corpus | `anonde-patterns` | `anonde-ner` | `presidio` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|
| `synth_finance_it` | 39.5% | **11.7%** 🥇 | – | 26.4% | 14.1% |

> **Partial coverage** — some engines were benchmarked on a fixed subsample, not every gold doc:
>
> - `openai-pf` on `synth_finance_it`: scored on **40/120 docs** (deterministic subsample — metrics above are over those 40 docs only, not the full corpus).

### Severity-weighted leak rate · lower is better

Each leaked span weighted by compliance tier — direct identifiers (PERSON, EMAIL, PHONE, ADDRESS, DOB) = 5, high-stakes IDs (SSN/MRN/IBAN) = 10, quasi-identifiers (LOCATION, ORG, PROFESSION) = 1. Defaults in `label_map.yaml::severity`. Shown only because at least one cell here moves >3pp from raw leak; otherwise the two tables tracked within noise.

| Corpus | `anonde-patterns` | `anonde-ner` | `presidio` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|
| `synth_finance_it` | 30.5% | 9.7% | – | 23.8% | **4.1%** 🥇 |

## Enterprise logs · English

Corpora in this group: `synth_logs`.

### Leak rate · lower is better

A gold PHI span is *leaked* when **no** predicted span overlaps it — 'did we miss a name?'

| Corpus | `anonde-patterns` | `anonde-ner` | `presidio` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|
| `synth_logs` | 29.5% | **13.3%** 🥇 | – | 73.2% | 15.1% |

> **Partial coverage** — some engines were benchmarked on a fixed subsample, not every gold doc:
>
> - `openai-pf` on `synth_logs`: scored on **40/120 docs** (deterministic subsample — metrics above are over those 40 docs only, not the full corpus).

### Severity-weighted leak rate · lower is better

Each leaked span weighted by compliance tier — direct identifiers (PERSON, EMAIL, PHONE, ADDRESS, DOB) = 5, high-stakes IDs (SSN/MRN/IBAN) = 10, quasi-identifiers (LOCATION, ORG, PROFESSION) = 1. Defaults in `label_map.yaml::severity`. Shown only because at least one cell here moves >3pp from raw leak; otherwise the two tables tracked within noise.

| Corpus | `anonde-patterns` | `anonde-ner` | `presidio` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|
| `synth_logs` | 35.0% | 16.4% | – | 79.7% | **7.3%** 🥇 |

## General structured PII · English

Corpora in this group: `ai4privacy_en`.

### Leak rate · lower is better

A gold PHI span is *leaked* when **no** predicted span overlaps it — 'did we miss a name?'

| Corpus | `anonde-patterns` | `anonde-ner` | `presidio` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|
| `ai4privacy_en` | 51.4% | **13.5%** 🥇 | – | 35.0% | 15.5% |

> **Partial coverage** — some engines were benchmarked on a fixed subsample, not every gold doc:
>
> - `openai-pf` on `ai4privacy_en`: scored on **40/1000 docs** (deterministic subsample — metrics above are over those 40 docs only, not the full corpus).

### Severity-weighted leak rate · lower is better

Each leaked span weighted by compliance tier — direct identifiers (PERSON, EMAIL, PHONE, ADDRESS, DOB) = 5, high-stakes IDs (SSN/MRN/IBAN) = 10, quasi-identifiers (LOCATION, ORG, PROFESSION) = 1. Defaults in `label_map.yaml::severity`. Shown only because at least one cell here moves >3pp from raw leak; otherwise the two tables tracked within noise.

| Corpus | `anonde-patterns` | `anonde-ner` | `presidio` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|
| `ai4privacy_en` | 41.8% | 11.0% | – | 37.0% | **10.0%** 🥇 |

## General structured PII · German

Corpora in this group: `ai4privacy_de`.

### Leak rate · lower is better

A gold PHI span is *leaked* when **no** predicted span overlaps it — 'did we miss a name?'

| Corpus | `anonde-patterns` | `anonde-ner` | `presidio` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|
| `ai4privacy_de` | 59.8% | **10.7%** 🥇 | – | 33.0% | 23.6% |

> **Partial coverage** — some engines were benchmarked on a fixed subsample, not every gold doc:
>
> - `openai-pf` on `ai4privacy_de`: scored on **40/1000 docs** (deterministic subsample — metrics above are over those 40 docs only, not the full corpus).

### Severity-weighted leak rate · lower is better

Each leaked span weighted by compliance tier — direct identifiers (PERSON, EMAIL, PHONE, ADDRESS, DOB) = 5, high-stakes IDs (SSN/MRN/IBAN) = 10, quasi-identifiers (LOCATION, ORG, PROFESSION) = 1. Defaults in `label_map.yaml::severity`. Shown only because at least one cell here moves >3pp from raw leak; otherwise the two tables tracked within noise.

| Corpus | `anonde-patterns` | `anonde-ner` | `presidio` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|
| `ai4privacy_de` | 63.9% | **9.7%** 🥇 | – | 34.7% | 16.3% |

## General structured PII · Spanish

Corpora in this group: `ai4privacy_es`.

### Leak rate · lower is better

A gold PHI span is *leaked* when **no** predicted span overlaps it — 'did we miss a name?'

| Corpus | `anonde-patterns` | `anonde-ner` | `presidio` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|
| `ai4privacy_es` | 68.9% | **14.2%** 🥇 | – | 34.6% | 17.5% |

> **Partial coverage** — some engines were benchmarked on a fixed subsample, not every gold doc:
>
> - `openai-pf` on `ai4privacy_es`: scored on **40/1000 docs** (deterministic subsample — metrics above are over those 40 docs only, not the full corpus).

### Severity-weighted leak rate · lower is better

Each leaked span weighted by compliance tier — direct identifiers (PERSON, EMAIL, PHONE, ADDRESS, DOB) = 5, high-stakes IDs (SSN/MRN/IBAN) = 10, quasi-identifiers (LOCATION, ORG, PROFESSION) = 1. Defaults in `label_map.yaml::severity`. Shown only because at least one cell here moves >3pp from raw leak; otherwise the two tables tracked within noise.

| Corpus | `anonde-patterns` | `anonde-ner` | `presidio` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|
| `ai4privacy_es` | 68.7% | 11.2% | – | 35.2% | **7.6%** 🥇 |

## General structured PII · French

Corpora in this group: `ai4privacy_fr`.

### Leak rate · lower is better

A gold PHI span is *leaked* when **no** predicted span overlaps it — 'did we miss a name?'

| Corpus | `anonde-patterns` | `anonde-ner` | `presidio` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|
| `ai4privacy_fr` | 68.9% | **14.2%** 🥇 | – | 34.9% | 16.3% |

> **Partial coverage** — some engines were benchmarked on a fixed subsample, not every gold doc:
>
> - `openai-pf` on `ai4privacy_fr`: scored on **40/1000 docs** (deterministic subsample — metrics above are over those 40 docs only, not the full corpus).

### Severity-weighted leak rate · lower is better

Each leaked span weighted by compliance tier — direct identifiers (PERSON, EMAIL, PHONE, ADDRESS, DOB) = 5, high-stakes IDs (SSN/MRN/IBAN) = 10, quasi-identifiers (LOCATION, ORG, PROFESSION) = 1. Defaults in `label_map.yaml::severity`. Shown only because at least one cell here moves >3pp from raw leak; otherwise the two tables tracked within noise.

| Corpus | `anonde-patterns` | `anonde-ner` | `presidio` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|
| `ai4privacy_fr` | 67.1% | 11.7% | – | 35.8% | **9.3%** 🥇 |

## General structured PII · Italian

Corpora in this group: `ai4privacy_it`.

### Leak rate · lower is better

A gold PHI span is *leaked* when **no** predicted span overlaps it — 'did we miss a name?'

| Corpus | `anonde-patterns` | `anonde-ner` | `presidio` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|
| `ai4privacy_it` | 63.9% | **15.0%** 🥇 | – | 36.0% | 21.2% |

> **Partial coverage** — some engines were benchmarked on a fixed subsample, not every gold doc:
>
> - `openai-pf` on `ai4privacy_it`: scored on **40/1000 docs** (deterministic subsample — metrics above are over those 40 docs only, not the full corpus).

### Severity-weighted leak rate · lower is better

Each leaked span weighted by compliance tier — direct identifiers (PERSON, EMAIL, PHONE, ADDRESS, DOB) = 5, high-stakes IDs (SSN/MRN/IBAN) = 10, quasi-identifiers (LOCATION, ORG, PROFESSION) = 1. Defaults in `label_map.yaml::severity`. Shown only because at least one cell here moves >3pp from raw leak; otherwise the two tables tracked within noise.

| Corpus | `anonde-patterns` | `anonde-ner` | `presidio` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|
| `ai4privacy_it` | 58.9% | **11.5%** 🥇 | – | 36.6% | 13.6% |

## Academic NER (newswire / social) · English

Corpora in this group: `conll2003_en`, `wnut_17`.

### Leak rate · lower is better

A gold PHI span is *leaked* when **no** predicted span overlaps it — 'did we miss a name?'

| Corpus | `anonde-patterns` | `anonde-ner` | `presidio` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|
| `conll2003_en` | 96.7% | **4.7%** 🥇 | – | 6.0% | 64.0% |
| `wnut_17` | 80.9% | **11.2%** 🥇 | – | 25.5% | 82.1% |

> **Partial coverage** — some engines were benchmarked on a fixed subsample, not every gold doc:
>
> - `openai-pf` on `conll2003_en`: scored on **40/300 docs** (deterministic subsample — metrics above are over those 40 docs only, not the full corpus).
> - `openai-pf` on `wnut_17`: scored on **40/300 docs** (deterministic subsample — metrics above are over those 40 docs only, not the full corpus).

### Severity-weighted leak rate · lower is better

Each leaked span weighted by compliance tier — direct identifiers (PERSON, EMAIL, PHONE, ADDRESS, DOB) = 5, high-stakes IDs (SSN/MRN/IBAN) = 10, quasi-identifiers (LOCATION, ORG, PROFESSION) = 1. Defaults in `label_map.yaml::severity`. Shown only because at least one cell here moves >3pp from raw leak; otherwise the two tables tracked within noise.

| Corpus | `anonde-patterns` | `anonde-ner` | `presidio` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|
| `conll2003_en` | 97.0% | **5.2%** 🥇 | – | 6.9% | 47.9% |
| `wnut_17` | 74.6% | **6.7%** 🥇 | – | 25.4% | 61.5% |

## Academic NER (newswire / social) · German

Corpora in this group: `wikiann_de`, `germeval_14`, `conll2003_de`.

### Leak rate · lower is better

A gold PHI span is *leaked* when **no** predicted span overlaps it — 'did we miss a name?'

| Corpus | `anonde-patterns` | `anonde-ner` | `presidio` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|
| `wikiann_de` | 41.9% | **2.7%** 🥇 | – | 12.8% | 59.6% |
| `germeval_14` | 55.0% | **8.4%** 🥇 | – | 18.9% | 94.6% |

> **Partial coverage** — some engines were benchmarked on a fixed subsample, not every gold doc:
>
> - `openai-pf` on `wikiann_de`: scored on **40/300 docs** (deterministic subsample — metrics above are over those 40 docs only, not the full corpus).
> - `openai-pf` on `germeval_14`: scored on **40/300 docs** (deterministic subsample — metrics above are over those 40 docs only, not the full corpus).

### Severity-weighted leak rate · lower is better

Each leaked span weighted by compliance tier — direct identifiers (PERSON, EMAIL, PHONE, ADDRESS, DOB) = 5, high-stakes IDs (SSN/MRN/IBAN) = 10, quasi-identifiers (LOCATION, ORG, PROFESSION) = 1. Defaults in `label_map.yaml::severity`. Shown only because at least one cell here moves >3pp from raw leak; otherwise the two tables tracked within noise.

| Corpus | `anonde-patterns` | `anonde-ner` | `presidio` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|
| `wikiann_de` | 30.3% | **1.8%** 🥇 | – | 10.7% | 44.3% |
| `germeval_14` | 47.5% | **5.9%** 🥇 | – | 16.1% | 88.8% |

## Adversarial / out-of-distribution · German

Corpora in this group: `adversarial_de`.

### Leak rate · lower is better

A gold PHI span is *leaked* when **no** predicted span overlaps it — 'did we miss a name?'

| Corpus | `anonde-patterns` | `anonde-ner` | `presidio` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|
| `adversarial_de` | 12.7% | **8.1%** 🥇 | – | 43.4% | 32.0% |

> **Partial coverage** — some engines were benchmarked on a fixed subsample, not every gold doc:
>
> - `openai-pf` on `adversarial_de`: scored on **40/300 docs** (deterministic subsample — metrics above are over those 40 docs only, not the full corpus).

### Severity-weighted leak rate · lower is better

Each leaked span weighted by compliance tier — direct identifiers (PERSON, EMAIL, PHONE, ADDRESS, DOB) = 5, high-stakes IDs (SSN/MRN/IBAN) = 10, quasi-identifiers (LOCATION, ORG, PROFESSION) = 1. Defaults in `label_map.yaml::severity`. Shown only because at least one cell here moves >3pp from raw leak; otherwise the two tables tracked within noise.

| Corpus | `anonde-patterns` | `anonde-ner` | `presidio` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|
| `adversarial_de` | 12.3% | **8.1%** 🥇 | – | 44.1% | 27.7% |

## Latency · per-document p50 / p95

Wall-clock per `engine.Analyze(doc)` call. p50 = steady-state, p95 = tail (the SLO knob). Mean + p99 in `results_matrix.csv`. One table across every corpus — latency tracks corpus length, not domain or language.

| Corpus | `anonde-patterns` p50 / p95 | `anonde-ner` p50 / p95 | `presidio` p50 / p95 | `gliner-py` p50 / p95 | `openai-pf` p50 / p95 |
|---|---:|---:|---:|---:|---:|
| `synth_clinical_en` | 2 ms / 3 ms | 208 ms / 320 ms | – | 417 ms / 521 ms | 423 ms / 474 ms |
| `openmed` | 8 ms / 24 ms | 1.0 s / 3.1 s | – | 1.5 s / 8.9 s | 2.5 s / 6.7 s |
| `synth_clinical` | 3 ms / 5 ms | 440 ms / 702 ms | – | 595 ms / 792 ms | 1.5 s / 2.2 s |
| `pharmaconer_es` | 4 ms / 9 ms | 985 ms / 2.2 s | – | 1.2 s / 3.1 s | 4.0 s / 8.6 s |
| `meddocan_es` | 5 ms / 9 ms | 1.3 s / 2.5 s | – | 1.6 s / 3.7 s | 4.9 s / 8.2 s |
| `synth_clinical_fr` | 2 ms / 3 ms | 609 ms / 808 ms | – | 646 ms / 881 ms | 1.8 s / 2.6 s |
| `synth_clinical_it` | 2 ms / 3 ms | 471 ms / 817 ms | – | 673 ms / 915 ms | 2.1 s / 2.9 s |
| `mapa_en` | 1 ms / 2 ms | 104 ms / 260 ms | – | 172 ms / 329 ms | 197 ms / 472 ms |
| `legal_de` | 2 ms / 3 ms | 161 ms / 210 ms | – | 355 ms / 447 ms | 362 ms / 429 ms |
| `mapa_de` | 0 ms / 1 ms | 107 ms / 210 ms | – | 177 ms / 272 ms | 215 ms / 347 ms |
| `mapa_es` | 1 ms / 1 ms | 125 ms / 334 ms | – | 207 ms / 401 ms | 344 ms / 793 ms |
| `mapa_fr` | 0 ms / 1 ms | 50 ms / 105 ms | – | 152 ms / 256 ms | 154 ms / 264 ms |
| `mapa_it` | 0 ms / 1 ms | 113 ms / 211 ms | – | 184 ms / 280 ms | 230 ms / 376 ms |
| `synth_finance_en` | 2 ms / 3 ms | 289 ms / 483 ms | – | 356 ms / 550 ms | 755 ms / 929 ms |
| `finance_de` | 2 ms / 4 ms | 425 ms / 632 ms | – | 486 ms / 680 ms | 2.1 s / 2.8 s |
| `synth_finance_de` | 2 ms / 3 ms | 324 ms / 484 ms | – | 396 ms / 616 ms | 914 ms / 1.4 s |
| `synth_finance_es` | 1 ms / 2 ms | 345 ms / 489 ms | – | 413 ms / 642 ms | 856 ms / 1.2 s |
| `synth_finance_fr` | 1 ms / 3 ms | 355 ms / 480 ms | – | 425 ms / 639 ms | 1.0 s / 1.3 s |
| `synth_finance_it` | 1 ms / 2 ms | 342 ms / 448 ms | – | 394 ms / 615 ms | 429 ms / 506 ms |
| `synth_logs` | 4 ms / 9 ms | 596 ms / 1.1 s | – | 890 ms / 2.9 s | 2.3 s / 3.5 s |
| `ai4privacy_en` | 1 ms / 1 ms | 204 ms / 304 ms | – | 279 ms / 384 ms | 337 ms / 396 ms |
| `ai4privacy_de` | 1 ms / 2 ms | 255 ms / 364 ms | – | 366 ms / 484 ms | 634 ms / 828 ms |
| `ai4privacy_es` | 1 ms / 1 ms | 233 ms / 339 ms | – | 321 ms / 432 ms | 707 ms / 813 ms |
| `ai4privacy_fr` | 1 ms / 1 ms | 239 ms / 330 ms | – | 328 ms / 421 ms | 637 ms / 838 ms |
| `ai4privacy_it` | 1 ms / 1 ms | 233 ms / 335 ms | – | 320 ms / 429 ms | 700 ms / 913 ms |
| `conll2003_en` | 0 ms / 1 ms | 67 ms / 116 ms | – | 150 ms / 199 ms | 163 ms / 331 ms |
| `wnut_17` | 0 ms / 1 ms | 36 ms / 67 ms | – | 126 ms / 173 ms | 103 ms / 147 ms |
| `wikiann_de` | 0 ms / 1 ms | 67 ms / 105 ms | – | 154 ms / 192 ms | 125 ms / 299 ms |
| `germeval_14` | 0 ms / 1 ms | 38 ms / 58 ms | – | 130 ms / 165 ms | 95 ms / 147 ms |
| `adversarial_de` | 3 ms / 4 ms | 359 ms / 530 ms | – | 478 ms / 869 ms | 1.0 s / 1.2 s |

<details><summary>Cost reference · USD per million characters</summary>

## Cost reference · USD per million characters

All engines in this matrix run on your hardware — no per-call charge. For procurement context, here is what the closest managed-service alternatives cost on their public pricing pages (verified 2026-05-15; vendor pricing drifts, re-check before quoting):

| Engine | Hosting | $/M chars | Notes |
|---|---|---:|---|
| `anonde-patterns` | self-host (small commodity VM) | ~**$0.0005** | Patterns-only; runs on ~256 MB RAM. Amortised cost dominated by infra base. |
| `anonde-ner` | self-host (~2 GB RAM VM) | ~**$0.001** | GLiNER PII baked into image. ~2 GB RAM is enough; CPU-only, runs on any commodity cloud VM. |
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
*Generated by `bench/scoring/render_matrix.py` over 120 cells. Full per-entity-type breakdown in `results_matrix.csv`.*
