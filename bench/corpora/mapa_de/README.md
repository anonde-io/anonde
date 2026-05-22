# bench/corpora/mapa_de — German legal/administrative PII bench (MAPA)

The German slice of the
[`joelniklaus/mapa`](https://huggingface.co/datasets/joelniklaus/mapa)
corpus (CC-BY-4.0) — MAPA, the Multilingual Anonymisation for Public
Administration dataset. Phase 2 of the multilingual bench expansion:
gives the matrix a real-gold **legal / administrative** domain number
in German.

This is a **thin wrapper** corpus. The dataset, the gold schema, the
label mapping, and the loader are identical to `mapa_en` — only the
language slice differs. The loader is shared:

    ../mapa_en/cmd/fetch_mapa.py --language de

The synthetic German legal corpus `legal_de` stays alongside this one —
`mapa_de` is real EU legal/administrative text, `legal_de` is a
template-generated corpus; they probe different things.

## What it does

- Runs anonde + Presidio over the German MAPA slice.
- Emits per-engine findings in the uniform JSONL schema.
- Computes precision / recall / F1 per entity type (span-exact match).

## Coarse vs fine

Gold is built from MAPA's **coarse** annotation layer
(PERSON / ORGANISATION / ADDRESS / DATE; AMOUNT dropped as non-PII).
The fine-grained layer is not a clean leaf decomposition of the coarse
layer and is left informational — see `mapa_en/README.md` for the full
rationale.

## Layout

```
bench/corpora/mapa_de/
├── README.md           ← this file
├── Makefile            ← thin wrapper; calls the shared loader + runners
└── data/               ← (gitignored) corpus + per-engine findings
```

## Reproduction

```bash
# Fetch the German slice.
make -C bench/corpora/mapa_de data

# Run anonde (patterns + GLiNER is the production stack).
make -C bench/corpora/mapa_de anonde ANONDE_BACKEND=gliner

# Run Presidio (needs the German spaCy model).
python -m spacy download de_core_news_lg
make -C bench/corpora/mapa_de presidio

# Compare.
make -C bench/corpora/mapa_de report
```

Or via the matrix: `make -C bench matrix-de`.

## Schema

Gold and findings JSONL match `mapa_en` exactly — see
`bench/corpora/mapa_en/README.md` for the schema.
