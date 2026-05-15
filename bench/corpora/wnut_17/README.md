# bench/corpora/wnut_17

WNUT 2017 English emerging-entities NER as the noisy-text English
benchmark complement to CoNLL-2003 EN.

## Why

CoNLL-2003 EN is clean newswire — well-capitalised, well-formed, no
slang. Real production traffic often isn't that polished: support
tickets, social-media data, chat transcripts, user reports. WNUT-17
specifically targets *emerging entities* in *noisy user-generated*
text (tweets, Reddit, YouTube descriptions, StackExchange), so the
F1 here is a different statistic than the F1 on clean newswire.

Adding it gives the bench:
- A second open English gold corpus alongside `ai4privacy_en`.
  ai4privacy is template-generated (slot-filled synthetic PII);
  WNUT-17 is real noisy human text.
- A *noisy* English NER number to pair with a *clean* CoNLL-2003 EN
  number — together they bracket realistic production deployment
  expectations.

## Caveats — read before quoting numbers

- WNUT-17 has six entity types. We map:
  - `person` → PERSON
  - `location` → LOCATION
  - `corporation` → ORGANIZATION
  - `group` → ORGANIZATION (e.g. bands, sports teams)
  - `creative-work` → **dropped** (book titles, films — no PII)
  - `product` → **dropped** (gadget names — no PII)
  Anonde findings of `PROFESSION`, `DATE_TIME`, etc. on WNUT text are
  not penalised by leak rate (no gold to match) but *will* show up in
  the strict-F1 false-positive count. Read both metrics together.
- Test split is intentionally hard — many entities are non-CoNLL
  brands and emerging names absent from pretraining corpora. F1 here
  will be 20-30 pp lower than CoNLL-2003 EN for the same engine.
  That's a property of the data, not a regression.
- The corpus is sentence-level. Average length ~20 tokens. Some
  examples are heavily fragmented (one-word tweets); the BIO walker
  handles these but they contribute little signal.

## Run

```bash
# fetch + emit corpus.jsonl
make -C bench/corpora/wnut_17 data

# include in the matrix (already wired in bench/Makefile)
make -C bench corpus-wnut_17
```

## Data provenance

- Source: `leondz/wnut_17` on Hugging Face Datasets (parquet auto-
  conversion ref, since the canonical `wnut_17` builder is script-
  based and refused by `datasets >= 4`).
- License: **CC-BY 4.0**. From the W-NUT 2017 shared task on Novel
  & Emerging Entity Recognition (Derczynski et al., 2017).
- Sample: 300 sentences from the `test` split, deterministic via
  `--seed 20260515`. Reservoir sampling stops at 3000 streamed
  examples (test split has ~1287 — safe upper bound).

## Sample document

```json
{
  "id": "wnut-17-0000",
  "text": "The bodies of the soldiers were recovered after the concerted efforts of the Avalanche Rescue Teams ( ART ) , which is equipped to work in inhospitable terrain and weather",
  "entities": [
    {"start": 78, "end": 100, "type": "ORGANIZATION"},
    {"start": 103, "end": 106, "type": "ORGANIZATION"}
  ]
}
```

Note `Avalanche Rescue Teams` and its acronym `ART` are both annotated
as group/ORGANIZATION — exactly the kind of emerging entity that
pretrained NER models often miss.
