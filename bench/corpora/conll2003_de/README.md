# bench/corpora/conll2003_de

German Frankfurter Rundschau newswire NER as a **precision probe** on
real natural text, with gold spans from the canonical CoNLL-2003 shared
task.

**Status: gated.** The German split of CoNLL-2003 (Frankfurter Rundschau
1992) is distributed by the LDC under a research-only license that
requires registration; at time of writing there is no public Hugging
Face mirror that hosts the gold annotations. `loader.py` tries a list
of community-mirror candidates and exits gracefully (code 2) with a
clear message if none resolve. The bench harness treats exit-2 as
"corpus unavailable, skip" rather than a hard failure.

## Why

`wikiann_de` is the only other German precision probe in the bench, and
its gold annotations are heuristic (Wikipedia-anchor-link induced).
CoNLL-2003 DE is the canonical hand-annotated German NER benchmark and
every German NER paper benchmarks against it — having both would let
us cross-check whether anonde's precision regressions are real or
artefacts of WikiAnn's noisy gold. We sample 300 sentences
(deterministic via `--seed`) from the `test` split to match the
sample size of every other DE corpus in the bench.

## Recommended alternative

Until a CoNLL-2003 DE mirror reappears, prefer
[`bench/corpora/germeval_14/`](../germeval_14/) — the GermEval 2014
German NER shared-task corpus is open-license (CC-BY 4.0), hand
annotated, and uses the same PER / LOC / ORG entity types plus
GermEval-specific subtypes (PERderiv, LOCpart, …) we collapse to the
canonical anonde three.

## Caveats — read before quoting numbers

- **License-gated.** Original distribution requires an LDC license. The
  bench will only succeed end-to-end if a mirror resolves; otherwise
  the loader skips with exit code 2 and the harness omits this cell.
- **BIO-precision-only gold (if a mirror works).** CoNLL-2003 gold is
  PER / LOC / ORG / MISC; we map PER → PERSON, LOC → LOCATION, ORG →
  ORGANIZATION and **drop MISC** (no clean anonde canonical mapping).
  Anonde emits 12+ entity types — any anonde finding outside PERSON /
  LOCATION / ORGANIZATION counts as a false positive in this scoring
  even when legitimate. So **precision on this corpus is a LOWER
  BOUND**; true precision restricted to PER / LOC / ORG is higher.
- **Sentence-level, not paragraph-level.** Average length ~17 tokens.
  GLiNER's recall benefits from longer context; expect its scores here
  to underestimate full-document performance.
- **Domain bias.** Frankfurter Rundschau 1992 articles are political /
  business prose with high named-entity density — not representative of
  general-purpose German.

## Run

```bash
# attempt fetch + emit corpus.jsonl. Exits 2 with a clear message if
# no mirror resolves; in that case the bench harness skips this cell.
make -C bench/corpora/conll2003_de data

# include in the matrix (conll2003_de must be in bench/Makefile's
# DE_CORPORA list — parent agent will register it). The harness must
# tolerate a missing corpus.jsonl for the gated-failure case.
make -C bench corpus-conll2003_de
```

To use a local licensed copy: drop a hand-built `data/corpus.jsonl`
with one `{id, text, entities}` JSON per line (entity types in the
canonical PERSON / LOCATION / ORGANIZATION set) into this folder; the
Makefile will not re-fetch if the file already exists.

## Data provenance

- Source (original): CoNLL-2003 shared-task, German split. Frankfurter
  Rundschau newswire, 1992. Tjong Kim Sang & De Meulder 2003.
- Distribution: LDC catalog (LDC2003T13 / via the original shared-task
  download which is no longer publicly accessible). Research-only
  license; registration required.
- Mirror candidates probed (none resolved as of 2026-05):
  `tomaarsen/conll2003[de]`, `MalumaDev/conll2003-german`,
  `flozi00/conll2003_german`, `severo/conll2003_german`,
  `PaDaS-Lab/conll2003-german`.
- Tag set: PER / LOC / ORG / MISC; we drop MISC and canonicalise the
  rest to PERSON / LOCATION / ORGANIZATION.

## Sample document

When a mirror is available, output will look like:

```json
{"id":"conll2003-de-0000",
 "text":"Bundeskanzler Helmut Kohl traf am Montag in Bonn ein .",
 "entities":[
   {"start":14,"end":25,"type":"PERSON"},
   {"start":44,"end":48,"type":"LOCATION"}
 ]}
```
