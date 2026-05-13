# bench/corpora/ai4privacy_en — English PII parity vs Presidio

End-to-end comparison between anonde and Microsoft Presidio on the
[`ai4privacy/pii-masking-200k`](https://huggingface.co/datasets/ai4privacy/pii-masking-200k)
corpus (CC-BY-4.0). Implements the Presidio parity criteria from
`PRIVACY_VAULT_PHASED_PLAN.md`.

This bench used to live at `bench/parity/`. It now plugs into the
top-level matrix (`make -C bench matrix-en`).

## What it does

- Runs both engines over the same labeled corpus.
- Emits per-engine findings in a uniform JSONL schema.
- Computes precision / recall / F1 per entity type (entity-level, span-exact match).
- Writes a Markdown report to `REPORT.md`.

## Pass criteria (must all hold to claim parity)

- F1 ≥ Presidio default − 2 points for `PERSON`, `LOCATION`, `EMAIL_ADDRESS`,
  `US_SSN`, `IP_ADDRESS`, `CREDIT_CARD`.
- F1 ≥ Presidio default − 5 points for the regional entities
  (`UK_NHS`, `IT_FISCAL_CODE`, `ES_NIF`, `IN_PAN`, `AU_TFN`, …).
- No entity below F1 0.7 in absolute terms.
- Pattern-only throughput ≥ 5× Presidio's default.

If any criterion fails, **do not claim parity**. Investigate the failing
entity, fix the recognizer or context module, and rerun.

## Inputs

- Each gold line: `{"id":"...","text":"...","entities":[{"start":N,"end":M,"type":"..."}, ...]}`.
- A 5-row smoke fixture lives at `data/smoke.jsonl` for quickly
  exercising the harness end-to-end.

## Layout

```
bench/corpora/ai4privacy_en/
├── README.md           ← this file
├── Makefile            ← entry point; calls bench/runners/{anonde.go,presidio.py}
├── cmd/
│   ├── fetch_pii_masking.py    ← pulls the HF dataset, emits corpus.jsonl
│   └── gen_eval/main.go        ← builds a synthetic eval corpus from segments
├── data/                ← (gitignored) corpus + per-engine findings
└── REPORT.md            ← (gitignored) generated comparison report
```

The Go runner (`bench/runners/anonde.go`) and Python runners
(`bench/runners/presidio.py`, `bench/runners/gliner.py`,
`bench/runners/openai_pf.py`) are shared with every other corpus.

## Reproduction

From the repo root:

```bash
# 1. Fetch the corpus.
make -C bench/corpora/ai4privacy_en data

# 2. Run anonde (patterns + GLiNER is the production stack).
make -C bench/corpora/ai4privacy_en anonde ANONDE_BACKEND=gliner

# 3. Run Presidio (sidecar — Python 3.10+, presidio-analyzer installed).
python3 -m venv .venv-bench && . .venv-bench/bin/activate
pip install presidio-analyzer spacy
python -m spacy download en_core_web_lg
make -C bench/corpora/ai4privacy_en presidio

# 4. Compare.
make -C bench/corpora/ai4privacy_en report
```

Or the whole thing in one matrix cell:

```bash
make -C bench matrix-en
open bench/REPORT_MATRIX.md
```

## Schema

`findings.jsonl` (per-line):

```json
{
  "id": "doc-42",
  "engine": "anonde-gliner",
  "findings": [
    {"start": 12, "end": 28, "type": "EMAIL_ADDRESS", "score": 1.0}
  ],
  "duration_ms": 8.4
}
```
