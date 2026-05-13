# bench/corpora/wikiann_de

German Wikipedia NER as a **precision probe** on real natural text.

## Why

Every other gold-annotated DE corpus in the bench is synthetic
(`synth_clinical`, `finance_de`, `legal_de`) or near-synthetic
(`openmed`/GraSCCo is real PHI annotation on synthetic clinical text).
That answers "does anonde catch the PII we generated?" but not "does
anonde correctly avoid flagging non-PII on real German prose?".

WikiAnn (a.k.a. PAN-X) is the standard multilingual NER benchmark
derived from Wikipedia anchor links. The German split has ~30 k
sentences with PER / LOC / ORG spans validated by the upstream
Rahman+Wang construction. We sample 300 sentences (deterministic via
`--seed`) for a bench cell that runs in ~3 min total across patterns +
gliner + gliner-py.

## Caveats — read before quoting numbers

- WikiAnn gold is **PER / LOC / ORG only**. Anonde emits 12+ entity
  types. Any anonde finding outside PER / LOC / ORG counts as a false
  positive in this scoring even when it's a legitimate detection.
  Concretely: anonde finds "1962" as `DATE_TIME` → wikiann gold has
  no DATE → counted as FP. So **the precision number on this corpus
  is a LOWER BOUND**; true precision restricted to PER / LOC / ORG
  is higher.
- The corpus is sentences, not paragraphs. Average length ~10 tokens.
  GLiNER's recall benefits from longer context; expect its scores here
  to underestimate full-document performance.
- WikiAnn tags are heuristic (anchor-link induced), so some gold
  spans are noisy. A handful of anonde's "FP" findings are correct
  entities the upstream missed.

## Run

```bash
# fetch + emit corpus.jsonl
make -C bench/corpora/wikiann_de data

# include in the matrix (wikiann_de must be in bench/Makefile's
# DE_CORPORA list — already wired)
make -C bench corpus-wikiann_de
```

## Data provenance

- Source: `wikiann/de` on Hugging Face Datasets (mirror of
  `Rahimi/wikiann-en` / PAN-X by Rahimi et al. 2019).
- License: ODC-BY 1.0 (the dataset is derivative of Wikipedia).
- Distribution: streamed at bench build time; no copy committed.

## Sample document

```json
{"id":"wikiann-de-0000","text":"Sie wurde im Auftrag von Schiffsführer Erich Bossler ... ",
 "entities":[{"start":40,"end":54,"type":"PERSON"}]}
```
