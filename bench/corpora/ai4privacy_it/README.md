# bench/corpora/ai4privacy_it — Italian PII bench (ai4privacy)

The Italian slice of the
[`ai4privacy/pii-masking-300k`](https://huggingface.co/datasets/ai4privacy/pii-masking-300k)
corpus (CC-BY-4.0). Phase 1 of the multilingual bench expansion: gives
the matrix a cross-language general-PII number alongside `ai4privacy_en`.

This is a **thin wrapper** corpus. The dataset, the gold schema, the
label mapping, and the loader are identical to `ai4privacy_en` — only
the language slice differs. The loader is shared:

    ../ai4privacy_en/cmd/fetch_pii_masking.py --language it

`ai4privacy/pii-masking-300k` interleaves en/fr/de/it/es/nl rows across
its `train` + `validation` splits, tagged by a per-row `language` column
(stored as full English names — the loader maps the ISO code). The
shared loader filters to the requested slice. The 300k release added
Spanish, so there is a sibling `ai4privacy_es` corpus.

## What it does

- Runs anonde + Presidio over the Italian ai4privacy slice.
- Emits per-engine findings in the uniform JSONL schema.
- Computes precision / recall / F1 per entity type (span-exact match).

## Layout

```
bench/corpora/ai4privacy_it/
├── README.md           ← this file
├── Makefile            ← thin wrapper; calls the shared loader + runners
└── data/               ← (gitignored) corpus + per-engine findings
```

## Reproduction

```bash
# Fetch the Italian slice.
make -C bench/corpora/ai4privacy_it data

# Run anonde (patterns + GLiNER is the production stack).
make -C bench/corpora/ai4privacy_it anonde ANONDE_BACKEND=gliner

# Run Presidio (needs the Italian spaCy model).
python -m spacy download it_core_news_lg
make -C bench/corpora/ai4privacy_it presidio

# Compare.
make -C bench/corpora/ai4privacy_it report
```

Or via the matrix: `make -C bench matrix-it`.

## Schema

Gold and findings JSONL match `ai4privacy_en` exactly — see
`bench/corpora/ai4privacy_en/README.md` for the schema.
