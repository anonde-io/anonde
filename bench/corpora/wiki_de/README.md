# bench/corpora/wiki_de — Wikipedia DE precision probe

Real German medical text from a different source than GraSCCo. Tests
whether anonde's recognizers (especially the anomaly detector) over-fire
on real clinical prose. Free, no authentication: the German Wikipedia
MediaWiki API is fully public.

## What this bench measures

Wikipedia medical articles have **no PHI by construction**. Every
finding anonde produces here is therefore:

- (a) Almost certainly a false positive (jargon flagged as PERSON, an
      eponym mistaken for a name, etc.)
- (b) Very rarely, a real PII leak by the article author (worth logging).

The bench reports per-type FP counts, per-doc finding distribution, and
a sample of findings for human review of FP patterns.

## Run

```bash
pip install pyyaml             # if not already
make -C bench/corpora/wiki_de all
open bench/corpora/wiki_de/REPORT.md
```

That downloads ~100 German medical articles (~5 MB) from Wikipedia
through the public API, runs anonde patterns-only against them, and
writes the report.

## Read the result

| Metric | Suspicious | OK | Great |
|---|---|---|---|
| Avg findings / doc | > 50 | 5–20 | < 5 |
| Docs with ≥1 finding | > 80% | 30–60% | < 20% |

Wikipedia articles average ~5 KB. A clean run with **< 5 findings/doc on
average and < 20% of docs flagged** is strong evidence the architecture
isn't overfit to GraSCCo. Higher numbers point to over-aggressive
recognizers — usually the anomaly detector flagging German technical
terms not in its vocabulary kernel.

## Pair with the openmed corpus

| Bench | Tests | Metric |
|---|---|---|
| `bench/corpora/openmed/` | Recall: did we catch all the PHI? | leak rate, per-entity F1 |
| `bench/corpora/wiki_de/` | Precision: do we over-fire on non-PHI? | findings/doc |

Run both to bracket the architecture, or use the
[top-level matrix](../../README.md) to score every engine on every corpus
in one go.
