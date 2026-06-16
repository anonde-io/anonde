# bench/corpora/openmed — GraSCCo PHI recall bench

End-to-end recall bench against GraSCCo_PHI (60 synthetic German clinical
letters with gold PHI annotations). This is the German clinical anchor
corpus — the hero in the matrix.

## Layout

```
bench/corpora/openmed/
├── README.md              this file
├── Makefile               make data / anonde / report / all
├── loader_grascco.py      Zenodo zip / CAS JSON -> data/corpus.jsonl
└── data/                  (gitignored) corpus + per-engine findings
```

The runner (`bench/runners/anonde.go`), comparator (`bench/scoring/compare.py`),
and label map (`bench/scoring/label_map.yaml`) are shared with every
other corpus.

## Run

```bash
pip install pyyaml
make -C bench/corpora/openmed all
open bench/corpora/openmed/REPORT.md
```

That downloads ~860 KB from Zenodo (one-time), loads 60 docs with
1300 gold PHI spans, runs anonde patterns-only, scores, and writes the
report.

## Knobs

```bash
# production stack: patterns + GLiNER PII (knowledgator/gliner-pii-base-v1.0)
make -C bench/corpora/openmed all ANONDE_BACKEND=gliner

# flat-decoder GLiNER (knowledgator/gliner-pii-large-v1.0 and other 4-input BIO exports)
make -C bench/corpora/openmed all ANONDE_BACKEND=gliner-flat

# re-score without re-running anonde
rm bench/corpora/openmed/REPORT.md && make -C bench/corpora/openmed report
```

To score this corpus against every engine in one go, run the
[top-level matrix](../../README.md) instead:

```bash
make -C bench corpus-openmed
```

## Metric views

`compare.py` reports three views per (entity-type):

- **Strict**     exact start, end, type
- **Partial**    any overlap, same canonical type
- **Type-only**  any overlap, type attributed to prediction

Plus a leak rate: gold spans with no overlapping prediction (the
anonymisation failure rate).
