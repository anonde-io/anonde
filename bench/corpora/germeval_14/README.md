# bench/corpora/germeval_14

GermEval 2014 German NER as the open-license German strict-F1 anchor.

## Why

CoNLL-2003 DE (Frankfurter Rundschau) is the canonical German NER
benchmark every paper cites, but it's licensed-restricted (LDC
distribution, no public HF mirror). GermEval-14 is the open-license
equivalent: same canonical PER / LOC / ORG entities, comparable
sentence count, expert annotation, CC-BY 4.0 (no DUA, no auth).

Adding it gives the bench:
- A standard-corpus F1 number an academic reader can compare to any
  German NER paper published since 2014.
- A second open German gold corpus alongside `wikiann_de`. WikiAnn is
  anchor-link-induced (noisy boundaries, ~1 entity / sentence);
  GermEval is hand-annotated and denser (~3 entities / sentence),
  making strict F1 more discriminating.

## Caveats — read before quoting numbers

- GermEval has *sub-tags* for derived (`PERderiv`, `LOCderiv`,
  `ORGderiv`) and part-of (`LOCpart`, …) entities. We collapse these
  to their base type (PER / LOC / ORG) before scoring. This matches
  the convention every comparable paper uses but is *not* identical to
  the strictest GermEval 2014 shared task definition (which scored sub-
  tags separately). Cite this as "GermEval-14 outer F1" if pressed.
- The `OTH` (other) tag is dropped because anonde has no canonical
  mapping for it — `OTH` includes nationalities, languages, products,
  events; collapsing all to a single anonde type would over-count both
  precision and recall in misleading ways.
- The corpus is sentence-level (not paragraph-level). Average length
  ~15 tokens. GLiNER's recall benefits from longer context; expect
  scores here to underestimate full-document performance.

## Run

```bash
# fetch + emit corpus.jsonl
make -C bench/corpora/germeval_14 data

# include in the matrix (already wired in bench/Makefile)
make -C bench corpus-germeval_14
```

## Data provenance

- Source: `gwlms/germeval2014` on Hugging Face Datasets (mirror of
  Benikova et al. 2014, "NoSta-D Named Entity Annotation for German").
- License: **CC-BY 4.0**. Data may be cached locally; we use streaming
  only to keep the repo lean.
- Sample: 300 sentences from the `test` split, deterministic via
  `--seed 20260515`. We fall back to `validation` and `train` if `test`
  doesn't materialise on a given HF revision.

## Sample document

```json
{
  "id": "germeval-14-0000",
  "text": "Sie wurde im Auftrag der Stadt Hamburg von Peter Müller errichtet .",
  "entities": [
    {"start": 25, "end": 38, "type": "LOCATION"},
    {"start": 42, "end": 55, "type": "PERSON"}
  ]
}
```

Note that `Stadt Hamburg` (city + name) is annotated as one
`LOCATION` span in GermEval; anonde patterns + GLiNER tend to emit
just `Hamburg`. Strict F1 will penalise this; type-agnostic leak rate
will count it as caught.
