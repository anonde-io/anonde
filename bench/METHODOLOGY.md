# Benchmark methodology

How the anonde benchmark scores engines, why those metrics, and the honest
limits. The goal is a benchmark a compliance buyer or a reviewer can check —
grounded in the established de-identification evaluation tradition, reproducible,
and cross-checked against a standard scoring library.

## Metrics

- **Leak rate (the headline).** Fraction of gold PII spans with **no overlapping
  prediction of any type** = `1 − binary recall`. Lower is better. This is the
  load-bearing safety metric: a *missed* identifier is the catastrophic failure
  for a redactor. It follows the i2b2/n2c2 clinical de-id convention of treating
  *binary "HIPAA" recall* (did you catch the PHI at all, regardless of type) as
  primary.
- **Precision / Recall / F1**, entity-level, two views:
  - **Strict** — exact span (`start` and `end`) **and** type match. The
    CoNLL-2003 / `seqeval` NER convention.
  - **Partial (relaxed)** — same type, spans **overlap**. The i2b2 relaxed
    convention (boundary tolerance): catching the identifier matters more than
    exact character boundaries.
- **Over-redaction = `1 − precision`.** The cost of recall-first tuning, reported
  explicitly (anonde trades precision for recall — see Caveats).

All figures are **micro-averaged, doc-weighted** across a group (larger corpora
count more).

## Matching rules (precise)

For a gold span `(s, e, type)` and a prediction `(s', e', type')`:

| | Rule |
|---|---|
| Strict TP | `s'==s` and `e'==e` and `type'==type` |
| Partial TP | `type'==type` and `[s',e')` overlaps `[s,e)` |
| **Leak** (type-agnostic) | a gold span is *leaked* iff **no** prediction (any type) overlaps it |

## Lineage

- **Span-based strict + relaxed matching and binary recall** follow the **i2b2
  (2006, 2014)** and **n2c2 (2016)** de-identification challenges — the canonical
  clinical de-id benchmarks.
- **Strict entity-level F1** matches the **CoNLL-2003** NER convention and
  cross-checks **exactly** (Δ≈0 across engines) against a standard span-based
  scoring library (`nervaluate`, SemEval/MUC) — see
  `bench/scoring/verify_official.py`. Our **strict** numbers reproduce
  independently; the **partial** view is a deliberately *lenient* overlap scorer
  (see Caveats).

## Precision zero-gold exclusion

A `(corpus, entity-type)` cell where the gold annotates **zero** spans of that
type (`tp + fn == 0`) is *unscoreable for precision*: with no gold present, every
prediction there is mechanically a false positive against absent gold — a schema
gap, not real over-redaction. Such cells are excluded from the **precision** pool
(the raw-including number is shown alongside for transparency). This is a
precision-aggregation choice only; **leak rate and recall are untouched** (they
score against the full gold).

## Competitor configuration

Each competitor runs its standard/recommended config, documented for
reproducibility:

- **`presidio`** — Microsoft Presidio with the production spaCy models
  (`en_core_web_lg`, `de/es/fr/it_core_news_lg`), score threshold 0.5.
- **`presidio-transformer`** — Presidio's best English config
  (`en_core_web_trf`), English corpora only (partial coverage).
- **`gliner-py`** — the same GLiNER PII model via PyTorch/safetensors; a parity
  check for anonde's in-process ONNX path.
- **`openai-pf`** — OpenAI's privacy/moderation filter on a fixed per-corpus
  subsample (rate/cost), marked partial-coverage.

## Reproducibility

- Full matrix: `make -C bench matrix`. One corpus: `make -C bench/corpora/<name> all`.
- Independent standard-scorer cross-check: `bench/scoring/verify_official.py`.
- Corpora are standard/public (ai4privacy, CoNLL-2003, WNUT-17, GermEval-14,
  MEDDOCAN, GGPONC, MAPA, PMC, …), spanning 5 languages and 7 domains.

## Honest caveats

- **Recall-first.** anonde deliberately over-redacts to minimize leaks, so it
  **trails precision-optimized tools on over-redaction** (`1 − precision`). Leak
  rate is the load-bearing metric for compliance, but the precision trade is real
  and shown — not hidden.
- **Partial is lenient.** The relaxed/overlap P/R/F1 books a TP per overlapping
  prediction, so it runs **higher than a standard one-to-one aligner**
  (`nervaluate` `ent_type`). It's an intentional boundary-tolerant view. For a
  citable overlap number use `nervaluate ent_type`; lead with **leak rate** and
  **strict F1** (both reproduce exactly). The precision scorecard is partial-based
  and therefore *optimistic* in absolute terms — but all engines are scored
  identically, so the cross-engine ranking is unaffected.
- **Self-run.** This is anonde's harness and corpus selection. We mitigate with a
  published methodology, per-cell reproducibility, and the standard-scorer
  cross-check — *run it yourself*.
- **Partial coverage.** `presidio-transformer` (EN-only) and `openai-pf`
  (subsample) roll up only over their covered cells; non-covered cells render `–`.
- **Corpus mix** leans multilingual + clinical (anonde's strength). Per-language
  and per-domain breakdowns are reported so the mix is visible; the aggregate is
  not a substitute for the slice that matches your data.
