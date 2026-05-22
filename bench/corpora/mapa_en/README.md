# bench/corpora/mapa_en — English legal/administrative PII bench (MAPA)

The English slice of the
[`joelniklaus/mapa`](https://huggingface.co/datasets/joelniklaus/mapa)
corpus (CC-BY-4.0) — MAPA, the **M**ultilingual **A**nonymisation for
**P**ublic **A**dministration dataset. Phase 2 of the multilingual
bench expansion: gives the matrix a real-gold **legal / administrative**
domain number alongside the synthetic `legal_de` corpus.

MAPA is an EU-funded, anonymisation-grade NER corpus of legal and
administrative text (EUR-Lex judgments + national public-administration
documents), and is one of the datasets in the LEXTREME legal-NLP
benchmark. It covers 24 EU languages; the bench wires five — en, de,
es, fr, it.

`mapa_en` is the **anchor** corpus of the `mapa_*` family: it holds the
shared loader at `cmd/fetch_mapa.py`. The four sibling corpora
(`mapa_de`, `mapa_es`, `mapa_fr`, `mapa_it`) are thin wrappers that
call this same loader with `--language <code>`.

## Coarse vs fine — why gold is built from the coarse layer

MAPA ships a **two-level** annotation: a `coarse_grained` and a
`fine_grained` BIO column. The coarse layer has five classes — PERSON,
ORGANISATION, ADDRESS, DATE, AMOUNT. The fine layer adds sub-labels
(FAMILY NAME, INITIAL NAME, TITLE, DAY/MONTH/YEAR, CITY/COUNTRY, …).

This loader builds gold from the **coarse** layer. The fine layer is
**not** a clean leaf decomposition of the coarse layer, so it cannot be
used as-is:

- There is **no `GIVEN NAME` label anywhere in MAPA**. Given names are
  `B-/I-PERSON` at the coarse level but plain `O` at the fine level.
  ~12 % of coarse PERSON tokens — and ~25 % of *all* coarse-entity
  tokens — are uncovered by the fine layer.
- The fine layer is structurally **cross-cutting**, not nested: a
  `FAMILY NAME` span turns up inside a coarse `DATE` or `ORGANISATION`;
  `CITY`/`COUNTRY` turn up inside a coarse `PERSON`. It tags
  sub-mentions, not leaves.

Building gold from fine-grained spans would drop every given name (a
recall hole no PII bench can accept) and emit nonsensical spans. The
coarse layer is MAPA's actual anonymisation layer and the only complete
one.

`AMOUNT` (monetary value) is dropped at the loader — anonde's rule is
"monetary amounts are not PII", so AMOUNT never enters gold and is out
of both recall and leak-rate scoring.

## What it does

- Runs anonde + Presidio over the English MAPA slice.
- Emits per-engine findings in the uniform JSONL schema.
- Computes precision / recall / F1 per entity type (span-exact match).

## Layout

```
bench/corpora/mapa_en/
├── README.md           ← this file
├── Makefile            ← entry point; calls the shared loader + runners
├── cmd/
│   └── fetch_mapa.py    ← SHARED loader for every mapa_* corpus
└── data/               ← (gitignored) corpus + per-engine findings
```

The Go runner (`bench/runners/anonde.go`) and Python runners
(`bench/runners/presidio.py`, etc.) are shared with every other corpus.

## Reproduction

```bash
# Fetch the English slice.
make -C bench/corpora/mapa_en data

# Run anonde (patterns + GLiNER is the production stack).
make -C bench/corpora/mapa_en anonde ANONDE_BACKEND=gliner

# Run Presidio (needs the English spaCy model).
python -m spacy download en_core_web_lg
make -C bench/corpora/mapa_en presidio

# Compare.
make -C bench/corpora/mapa_en report
```

Or via the matrix: `make -C bench matrix-en`.

## Schema

Each gold line:
`{"id":"...","text":"...","entities":[{"start":N,"end":M,"type":"..."}, ...]}`
— codepoint offsets, canonical anonde/Presidio entity types. Findings
JSONL matches every other corpus; see `bench/corpora/ai4privacy_en/README.md`.
