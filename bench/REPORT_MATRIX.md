# 🛡️ anonde bench matrix

**`anonde-ner` — the default NER image — is the lowest-leak PII redactor in this benchmark: it misses just 11.1% of gold PII vs Presidio 41.8% / raw GLiNER 33.1% / OpenAI Privacy Filter 24.0%, across 29 gold-annotated corpora and 5 languages.** Tuned recall-first — it catches more PII than the precision-optimised tools, at the cost of more over-redaction (quantified two lines down).

| Language | `anonde-ner` ⬅ ours | `presidio` | `presidio-transformer` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|
| **English** | **9.8%** 🥇 | 36.5% | 38.1% | 39.8% | 19.5% |
| **German** | **6.4%** 🥇 | 38.3% | – | 31.9% | 28.8% |
| **Spanish** | **15.8%** 🥇 | 47.0% | – | 29.3% | 25.7% |
| **French** | **13.9%** 🥇 | 43.1% | – | 30.7% | 21.1% |
| **Italian** | **14.6%** 🥇 | 48.5% | – | 33.8% | 21.0% |
| **All** | **11.1%** 🥇 | **41.8%** | **38.1%** | **33.1%** | **24.0%** |

*The one table. **Leak rate** = fraction of gold PII spans **missed** — lower is better; 🥇 = lowest-leak engine in the row. Columns are the default NER image `anonde-ner` vs the competing field (anonde's own patterns / stack tiers and the per-domain roll-ups are under **Details**). `presidio-transformer` is EN-only (`–` elsewhere, by design); `openai-pf` is scored on a fixed per-corpus subsample. Full method, precision, and every slice are in **Details** below.*

| Language | `anonde-ner` ⬅ ours | `presidio` | `presidio-transformer` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|
| **English** | **0.563** 🥇 | 0.404 | 0.464 | 0.441 | 0.002 |
| **German** | **0.595** 🥇 | 0.385 | – | 0.479 | 0.006 |
| **Spanish** | **0.555** 🥇 | 0.345 | – | 0.511 | 0.000 |
| **French** | **0.682** 🥇 | 0.340 | – | 0.536 | 0.003 |
| **Italian** | **0.633** 🥇 | 0.320 | – | 0.480 | 0.004 |
| **All** | **0.599** 🥇 | **0.365** | **0.464** | **0.487** | **0.003** |

*The twin. **Strict F1** = exact span **and** type match (CoNLL) — higher is better, 🥇 = best in row. It reproduces the standard scorer (`nervaluate`) *exactly* (Δ≈0 in `verify_official.py`), so it is the citable accuracy metric alongside leak rate. It is precision-inclusive, so it also reflects over-redaction: an over-redacting tool can rank lower here than on leak rate, where a precision-first rival edges ahead. The lenient overlap view and full method are under **Details** / `METHODOLOGY.md`.*

> **The trade — over-redaction (1 − precision, lower is better):** `anonde-ner` 32.9% vs Presidio 41.8% / raw GLiNER 20.8%. anonde deliberately over-redacts more so it leaks less — the recall-first dial, not a defect. Full precision scorecard (with the zero-gold exclusion rule) is under Details.

---

## Details

The full working behind the scorecard above — leak-rate and precision roll-ups (per domain · per language · overall), the per-cell grids, latency, cost, caveats, and the glossary, in the order they were already in. The hero table is the answer; everything here is how it is computed.

> **TL;DR** — `anonde-ner` (the default NER image, `ghcr.io/anonde-io/anonde-ner`) is the lowest-leak engine on **28 of 29** gold-annotated corpora. Biggest absolute improvement over the best baseline: **+24.1pp** in leak rate. Strict F1 trades exact-byte alignment for catching more PHI — the right trade-off for a redactor, not a benchmark gaming exercise. The inverse cost — over-redaction / false positives — now has its own **Precision scorecard** directly below this leak-rate one (partial, overlap-based precision per engine, roll-up rows + a per-cell grid in the Detailed breakdown); read it to compare how much each engine over-redacts.

## 🎯 Scorecard · leak rate roll-ups

The one table. Roll-up rows only (per domain · per language · overall); the per-(domain × language) detail grid lives in the Detailed breakdown below. Each number is **leak rate** (fraction of gold PHI spans missed — lower is better). `anonde-ner` is the default NER image (`ghcr.io/anonde-io/anonde-ner`) and the anchor column; **Verdict** says whether it beats the field. 🥇 marks the lowest-leak engine in the row. Roll-up rows pool leaked-over-gold across the group (doc-weighted, so larger corpora count more).

| Slice | Scope | `anonde-ner` ⬅︎ anonde (default NER) | `anonde-patterns` | `presidio` | `presidio-transformer` | `gliner-py` | `openai-pf` | Verdict |
|---|---|---:|---:|---:|---:|---:|---:|:--:|
| **Σ ALL** | **all** | **11.1%** 🥇 | **46.1%** | **41.8%** | **38.1%** | **33.1%** | **24.0%** | ✅ |
| | | | | | | | | |
| _Σ Clinical / medical de-identification_ | _all langs_ | **11.3%** 🥇 | 47.5% | 31.1% | 15.2% | 28.0% | 27.3% | ✅ |
| _Σ Legal / administrative_ | _all langs_ | **2.8%** 🥇 | 23.1% | 29.6% | 13.3% | 21.5% | 36.8% | ✅ |
| _Σ Retail finance_ | _all langs_ | **8.2%** 🥇 | 24.4% | 21.3% | 12.0% | 23.5% | 19.0% | ✅ |
| _Σ Enterprise logs_ | _all langs_ | **13.2%** 🥇 | 28.9% | 31.5% | 37.3% | 73.2% | 15.3% | ✅ |
| _Σ General structured PII_ | _all langs_ | **13.4%** 🥇 | 62.1% | 57.8% | 57.4% | 34.7% | 18.8% | ✅ |
| _Σ Academic NER (newswire / social)_ | _all langs_ | **6.1%** 🥇 | 68.4% | 18.3% | 16.5% | 13.8% | 72.7% | ✅ |
| _Σ Adversarial / out-of-distribution_ | _all langs_ | **8.1%** 🥇 | 12.7% | 37.6% | – | 43.4% | 32.0% | ✅ |
| | | | | | | | | |
| _Σ all domains_ | _English_ | **9.8%** 🥇 | 35.6% | 36.5% | 38.1% | 39.8% | 19.5% | ✅ |
| _Σ all domains_ | _German_ | **6.4%** 🥇 | 25.0% | 38.3% | – | 31.9% | 28.8% | ✅ |
| _Σ all domains_ | _Spanish_ | **15.8%** 🥇 | 69.9% | 47.0% | – | 29.3% | 25.7% | ✅ |
| _Σ all domains_ | _French_ | **13.9%** 🥇 | 63.6% | 43.1% | – | 30.7% | 21.1% | ✅ |
| _Σ all domains_ | _Italian_ | **14.6%** 🥇 | 58.6% | 48.5% | – | 33.8% | 21.0% | ✅ |

> **Anonde scoreboard** — across the **24** populated `(domain, language)` cells in the matrix, `anonde-ner` is the **lowest-leak engine in 23**, ties in **0**, and is beaten in **1**. ✅ = anonde leads · 🟰 = tied · ❌ = a baseline leaks less. See the per-cell leak-rate grid in the Detailed breakdown below for which baseline wins where. (The TL;DR's win count is per-corpus, a finer split than these per-cell rows.)

> **EN-only column** — `presidio-transformer`: English corpora only. `presidio-transformer` is Presidio's `en_core_web_trf` config (an English transformer model), benchmarked next to the default `en_core_web_lg` `presidio` column so the report shows both Presidio configs on English. Non-EN cells render `–` **by design** — not a failed run — and the roll-up rows pool leak rate over the English corpora only (partial coverage, like a subsampled engine). Compare against the other engines on the English rows only.

## 🎯 Precision scorecard · false-positive rate roll-ups

Partial precision = fraction of redacted spans that overlap a real PII span; the inverse (**1 − precision**) is the over-redaction / false-positive rate. **Higher is better.** This is the overlap-based *partial* view (a predicted span counts as a true positive if it overlaps **any** gold span), NOT the strict byte-exact view — strict punishes a one-char offset as a full false positive and reads misleadingly low (~0.1–0.3) for every engine including the baselines, so it is the wrong headline for a redactor. The leak-rate scorecard above answers recall ("did we miss real PII?"); this one answers the inverse cost — over-redaction. Same structure as the leak scorecard: roll-up rows only (per domain · per language · overall), `anonde-ner` anchored first. 🥇 marks the highest-precision engine in the row. Each cell pools tp/(tp+fp) across the group (micro-average, doc-weighted) and annotates the pooled raw FP count — precision can look fine while absolute false-positive volume is high.

| Slice | Scope | `anonde-ner` ⬅︎ anonde (default NER) | `anonde-patterns` | `presidio` | `presidio-transformer` | `gliner-py` | `openai-pf` |
|---|---|---:|---:|---:|---:|---:|---:|
| **Σ ALL** | **all** | **67.1% (31924 fp)** | **76.9% (12287 fp)** | **58.2% (26886 fp)** | **74.2% (2969 fp)** | **79.2% (12753 fp)** 🥇 | **75.2% (4690 fp)** |
| | | | | | | | |
| _Σ Clinical / medical de-identification_ | _all langs_ | 60.9% (8527 fp) | 72.0% (3197 fp) | 53.2% (8508 fp) | 85.8% (303 fp) | **88.5% (1491 fp)** 🥇 | 88.4% (736 fp) |
| _Σ Legal / administrative_ | _all langs_ | 43.2% (4153 fp) | **81.4% (587 fp)** 🥇 | 47.4% (2242 fp) | 25.7% (465 fp) | 45.5% (3315 fp) | 75.9% (228 fp) |
| _Σ Retail finance_ | _all langs_ | 83.2% (2275 fp) | 84.9% (1649 fp) | 79.4% (2391 fp) | **93.1% (123 fp)** 🥇 | 92.0% (957 fp) | 83.5% (877 fp) |
| _Σ Enterprise logs_ | _all langs_ | 73.2% (1034 fp) | **83.7% (502 fp)** 🥇 | 57.3% (1554 fp) | 59.5% (1418 fp) | 75.9% (243 fp) | 30.2% (2497 fp) |
| _Σ General structured PII_ | _all langs_ | 67.8% (13376 fp) | 71.0% (5199 fp) | 54.5% (8861 fp) | 81.8% (527 fp) | 78.3% (5131 fp) | **93.1% (113 fp)** 🥇 |
| _Σ Academic NER (newswire / social)_ | _all langs_ | 38.6% (1479 fp) | 41.3% (334 fp) | 71.5% (378 fp) | **77.6% (133 fp)** 🥇 | 57.9% (829 fp) | 70.0% (36 fp) |
| _Σ Adversarial / out-of-distribution_ | _all langs_ | 83.2% (1080 fp) | **86.5% (819 fp)** 🥇 | 49.5% (2952 fp) | – | 80.0% (787 fp) | 80.4% (203 fp) |
| | | | | | | | |
| _Σ all domains_ | _English_ | 56.5% (9568 fp) | 57.2% (6800 fp) | 65.2% (4281 fp) | 74.2% (2969 fp) | **79.4% (2240 fp)** 🥇 | 53.4% (2694 fp) |
| _Σ all domains_ | _German_ | 68.9% (10123 fp) | 79.9% (4623 fp) | 58.8% (9013 fp) | – | 79.3% (4336 fp) | **85.7% (932 fp)** 🥇 |
| _Σ all domains_ | _Spanish_ | 68.0% (5187 fp) | **97.1% (127 fp)** 🥇 | 51.6% (5756 fp) | – | 80.9% (2131 fp) | 76.5% (504 fp) |
| _Σ all domains_ | _French_ | 75.9% (3080 fp) | **95.8% (191 fp)** 🥇 | 56.5% (4116 fp) | – | 81.0% (1782 fp) | 87.5% (270 fp) |
| _Σ all domains_ | _Italian_ | 70.3% (3966 fp) | **89.7% (546 fp)** 🥇 | 57.7% (3720 fp) | – | 75.1% (2264 fp) | 87.5% (290 fp) |

> **Reading this table** — a cell of `92.0% (40 fp)` means 92% of the spans that engine redacted overlapped real PII; the remaining 8% (40 absolute spans) were over-redaction. Recall (leak rate) is in the scorecard above; this is the other half of the trade-off.

> **Why some predictions are not counted** — a `(corpus, entity-type)` cell where the gold annotates **zero** spans of that type (`tp + fn == 0`) is *unscoreable for precision*: with no gold of that type present, every prediction there is mechanically a false positive against absent gold — a **schema gap** (e.g. a corpus that annotates PERSON but not LOCATION), not real over-redaction. Such cells are **excluded** from the precision pool above, and an empty-gold corpus (every type zero-gold) drops out entirely. This is a scorecard-aggregation choice only — the raw per-type counts stay intact in `results_matrix.csv`, and **leak-rate / recall are untouched** (they score against the full gold). Excluded here: **125 (corpus, type) cells** across **1 empty-gold corpora**. For full transparency, the raw Σ ALL precision *including* every zero-gold cell: `anonde-ner` 60.2% (42906 fp) · `anonde-patterns` 73.9% (14435 fp) · `presidio` 47.9% (40742 fp) · `presidio-transformer` 59.7% (5768 fp) · `gliner-py` 67.5% (23463 fp) · `openai-pf` 69.6% (6213 fp).

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

| Domain | Language | `anonde-ner` ⬅︎ anonde (default NER) | `anonde-patterns` | `presidio` | `presidio-transformer` | `gliner-py` | `openai-pf` | Verdict |
|---|---|---:|---:|---:|---:|---:|---:|:--:|
| **Clinical / medical de-identification** | English | **1.6%** 🥇 | 8.0% | 20.3% | 15.2% | 23.3% | 24.6% | ✅ |
| **Clinical / medical de-identification** | German | **4.6%** 🥇 | 9.0% | 30.9% | – | 34.2% | 29.9% | ✅ |
| **Clinical / medical de-identification** | Spanish | **17.8%** 🥇 | 79.6% | 38.4% | – | 23.7% | 31.6% | ✅ |
| **Clinical / medical de-identification** | French | **11.5%** 🥇 | 61.4% | 28.2% | – | 25.3% | 21.8% | ✅ |
| **Clinical / medical de-identification** | Italian | **16.2%** 🥇 | 59.7% | 28.5% | – | 35.2% | 25.1% | ✅ |
| **Legal / administrative** | English | 8.4% | 48.9% | **6.2%** 🥇 | 13.3% | 10.7% | 76.9% | ❌ |
| **Legal / administrative** | German | **1.1%** 🥇 | 11.4% | 26.7% | – | 23.8% | 32.9% | ✅ |
| **Legal / administrative** | Spanish | **6.5%** 🥇 | 100.0% | 59.1% | – | 18.3% | 93.8% | ✅ |
| **Legal / administrative** | French | **11.4%** 🥇 | 75.2% | 44.9% | – | 16.3% | 95.5% | ✅ |
| **Legal / administrative** | Italian | **5.5%** 🥇 | 40.4% | 67.1% | – | 6.2% | 50.0% | ✅ |
| **Retail finance** | English | **2.8%** 🥇 | 21.1% | 10.3% | 12.0% | 20.6% | 18.5% | ✅ |
| **Retail finance** | German | **2.6%** 🥇 | 3.4% | 24.1% | – | 22.6% | 21.3% | ✅ |
| **Retail finance** | Spanish | **16.4%** 🥇 | 46.4% | 18.5% | – | 25.4% | 17.4% | ✅ |
| **Retail finance** | French | **15.9%** 🥇 | 44.6% | 19.4% | – | 23.9% | 19.3% | ✅ |
| **Retail finance** | Italian | **11.7%** 🥇 | 39.5% | 29.6% | – | 26.4% | 14.1% | ✅ |
| **Enterprise logs** | English | **13.2%** 🥇 | 28.9% | 31.5% | 37.3% | 73.2% | 15.3% | ✅ |
| **General structured PII** | English | **13.0%** 🥇 | 47.8% | 56.0% | 57.4% | 35.0% | 15.5% | ✅ |
| **General structured PII** | German | **10.7%** 🥇 | 59.8% | 58.4% | – | 33.0% | 23.6% | ✅ |
| **General structured PII** | Spanish | **14.2%** 🥇 | 68.9% | 61.0% | – | 34.6% | 17.5% | ✅ |
| **General structured PII** | French | **14.2%** 🥇 | 68.9% | 54.0% | – | 34.9% | 16.3% | ✅ |
| **General structured PII** | Italian | **15.0%** 🥇 | 63.9% | 59.6% | – | 36.0% | 21.2% | ✅ |
| **Academic NER (newswire / social)** | English | **6.6%** 🥇 | 92.0% | 17.4% | 16.5% | 11.8% | 71.9% | ✅ |
| **Academic NER (newswire / social)** | German | **5.8%** 🥇 | 47.7% | 19.1% | – | 15.5% | 73.4% | ✅ |
| **Adversarial / out-of-distribution** | German | **8.1%** 🥇 | 12.7% | 37.6% | – | 43.4% | 32.0% | ✅ |

## Per-cell precision · domain × language

Detail behind the precision scorecard: one row per populated `(domain, language)` cell, partial precision pooled across the cell's corpora (raw FP count annotated). Higher is better; 🥇 marks the highest-precision engine in the row. Strict byte-exact precision per entity type stays in `results_matrix.csv`.

| Domain | Language | `anonde-ner` ⬅︎ anonde (default NER) | `anonde-patterns` | `presidio` | `presidio-transformer` | `gliner-py` | `openai-pf` |
|---|---|---:|---:|---:|---:|---:|---:|
| **Clinical / medical de-identification** | English | 51.9% (2035 fp) | 51.2% (1929 fp) | 78.3% (458 fp) | 85.8% (303 fp) | 93.9% (126 fp) | **99.1% (9 fp)** 🥇 |
| **Clinical / medical de-identification** | German | 53.2% (2905 fp) | 74.2% (1158 fp) | 44.6% (2796 fp) | – | **87.7% (334 fp)** 🥇 | 87.5% (273 fp) |
| **Clinical / medical de-identification** | Spanish | 58.2% (2759 fp) | **98.5% (16 fp)** 🥇 | 44.0% (3307 fp) | – | 82.1% (806 fp) | 69.8% (313 fp) |
| **Clinical / medical de-identification** | French | 82.8% (421 fp) | **97.4% (24 fp)** 🥇 | 59.4% (966 fp) | – | 97.0% (58 fp) | 92.1% (83 fp) |
| **Clinical / medical de-identification** | Italian | 82.4% (407 fp) | 92.9% (70 fp) | 64.2% (981 fp) | – | 90.5% (167 fp) | **94.5% (58 fp)** 🥇 |
| **Legal / administrative** | English | 17.0% (767 fp) | 58.6% (67 fp) | 18.9% (693 fp) | 25.7% (465 fp) | 21.7% (637 fp) | **100.0% (0 fp)** 🥇 |
| **Legal / administrative** | German | 65.9% (1344 fp) | **81.7% (518 fp)** 🥇 | 67.0% (842 fp) | – | 65.8% (1135 fp) | 78.9% (189 fp) |
| **Legal / administrative** | Spanish | 18.3% (259 fp) | – | 6.5% (145 fp) | – | **23.6% (194 fp)** 🥇 | 11.1% (16 fp) |
| **Legal / administrative** | French | 22.2% (809 fp) | **98.8% (1 fp)** 🥇 | 35.6% (224 fp) | – | 28.0% (582 fp) | 0.0% (8 fp) |
| **Legal / administrative** | Italian | 10.0% (974 fp) | **98.8% (1 fp)** 🥇 | 5.1% (338 fp) | – | 13.2% (767 fp) | 28.6% (15 fp) |
| **Retail finance** | English | 69.3% (807 fp) | 63.0% (833 fp) | 80.7% (398 fp) | **93.1% (123 fp)** 🥇 | 91.7% (161 fp) | 82.1% (137 fp) |
| **Retail finance** | German | 80.2% (1121 fp) | 85.1% (785 fp) | 78.9% (891 fp) | – | **90.0% (460 fp)** 🥇 | 88.6% (238 fp) |
| **Retail finance** | Spanish | 93.9% (102 fp) | **99.2% (8 fp)** 🥇 | 78.8% (397 fp) | – | 94.5% (96 fp) | 80.1% (159 fp) |
| **Retail finance** | French | 94.8% (92 fp) | **100.0% (0 fp)** 🥇 | 82.8% (317 fp) | – | 93.5% (122 fp) | 80.3% (161 fp) |
| **Retail finance** | Italian | 91.8% (153 fp) | **98.1% (23 fp)** 🥇 | 76.0% (388 fp) | – | 93.2% (118 fp) | 78.5% (182 fp) |
| **Enterprise logs** | English | 73.2% (1034 fp) | **83.7% (502 fp)** 🥇 | 57.3% (1554 fp) | 59.5% (1418 fp) | 75.9% (243 fp) | 30.2% (2497 fp) |
| **General structured PII** | English | 53.8% (4273 fp) | 46.6% (3359 fp) | 67.8% (970 fp) | 81.8% (527 fp) | 83.4% (696 fp) | **92.3% (30 fp)** 🥇 |
| **General structured PII** | German | 68.5% (2846 fp) | 71.6% (1119 fp) | 61.4% (1362 fp) | – | 77.8% (1168 fp) | **94.6% (14 fp)** 🥇 |
| **General structured PII** | Spanish | 72.9% (2067 fp) | **95.5% (103 fp)** 🥇 | 51.8% (1907 fp) | – | 77.8% (1035 fp) | 94.5% (16 fp) |
| **General structured PII** | French | 76.8% (1758 fp) | 93.1% (166 fp) | 46.7% (2609 fp) | – | 78.7% (1020 fp) | **93.6% (18 fp)** 🥇 |
| **General structured PII** | Italian | 69.9% (2432 fp) | 84.9% (452 fp) | 50.6% (2013 fp) | – | 74.2% (1212 fp) | **91.3% (35 fp)** 🥇 |
| **Academic NER (newswire / social)** | English | 40.5% (652 fp) | 19.1% (110 fp) | 67.2% (208 fp) | **77.6% (133 fp)** 🥇 | 56.6% (377 fp) | 63.8% (21 fp) |
| **Academic NER (newswire / social)** | German | 37.0% (827 fp) | 48.3% (224 fp) | 75.4% (170 fp) | – | 58.9% (452 fp) | **75.8% (15 fp)** 🥇 |
| **Adversarial / out-of-distribution** | German | 83.2% (1080 fp) | **86.5% (819 fp)** 🥇 | 49.5% (2952 fp) | – | 80.0% (787 fp) | 80.4% (203 fp) |

## Clinical / medical de-identification · English

Corpora in this group: `synth_clinical_en`.

### Leak rate · lower is better

A gold PHI span is *leaked* when **no** predicted span overlaps it — 'did we miss a name?'

| Corpus | `anonde-patterns` | `anonde-ner` | `presidio` | `presidio-transformer` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|---:|
| `synth_clinical_en` | 8.0% | **1.6%** 🥇 | 20.3% | 15.2% | 23.3% | 24.6% |

> **Partial coverage** — some engines were benchmarked on a fixed subsample, not every gold doc:
>
> - `openai-pf` on `synth_clinical_en`: scored on **40/120 docs** (deterministic subsample — metrics above are over those 40 docs only, not the full corpus).

### Severity-weighted leak rate · lower is better

Each leaked span weighted by compliance tier — direct identifiers (PERSON, EMAIL, PHONE, ADDRESS, DOB) = 5, high-stakes IDs (SSN/MRN/IBAN) = 10, quasi-identifiers (LOCATION, ORG, PROFESSION) = 1. Defaults in `label_map.yaml::severity`. Shown only because at least one cell here moves >3pp from raw leak; otherwise the two tables tracked within noise.

| Corpus | `anonde-patterns` | `anonde-ner` | `presidio` | `presidio-transformer` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|---:|
| `synth_clinical_en` | 5.3% | **1.7%** 🥇 | 26.7% | 21.3% | 26.4% | 17.4% |

## Clinical / medical de-identification · German

Corpora in this group: `openmed`, `pmc_de`, `synth_clinical`, `wiki_de`.

### Leak rate · lower is better

A gold PHI span is *leaked* when **no** predicted span overlaps it — 'did we miss a name?'

| Corpus | `anonde-patterns` | `anonde-ner` | `presidio` | `presidio-transformer` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|---:|
| `openmed` | 21.6% | **11.7%** 🥇 | 33.9% | – | 49.7% | 35.4% |
| `synth_clinical` | 1.8% | **0.5%** 🥇 | 29.1% | – | 25.4% | 23.4% |

> **Partial coverage** — some engines were benchmarked on a fixed subsample, not every gold doc:
>
> - `openai-pf` on `openmed`: scored on **40/63 docs** (deterministic subsample — metrics above are over those 40 docs only, not the full corpus).
> - `openai-pf` on `synth_clinical`: scored on **40/120 docs** (deterministic subsample — metrics above are over those 40 docs only, not the full corpus).

### Severity-weighted leak rate · lower is better

Each leaked span weighted by compliance tier — direct identifiers (PERSON, EMAIL, PHONE, ADDRESS, DOB) = 5, high-stakes IDs (SSN/MRN/IBAN) = 10, quasi-identifiers (LOCATION, ORG, PROFESSION) = 1. Defaults in `label_map.yaml::severity`. Shown only because at least one cell here moves >3pp from raw leak; otherwise the two tables tracked within noise.

| Corpus | `anonde-patterns` | `anonde-ner` | `presidio` | `presidio-transformer` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|---:|
| `openmed` | 21.1% | **11.6%** 🥇 | 35.5% | – | 50.5% | 33.8% |
| `synth_clinical` | 0.4% | **0.1%** 🥇 | 33.3% | – | 27.5% | 17.4% |

## Clinical / medical de-identification · Spanish

Corpora in this group: `pharmaconer_es`, `meddocan_es`.

### Leak rate · lower is better

A gold PHI span is *leaked* when **no** predicted span overlaps it — 'did we miss a name?'

| Corpus | `anonde-patterns` | `anonde-ner` | `presidio` | `presidio-transformer` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|---:|
| `meddocan_es` | 79.6% | **17.8%** 🥇 | 38.4% | – | 23.7% | 31.6% |

> **Partial coverage** — some engines were benchmarked on a fixed subsample, not every gold doc:
>
> - `openai-pf` on `pharmaconer_es`: scored on **40/200 docs** (deterministic subsample — metrics above are over those 40 docs only, not the full corpus).
> - `openai-pf` on `meddocan_es`: scored on **40/250 docs** (deterministic subsample — metrics above are over those 40 docs only, not the full corpus).

### Severity-weighted leak rate · lower is better

Each leaked span weighted by compliance tier — direct identifiers (PERSON, EMAIL, PHONE, ADDRESS, DOB) = 5, high-stakes IDs (SSN/MRN/IBAN) = 10, quasi-identifiers (LOCATION, ORG, PROFESSION) = 1. Defaults in `label_map.yaml::severity`. Shown only because at least one cell here moves >3pp from raw leak; otherwise the two tables tracked within noise.

| Corpus | `anonde-patterns` | `anonde-ner` | `presidio` | `presidio-transformer` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|---:|
| `meddocan_es` | 75.4% | 21.2% | 46.4% | – | 27.2% | **14.1%** 🥇 |

## Clinical / medical de-identification · French

Corpora in this group: `synth_clinical_fr`.

### Leak rate · lower is better

A gold PHI span is *leaked* when **no** predicted span overlaps it — 'did we miss a name?'

| Corpus | `anonde-patterns` | `anonde-ner` | `presidio` | `presidio-transformer` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|---:|
| `synth_clinical_fr` | 61.4% | **11.5%** 🥇 | 28.2% | – | 25.3% | 21.8% |

> **Partial coverage** — some engines were benchmarked on a fixed subsample, not every gold doc:
>
> - `openai-pf` on `synth_clinical_fr`: scored on **40/120 docs** (deterministic subsample — metrics above are over those 40 docs only, not the full corpus).

### Severity-weighted leak rate · lower is better

Each leaked span weighted by compliance tier — direct identifiers (PERSON, EMAIL, PHONE, ADDRESS, DOB) = 5, high-stakes IDs (SSN/MRN/IBAN) = 10, quasi-identifiers (LOCATION, ORG, PROFESSION) = 1. Defaults in `label_map.yaml::severity`. Shown only because at least one cell here moves >3pp from raw leak; otherwise the two tables tracked within noise.

| Corpus | `anonde-patterns` | `anonde-ner` | `presidio` | `presidio-transformer` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|---:|
| `synth_clinical_fr` | 56.8% | **11.6%** 🥇 | 30.9% | – | 28.0% | 15.3% |

## Clinical / medical de-identification · Italian

Corpora in this group: `synth_clinical_it`.

### Leak rate · lower is better

A gold PHI span is *leaked* when **no** predicted span overlaps it — 'did we miss a name?'

| Corpus | `anonde-patterns` | `anonde-ner` | `presidio` | `presidio-transformer` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|---:|
| `synth_clinical_it` | 59.7% | **16.2%** 🥇 | 28.5% | – | 35.2% | 25.1% |

> **Partial coverage** — some engines were benchmarked on a fixed subsample, not every gold doc:
>
> - `openai-pf` on `synth_clinical_it`: scored on **40/120 docs** (deterministic subsample — metrics above are over those 40 docs only, not the full corpus).

### Severity-weighted leak rate · lower is better

Each leaked span weighted by compliance tier — direct identifiers (PERSON, EMAIL, PHONE, ADDRESS, DOB) = 5, high-stakes IDs (SSN/MRN/IBAN) = 10, quasi-identifiers (LOCATION, ORG, PROFESSION) = 1. Defaults in `label_map.yaml::severity`. Shown only because at least one cell here moves >3pp from raw leak; otherwise the two tables tracked within noise.

| Corpus | `anonde-patterns` | `anonde-ner` | `presidio` | `presidio-transformer` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|---:|
| `synth_clinical_it` | 55.0% | **15.5%** 🥇 | 31.9% | – | 38.7% | 17.9% |

## Legal / administrative · English

Corpora in this group: `mapa_en`.

### Leak rate · lower is better

A gold PHI span is *leaked* when **no** predicted span overlaps it — 'did we miss a name?'

| Corpus | `anonde-patterns` | `anonde-ner` | `presidio` | `presidio-transformer` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|---:|
| `mapa_en` | 48.9% | 8.4% | **6.2%** 🥇 | 13.3% | 10.7% | 76.9% |

> **Partial coverage** — some engines were benchmarked on a fixed subsample, not every gold doc:
>
> - `openai-pf` on `mapa_en`: scored on **40/408 docs** (deterministic subsample — metrics above are over those 40 docs only, not the full corpus).

### Severity-weighted leak rate · lower is better

Each leaked span weighted by compliance tier — direct identifiers (PERSON, EMAIL, PHONE, ADDRESS, DOB) = 5, high-stakes IDs (SSN/MRN/IBAN) = 10, quasi-identifiers (LOCATION, ORG, PROFESSION) = 1. Defaults in `label_map.yaml::severity`. Shown only because at least one cell here moves >3pp from raw leak; otherwise the two tables tracked within noise.

| Corpus | `anonde-patterns` | `anonde-ner` | `presidio` | `presidio-transformer` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|---:|
| `mapa_en` | 42.9% | 8.4% | **4.9%** 🥇 | 7.5% | 9.8% | 75.4% |

## Legal / administrative · German

Corpora in this group: `legal_de`, `mapa_de`.

### Leak rate · lower is better

A gold PHI span is *leaked* when **no** predicted span overlaps it — 'did we miss a name?'

| Corpus | `anonde-patterns` | `anonde-ner` | `presidio` | `presidio-transformer` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|---:|
| `legal_de` | 6.5% | **0.4%** 🥇 | 24.5% | – | 25.4% | 31.4% |
| `mapa_de` | 51.6% | **6.2%** 🥇 | 44.8% | – | 10.4% | 85.0% |

> **Partial coverage** — some engines were benchmarked on a fixed subsample, not every gold doc:
>
> - `openai-pf` on `legal_de`: scored on **40/150 docs** (deterministic subsample — metrics above are over those 40 docs only, not the full corpus).
> - `openai-pf` on `mapa_de`: scored on **40/558 docs** (deterministic subsample — metrics above are over those 40 docs only, not the full corpus).

### Severity-weighted leak rate · lower is better

Each leaked span weighted by compliance tier — direct identifiers (PERSON, EMAIL, PHONE, ADDRESS, DOB) = 5, high-stakes IDs (SSN/MRN/IBAN) = 10, quasi-identifiers (LOCATION, ORG, PROFESSION) = 1. Defaults in `label_map.yaml::severity`. Shown only because at least one cell here moves >3pp from raw leak; otherwise the two tables tracked within noise.

| Corpus | `anonde-patterns` | `anonde-ner` | `presidio` | `presidio-transformer` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|---:|
| `legal_de` | 5.1% | **0.4%** 🥇 | 32.9% | – | 33.3% | 17.0% |
| `mapa_de` | 36.4% | **5.5%** 🥇 | 52.0% | – | 8.4% | 68.8% |

## Legal / administrative · Spanish

Corpora in this group: `mapa_es`.

### Leak rate · lower is better

A gold PHI span is *leaked* when **no** predicted span overlaps it — 'did we miss a name?'

| Corpus | `anonde-patterns` | `anonde-ner` | `presidio` | `presidio-transformer` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|---:|
| `mapa_es` | 100.0% | **6.5%** 🥇 | 59.1% | – | 18.3% | 93.8% |

> **Partial coverage** — some engines were benchmarked on a fixed subsample, not every gold doc:
>
> - `openai-pf` on `mapa_es`: scored on **40/155 docs** (deterministic subsample — metrics above are over those 40 docs only, not the full corpus).

## Legal / administrative · French

Corpora in this group: `mapa_fr`.

### Leak rate · lower is better

A gold PHI span is *leaked* when **no** predicted span overlaps it — 'did we miss a name?'

| Corpus | `anonde-patterns` | `anonde-ner` | `presidio` | `presidio-transformer` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|---:|
| `mapa_fr` | 75.2% | **11.4%** 🥇 | 44.9% | – | 16.3% | 95.5% |

> **Partial coverage** — some engines were benchmarked on a fixed subsample, not every gold doc:
>
> - `openai-pf` on `mapa_fr`: scored on **40/490 docs** (deterministic subsample — metrics above are over those 40 docs only, not the full corpus).

### Severity-weighted leak rate · lower is better

Each leaked span weighted by compliance tier — direct identifiers (PERSON, EMAIL, PHONE, ADDRESS, DOB) = 5, high-stakes IDs (SSN/MRN/IBAN) = 10, quasi-identifiers (LOCATION, ORG, PROFESSION) = 1. Defaults in `label_map.yaml::severity`. Shown only because at least one cell here moves >3pp from raw leak; otherwise the two tables tracked within noise.

| Corpus | `anonde-patterns` | `anonde-ner` | `presidio` | `presidio-transformer` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|---:|
| `mapa_fr` | 60.3% | **8.9%** 🥇 | 53.2% | – | 11.2% | 98.5% |

## Legal / administrative · Italian

Corpora in this group: `mapa_it`.

### Leak rate · lower is better

A gold PHI span is *leaked* when **no** predicted span overlaps it — 'did we miss a name?'

| Corpus | `anonde-patterns` | `anonde-ner` | `presidio` | `presidio-transformer` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|---:|
| `mapa_it` | 40.4% | **5.5%** 🥇 | 67.1% | – | 6.2% | 50.0% |

> **Partial coverage** — some engines were benchmarked on a fixed subsample, not every gold doc:
>
> - `openai-pf` on `mapa_it`: scored on **40/550 docs** (deterministic subsample — metrics above are over those 40 docs only, not the full corpus).

## Retail finance · English

Corpora in this group: `synth_finance_en`.

### Leak rate · lower is better

A gold PHI span is *leaked* when **no** predicted span overlaps it — 'did we miss a name?'

| Corpus | `anonde-patterns` | `anonde-ner` | `presidio` | `presidio-transformer` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|---:|
| `synth_finance_en` | 21.1% | **2.8%** 🥇 | 10.3% | 12.0% | 20.6% | 18.5% |

> **Partial coverage** — some engines were benchmarked on a fixed subsample, not every gold doc:
>
> - `openai-pf` on `synth_finance_en`: scored on **40/120 docs** (deterministic subsample — metrics above are over those 40 docs only, not the full corpus).

### Severity-weighted leak rate · lower is better

Each leaked span weighted by compliance tier — direct identifiers (PERSON, EMAIL, PHONE, ADDRESS, DOB) = 5, high-stakes IDs (SSN/MRN/IBAN) = 10, quasi-identifiers (LOCATION, ORG, PROFESSION) = 1. Defaults in `label_map.yaml::severity`. Shown only because at least one cell here moves >3pp from raw leak; otherwise the two tables tracked within noise.

| Corpus | `anonde-patterns` | `anonde-ner` | `presidio` | `presidio-transformer` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|---:|
| `synth_finance_en` | 14.1% | **1.9%** 🥇 | 11.9% | 14.2% | 22.7% | 6.0% |

## Retail finance · German

Corpora in this group: `finance_de`, `synth_finance_de`.

### Leak rate · lower is better

A gold PHI span is *leaked* when **no** predicted span overlaps it — 'did we miss a name?'

| Corpus | `anonde-patterns` | `anonde-ner` | `presidio` | `presidio-transformer` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|---:|
| `finance_de` | 3.2% | **1.6%** 🥇 | 25.9% | – | 26.2% | 20.8% |
| `synth_finance_de` | **3.8%** 🥇 | 4.2% | 21.4% | – | 17.2% | 22.2% |

> **Partial coverage** — some engines were benchmarked on a fixed subsample, not every gold doc:
>
> - `openai-pf` on `finance_de`: scored on **40/150 docs** (deterministic subsample — metrics above are over those 40 docs only, not the full corpus).
> - `openai-pf` on `synth_finance_de`: scored on **40/120 docs** (deterministic subsample — metrics above are over those 40 docs only, not the full corpus).

### Severity-weighted leak rate · lower is better

Each leaked span weighted by compliance tier — direct identifiers (PERSON, EMAIL, PHONE, ADDRESS, DOB) = 5, high-stakes IDs (SSN/MRN/IBAN) = 10, quasi-identifiers (LOCATION, ORG, PROFESSION) = 1. Defaults in `label_map.yaml::severity`. Shown only because at least one cell here moves >3pp from raw leak; otherwise the two tables tracked within noise.

| Corpus | `anonde-patterns` | `anonde-ner` | `presidio` | `presidio-transformer` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|---:|
| `finance_de` | 3.3% | **1.7%** 🥇 | 30.2% | – | 29.2% | 17.1% |
| `synth_finance_de` | 6.2% | **4.9%** 🥇 | 23.0% | – | 17.2% | 9.9% |

## Retail finance · Spanish

Corpora in this group: `synth_finance_es`.

### Leak rate · lower is better

A gold PHI span is *leaked* when **no** predicted span overlaps it — 'did we miss a name?'

| Corpus | `anonde-patterns` | `anonde-ner` | `presidio` | `presidio-transformer` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|---:|
| `synth_finance_es` | 46.4% | **16.4%** 🥇 | 18.5% | – | 25.4% | 17.4% |

> **Partial coverage** — some engines were benchmarked on a fixed subsample, not every gold doc:
>
> - `openai-pf` on `synth_finance_es`: scored on **40/120 docs** (deterministic subsample — metrics above are over those 40 docs only, not the full corpus).

### Severity-weighted leak rate · lower is better

Each leaked span weighted by compliance tier — direct identifiers (PERSON, EMAIL, PHONE, ADDRESS, DOB) = 5, high-stakes IDs (SSN/MRN/IBAN) = 10, quasi-identifiers (LOCATION, ORG, PROFESSION) = 1. Defaults in `label_map.yaml::severity`. Shown only because at least one cell here moves >3pp from raw leak; otherwise the two tables tracked within noise.

| Corpus | `anonde-patterns` | `anonde-ner` | `presidio` | `presidio-transformer` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|---:|
| `synth_finance_es` | 37.2% | 16.2% | 19.9% | – | 28.8% | **4.7%** 🥇 |

## Retail finance · French

Corpora in this group: `synth_finance_fr`.

### Leak rate · lower is better

A gold PHI span is *leaked* when **no** predicted span overlaps it — 'did we miss a name?'

| Corpus | `anonde-patterns` | `anonde-ner` | `presidio` | `presidio-transformer` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|---:|
| `synth_finance_fr` | 44.6% | **15.9%** 🥇 | 19.4% | – | 23.9% | 19.3% |

> **Partial coverage** — some engines were benchmarked on a fixed subsample, not every gold doc:
>
> - `openai-pf` on `synth_finance_fr`: scored on **40/120 docs** (deterministic subsample — metrics above are over those 40 docs only, not the full corpus).

### Severity-weighted leak rate · lower is better

Each leaked span weighted by compliance tier — direct identifiers (PERSON, EMAIL, PHONE, ADDRESS, DOB) = 5, high-stakes IDs (SSN/MRN/IBAN) = 10, quasi-identifiers (LOCATION, ORG, PROFESSION) = 1. Defaults in `label_map.yaml::severity`. Shown only because at least one cell here moves >3pp from raw leak; otherwise the two tables tracked within noise.

| Corpus | `anonde-patterns` | `anonde-ner` | `presidio` | `presidio-transformer` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|---:|
| `synth_finance_fr` | 35.1% | 13.5% | 21.7% | – | 26.4% | **6.7%** 🥇 |

## Retail finance · Italian

Corpora in this group: `synth_finance_it`.

### Leak rate · lower is better

A gold PHI span is *leaked* when **no** predicted span overlaps it — 'did we miss a name?'

| Corpus | `anonde-patterns` | `anonde-ner` | `presidio` | `presidio-transformer` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|---:|
| `synth_finance_it` | 39.5% | **11.7%** 🥇 | 29.6% | – | 26.4% | 14.1% |

> **Partial coverage** — some engines were benchmarked on a fixed subsample, not every gold doc:
>
> - `openai-pf` on `synth_finance_it`: scored on **40/120 docs** (deterministic subsample — metrics above are over those 40 docs only, not the full corpus).

### Severity-weighted leak rate · lower is better

Each leaked span weighted by compliance tier — direct identifiers (PERSON, EMAIL, PHONE, ADDRESS, DOB) = 5, high-stakes IDs (SSN/MRN/IBAN) = 10, quasi-identifiers (LOCATION, ORG, PROFESSION) = 1. Defaults in `label_map.yaml::severity`. Shown only because at least one cell here moves >3pp from raw leak; otherwise the two tables tracked within noise.

| Corpus | `anonde-patterns` | `anonde-ner` | `presidio` | `presidio-transformer` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|---:|
| `synth_finance_it` | 30.5% | 9.7% | 40.5% | – | 23.8% | **4.1%** 🥇 |

## Enterprise logs · English

Corpora in this group: `synth_logs`.

### Leak rate · lower is better

A gold PHI span is *leaked* when **no** predicted span overlaps it — 'did we miss a name?'

| Corpus | `anonde-patterns` | `anonde-ner` | `presidio` | `presidio-transformer` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|---:|
| `synth_logs` | 28.9% | **13.2%** 🥇 | 31.5% | 37.3% | 73.2% | 15.3% |

> **Partial coverage** — some engines were benchmarked on a fixed subsample, not every gold doc:
>
> - `openai-pf` on `synth_logs`: scored on **40/120 docs** (deterministic subsample — metrics above are over those 40 docs only, not the full corpus).

### Severity-weighted leak rate · lower is better

Each leaked span weighted by compliance tier — direct identifiers (PERSON, EMAIL, PHONE, ADDRESS, DOB) = 5, high-stakes IDs (SSN/MRN/IBAN) = 10, quasi-identifiers (LOCATION, ORG, PROFESSION) = 1. Defaults in `label_map.yaml::severity`. Shown only because at least one cell here moves >3pp from raw leak; otherwise the two tables tracked within noise.

| Corpus | `anonde-patterns` | `anonde-ner` | `presidio` | `presidio-transformer` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|---:|
| `synth_logs` | 34.7% | 16.4% | 39.6% | 46.5% | 79.7% | **7.7%** 🥇 |

## General structured PII · English

Corpora in this group: `ai4privacy_en`.

### Leak rate · lower is better

A gold PHI span is *leaked* when **no** predicted span overlaps it — 'did we miss a name?'

| Corpus | `anonde-patterns` | `anonde-ner` | `presidio` | `presidio-transformer` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|---:|
| `ai4privacy_en` | 47.8% | **13.0%** 🥇 | 56.0% | 57.4% | 35.0% | 15.5% |

> **Partial coverage** — some engines were benchmarked on a fixed subsample, not every gold doc:
>
> - `openai-pf` on `ai4privacy_en`: scored on **40/1000 docs** (deterministic subsample — metrics above are over those 40 docs only, not the full corpus).

### Severity-weighted leak rate · lower is better

Each leaked span weighted by compliance tier — direct identifiers (PERSON, EMAIL, PHONE, ADDRESS, DOB) = 5, high-stakes IDs (SSN/MRN/IBAN) = 10, quasi-identifiers (LOCATION, ORG, PROFESSION) = 1. Defaults in `label_map.yaml::severity`. Shown only because at least one cell here moves >3pp from raw leak; otherwise the two tables tracked within noise.

| Corpus | `anonde-patterns` | `anonde-ner` | `presidio` | `presidio-transformer` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|---:|
| `ai4privacy_en` | 39.1% | 10.8% | 57.4% | 58.8% | 37.0% | **10.0%** 🥇 |

## General structured PII · German

Corpora in this group: `ai4privacy_de`.

### Leak rate · lower is better

A gold PHI span is *leaked* when **no** predicted span overlaps it — 'did we miss a name?'

| Corpus | `anonde-patterns` | `anonde-ner` | `presidio` | `presidio-transformer` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|---:|
| `ai4privacy_de` | 59.8% | **10.7%** 🥇 | 58.4% | – | 33.0% | 23.6% |

> **Partial coverage** — some engines were benchmarked on a fixed subsample, not every gold doc:
>
> - `openai-pf` on `ai4privacy_de`: scored on **40/1000 docs** (deterministic subsample — metrics above are over those 40 docs only, not the full corpus).

### Severity-weighted leak rate · lower is better

Each leaked span weighted by compliance tier — direct identifiers (PERSON, EMAIL, PHONE, ADDRESS, DOB) = 5, high-stakes IDs (SSN/MRN/IBAN) = 10, quasi-identifiers (LOCATION, ORG, PROFESSION) = 1. Defaults in `label_map.yaml::severity`. Shown only because at least one cell here moves >3pp from raw leak; otherwise the two tables tracked within noise.

| Corpus | `anonde-patterns` | `anonde-ner` | `presidio` | `presidio-transformer` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|---:|
| `ai4privacy_de` | 63.9% | **9.7%** 🥇 | 62.3% | – | 34.7% | 16.3% |

## General structured PII · Spanish

Corpora in this group: `ai4privacy_es`.

### Leak rate · lower is better

A gold PHI span is *leaked* when **no** predicted span overlaps it — 'did we miss a name?'

| Corpus | `anonde-patterns` | `anonde-ner` | `presidio` | `presidio-transformer` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|---:|
| `ai4privacy_es` | 68.9% | **14.2%** 🥇 | 61.0% | – | 34.6% | 17.5% |

> **Partial coverage** — some engines were benchmarked on a fixed subsample, not every gold doc:
>
> - `openai-pf` on `ai4privacy_es`: scored on **40/1000 docs** (deterministic subsample — metrics above are over those 40 docs only, not the full corpus).

### Severity-weighted leak rate · lower is better

Each leaked span weighted by compliance tier — direct identifiers (PERSON, EMAIL, PHONE, ADDRESS, DOB) = 5, high-stakes IDs (SSN/MRN/IBAN) = 10, quasi-identifiers (LOCATION, ORG, PROFESSION) = 1. Defaults in `label_map.yaml::severity`. Shown only because at least one cell here moves >3pp from raw leak; otherwise the two tables tracked within noise.

| Corpus | `anonde-patterns` | `anonde-ner` | `presidio` | `presidio-transformer` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|---:|
| `ai4privacy_es` | 68.7% | 11.2% | 65.1% | – | 35.2% | **7.6%** 🥇 |

## General structured PII · French

Corpora in this group: `ai4privacy_fr`.

### Leak rate · lower is better

A gold PHI span is *leaked* when **no** predicted span overlaps it — 'did we miss a name?'

| Corpus | `anonde-patterns` | `anonde-ner` | `presidio` | `presidio-transformer` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|---:|
| `ai4privacy_fr` | 68.9% | **14.2%** 🥇 | 54.0% | – | 34.9% | 16.3% |

> **Partial coverage** — some engines were benchmarked on a fixed subsample, not every gold doc:
>
> - `openai-pf` on `ai4privacy_fr`: scored on **40/1000 docs** (deterministic subsample — metrics above are over those 40 docs only, not the full corpus).

### Severity-weighted leak rate · lower is better

Each leaked span weighted by compliance tier — direct identifiers (PERSON, EMAIL, PHONE, ADDRESS, DOB) = 5, high-stakes IDs (SSN/MRN/IBAN) = 10, quasi-identifiers (LOCATION, ORG, PROFESSION) = 1. Defaults in `label_map.yaml::severity`. Shown only because at least one cell here moves >3pp from raw leak; otherwise the two tables tracked within noise.

| Corpus | `anonde-patterns` | `anonde-ner` | `presidio` | `presidio-transformer` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|---:|
| `ai4privacy_fr` | 67.1% | 11.7% | 56.7% | – | 35.8% | **9.3%** 🥇 |

## General structured PII · Italian

Corpora in this group: `ai4privacy_it`.

### Leak rate · lower is better

A gold PHI span is *leaked* when **no** predicted span overlaps it — 'did we miss a name?'

| Corpus | `anonde-patterns` | `anonde-ner` | `presidio` | `presidio-transformer` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|---:|
| `ai4privacy_it` | 63.9% | **15.0%** 🥇 | 59.6% | – | 36.0% | 21.2% |

> **Partial coverage** — some engines were benchmarked on a fixed subsample, not every gold doc:
>
> - `openai-pf` on `ai4privacy_it`: scored on **40/1000 docs** (deterministic subsample — metrics above are over those 40 docs only, not the full corpus).

### Severity-weighted leak rate · lower is better

Each leaked span weighted by compliance tier — direct identifiers (PERSON, EMAIL, PHONE, ADDRESS, DOB) = 5, high-stakes IDs (SSN/MRN/IBAN) = 10, quasi-identifiers (LOCATION, ORG, PROFESSION) = 1. Defaults in `label_map.yaml::severity`. Shown only because at least one cell here moves >3pp from raw leak; otherwise the two tables tracked within noise.

| Corpus | `anonde-patterns` | `anonde-ner` | `presidio` | `presidio-transformer` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|---:|
| `ai4privacy_it` | 58.9% | **11.5%** 🥇 | 62.4% | – | 36.6% | 13.6% |

## Academic NER (newswire / social) · English

Corpora in this group: `conll2003_en`, `wnut_17`.

### Leak rate · lower is better

A gold PHI span is *leaked* when **no** predicted span overlaps it — 'did we miss a name?'

| Corpus | `anonde-patterns` | `anonde-ner` | `presidio` | `presidio-transformer` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|---:|
| `conll2003_en` | 96.7% | **4.7%** 🥇 | 6.7% | 12.2% | 6.0% | 64.0% |
| `wnut_17` | 80.9% | **11.2%** 🥇 | 43.1% | 26.6% | 25.5% | 82.1% |

> **Partial coverage** — some engines were benchmarked on a fixed subsample, not every gold doc:
>
> - `openai-pf` on `conll2003_en`: scored on **40/300 docs** (deterministic subsample — metrics above are over those 40 docs only, not the full corpus).
> - `openai-pf` on `wnut_17`: scored on **40/300 docs** (deterministic subsample — metrics above are over those 40 docs only, not the full corpus).

### Severity-weighted leak rate · lower is better

Each leaked span weighted by compliance tier — direct identifiers (PERSON, EMAIL, PHONE, ADDRESS, DOB) = 5, high-stakes IDs (SSN/MRN/IBAN) = 10, quasi-identifiers (LOCATION, ORG, PROFESSION) = 1. Defaults in `label_map.yaml::severity`. Shown only because at least one cell here moves >3pp from raw leak; otherwise the two tables tracked within noise.

| Corpus | `anonde-patterns` | `anonde-ner` | `presidio` | `presidio-transformer` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|---:|
| `conll2003_en` | 97.0% | **5.2%** 🥇 | 5.3% | 6.2% | 6.9% | 47.9% |
| `wnut_17` | 74.6% | **6.7%** 🥇 | 42.2% | 21.4% | 25.4% | 61.5% |

## Academic NER (newswire / social) · German

Corpora in this group: `wikiann_de`, `germeval_14`, `conll2003_de`.

### Leak rate · lower is better

A gold PHI span is *leaked* when **no** predicted span overlaps it — 'did we miss a name?'

| Corpus | `anonde-patterns` | `anonde-ner` | `presidio` | `presidio-transformer` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|---:|
| `wikiann_de` | 41.9% | **3.0%** 🥇 | 16.7% | – | 12.8% | 59.6% |
| `germeval_14` | 55.0% | **9.3%** 🥇 | 22.0% | – | 18.9% | 94.6% |

> **Partial coverage** — some engines were benchmarked on a fixed subsample, not every gold doc:
>
> - `openai-pf` on `wikiann_de`: scored on **40/300 docs** (deterministic subsample — metrics above are over those 40 docs only, not the full corpus).
> - `openai-pf` on `germeval_14`: scored on **40/300 docs** (deterministic subsample — metrics above are over those 40 docs only, not the full corpus).

### Severity-weighted leak rate · lower is better

Each leaked span weighted by compliance tier — direct identifiers (PERSON, EMAIL, PHONE, ADDRESS, DOB) = 5, high-stakes IDs (SSN/MRN/IBAN) = 10, quasi-identifiers (LOCATION, ORG, PROFESSION) = 1. Defaults in `label_map.yaml::severity`. Shown only because at least one cell here moves >3pp from raw leak; otherwise the two tables tracked within noise.

| Corpus | `anonde-patterns` | `anonde-ner` | `presidio` | `presidio-transformer` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|---:|
| `wikiann_de` | 30.3% | **1.9%** 🥇 | 6.9% | – | 10.7% | 44.3% |
| `germeval_14` | 47.5% | **6.2%** 🥇 | 15.8% | – | 16.1% | 88.8% |

## Adversarial / out-of-distribution · German

Corpora in this group: `adversarial_de`.

### Leak rate · lower is better

A gold PHI span is *leaked* when **no** predicted span overlaps it — 'did we miss a name?'

| Corpus | `anonde-patterns` | `anonde-ner` | `presidio` | `presidio-transformer` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|---:|
| `adversarial_de` | 12.7% | **8.1%** 🥇 | 37.6% | – | 43.4% | 32.0% |

> **Partial coverage** — some engines were benchmarked on a fixed subsample, not every gold doc:
>
> - `openai-pf` on `adversarial_de`: scored on **40/300 docs** (deterministic subsample — metrics above are over those 40 docs only, not the full corpus).

### Severity-weighted leak rate · lower is better

Each leaked span weighted by compliance tier — direct identifiers (PERSON, EMAIL, PHONE, ADDRESS, DOB) = 5, high-stakes IDs (SSN/MRN/IBAN) = 10, quasi-identifiers (LOCATION, ORG, PROFESSION) = 1. Defaults in `label_map.yaml::severity`. Shown only because at least one cell here moves >3pp from raw leak; otherwise the two tables tracked within noise.

| Corpus | `anonde-patterns` | `anonde-ner` | `presidio` | `presidio-transformer` | `gliner-py` | `openai-pf` |
|---|---:|---:|---:|---:|---:|---:|
| `adversarial_de` | 12.2% | **8.1%** 🥇 | 40.8% | – | 44.0% | 27.7% |

## Latency · per-document p50 / p95

Wall-clock per `engine.Analyze(doc)` call. p50 = steady-state, p95 = tail (the SLO knob). Mean + p99 in `results_matrix.csv`. One table across every corpus — latency tracks corpus length, not domain or language.

| Corpus | `anonde-patterns` p50 / p95 | `anonde-ner` p50 / p95 | `presidio` p50 / p95 | `presidio-transformer` p50 / p95 | `gliner-py` p50 / p95 | `openai-pf` p50 / p95 |
|---|---:|---:|---:|---:|---:|---:|
| `synth_clinical_en` | 3 ms / 5 ms | 467 ms / 741 ms | 41 ms / 56 ms | 477 ms / 636 ms | 528 ms / 669 ms | 1.5 s / 2.0 s |
| `openmed` | 9 ms / 28 ms | 1.3 s / 4.4 s | 135 ms / 336 ms | – | 2.1 s / 9.9 s | 7.9 s / 22.2 s |
| `synth_clinical` | 3 ms / 5 ms | 320 ms / 512 ms | 30 ms / 39 ms | – | 415 ms / 573 ms | 745 ms / 860 ms |
| `pharmaconer_es` | 4 ms / 8 ms | 1.0 s / 2.3 s | 64 ms / 151 ms | – | 1.2 s / 3.2 s | 4.1 s / 8.9 s |
| `meddocan_es` | 5 ms / 9 ms | 1.3 s / 2.5 s | 87 ms / 171 ms | – | 1.6 s / 3.6 s | 5.0 s / 8.3 s |
| `synth_clinical_fr` | 2 ms / 3 ms | 613 ms / 805 ms | 48 ms / 65 ms | – | 653 ms / 894 ms | 1.8 s / 2.6 s |
| `synth_clinical_it` | 3 ms / 4 ms | 469 ms / 796 ms | 48 ms / 66 ms | – | 663 ms / 953 ms | 2.1 s / 2.9 s |
| `mapa_en` | 1 ms / 2 ms | 104 ms / 260 ms | 8 ms / 23 ms | 69 ms / 301 ms | 173 ms / 333 ms | 208 ms / 491 ms |
| `legal_de` | 2 ms / 3 ms | 383 ms / 477 ms | 36 ms / 51 ms | – | 471 ms / 594 ms | 1.2 s / 1.6 s |
| `mapa_de` | 1 ms / 1 ms | 127 ms / 246 ms | 10 ms / 18 ms | – | 223 ms / 347 ms | 304 ms / 593 ms |
| `mapa_es` | 0 ms / 1 ms | 126 ms / 335 ms | 10 ms / 28 ms | – | 212 ms / 413 ms | 354 ms / 798 ms |
| `mapa_fr` | 0 ms / 1 ms | 127 ms / 274 ms | 11 ms / 28 ms | – | 214 ms / 359 ms | 393 ms / 772 ms |
| `mapa_it` | 0 ms / 1 ms | 122 ms / 228 ms | 10 ms / 18 ms | – | 207 ms / 314 ms | 383 ms / 682 ms |
| `synth_finance_en` | 2 ms / 3 ms | 289 ms / 479 ms | 31 ms / 55 ms | 335 ms / 642 ms | 368 ms / 577 ms | 786 ms / 958 ms |
| `finance_de` | 3 ms / 4 ms | 305 ms / 453 ms | 31 ms / 51 ms | – | 350 ms / 520 ms | 842 ms / 953 ms |
| `synth_finance_de` | 2 ms / 3 ms | 319 ms / 482 ms | 28 ms / 57 ms | – | 401 ms / 641 ms | 927 ms / 1.2 s |
| `synth_finance_es` | 1 ms / 2 ms | 341 ms / 493 ms | 30 ms / 55 ms | – | 430 ms / 669 ms | 892 ms / 1.2 s |
| `synth_finance_fr` | 1 ms / 2 ms | 356 ms / 495 ms | 35 ms / 60 ms | – | 446 ms / 680 ms | 979 ms / 1.2 s |
| `synth_finance_it` | 1 ms / 3 ms | 355 ms / 612 ms | 31 ms / 55 ms | – | 442 ms / 714 ms | 1.0 s / 1.3 s |
| `synth_logs` | 4 ms / 8 ms | 745 ms / 1.5 s | 61 ms / 150 ms | 1.1 s / 2.5 s | 1.2 s / 3.2 s | 7.6 s / 10.8 s |
| `ai4privacy_en` | 1 ms / 2 ms | 218 ms / 324 ms | 22 ms / 31 ms | 281 ms / 337 ms | 312 ms / 435 ms | 620 ms / 837 ms |
| `ai4privacy_de` | 1 ms / 2 ms | 234 ms / 336 ms | 20 ms / 29 ms | – | 330 ms / 441 ms | 689 ms / 908 ms |
| `ai4privacy_es` | 1 ms / 1 ms | 233 ms / 340 ms | 19 ms / 27 ms | – | 334 ms / 448 ms | 678 ms / 798 ms |
| `ai4privacy_fr` | 1 ms / 1 ms | 262 ms / 358 ms | 26 ms / 37 ms | – | 391 ms / 510 ms | 614 ms / 805 ms |
| `ai4privacy_it` | 1 ms / 1 ms | 237 ms / 340 ms | 20 ms / 29 ms | – | 332 ms / 448 ms | 716 ms / 933 ms |
| `conll2003_en` | 0 ms / 1 ms | 68 ms / 122 ms | 7 ms / 13 ms | 54 ms / 87 ms | 149 ms / 201 ms | 173 ms / 349 ms |
| `wnut_17` | 0 ms / 1 ms | 93 ms / 167 ms | 7 ms / 13 ms | 69 ms / 125 ms | 190 ms / 269 ms | 226 ms / 352 ms |
| `wikiann_de` | 0 ms / 1 ms | 66 ms / 104 ms | 5 ms / 9 ms | – | 152 ms / 190 ms | 128 ms / 302 ms |
| `germeval_14` | 0 ms / 1 ms | 94 ms / 142 ms | 7 ms / 11 ms | – | 190 ms / 240 ms | 199 ms / 339 ms |
| `adversarial_de` | 3 ms / 4 ms | 463 ms / 691 ms | 45 ms / 59 ms | – | 675 ms / 1.1 s | 2.9 s / 4.2 s |

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
*Generated by `bench/scoring/render_matrix.py` over 157 cells. Full per-entity-type breakdown in `results_matrix.csv`.*
