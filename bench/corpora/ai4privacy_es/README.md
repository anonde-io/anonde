# bench/corpora/ai4privacy_es — Spanish PII bench (ai4privacy)

The Spanish slice of the
[`ai4privacy/pii-masking-300k`](https://huggingface.co/datasets/ai4privacy/pii-masking-300k)
corpus. Phase 1 of the multilingual bench expansion: gives the matrix a
cross-language general-PII number alongside `ai4privacy_en`.

This is a **thin wrapper** corpus. The dataset, the gold schema, the
label mapping, and the loader are identical to `ai4privacy_en` — only
the language slice differs. The loader is shared:

    ../ai4privacy_en/cmd/fetch_pii_masking.py --language es

`ai4privacy/pii-masking-300k` interleaves en/fr/de/it/es/nl rows across
its `train` + `validation` splits, tagged by a per-row `language`
column. The shared loader filters to the requested slice. Spanish is
**new in the 300k release** (the older 200k dataset shipped no Spanish
split) — this corpus exists because the bench moved to 300k.

## What it does

- Runs anonde + Presidio over the Spanish ai4privacy slice.
- Emits per-engine findings in the uniform JSONL schema.
- Computes precision / recall / F1 per entity type (span-exact match).

## Layout

```
bench/corpora/ai4privacy_es/
├── README.md           ← this file
├── Makefile            ← thin wrapper; calls the shared loader + runners
└── data/               ← (gitignored) corpus + per-engine findings
```

## Reproduction

```bash
# Fetch the Spanish slice.
make -C bench/corpora/ai4privacy_es data

# Run anonde (patterns + GLiNER is the production stack).
make -C bench/corpora/ai4privacy_es anonde ANONDE_BACKEND=gliner

# Run Presidio (needs the Spanish spaCy model).
python -m spacy download es_core_news_lg
make -C bench/corpora/ai4privacy_es presidio

# Compare.
make -C bench/corpora/ai4privacy_es report
```

Or via the matrix: `make -C bench matrix-es`.

## Schema

Gold and findings JSONL match `ai4privacy_en` exactly — see
`bench/corpora/ai4privacy_en/README.md` for the schema.
