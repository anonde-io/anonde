# bench/corpora/pmc_de — PubMed Central German case-report precision probe

German clinical case reports from PubMed Central Open Access — a real
clinical sublanguage that's **not** GraSCCo discharge-letter format.
No HuggingFace, no DUA, no API key: NCBI E-utilities is fully public.

## What this bench measures

Case reports are de-identified before publication. So any finding anonde
produces here is, like the wiki_de corpus, almost certainly a false
positive (or rarely, a real leak the authors overlooked — worth
logging).

The point of this bench: openmed shows anonde works on **discharge
letters** (GraSCCo). wiki_de showed it over-fires on **encyclopedic
narrative**. This bench fills the middle gap — does it generalise to
**another clinical sublanguage** (case reports), or is the architecture
specifically over-fit to discharge-letter format?

| Bench | Domain | Style | Tells us |
|---|---|---|---|
| `bench/corpora/openmed/` | clinical | discharge letters | recall, leak rate |
| `bench/corpora/pmc_de/` | clinical | case reports | precision on other clinical prose |
| `bench/corpora/wiki_de/` | encyclopedic | narrative | precision floor on non-clinical |

## Run

```bash
make -C bench/corpora/pmc_de all
open bench/corpora/pmc_de/REPORT.md
```

That fetches ~150 case-report PMCIDs from NCBI, downloads + extracts
their XML to plain text, filters out abstract-only-German papers
(keeping body-German text), runs anonde patterns-only, and writes the
report.

## Read the result

Same scale as wiki_de:

| Metric | Suspicious | OK | Great |
|---|---|---|---:|
| Avg findings / doc | > 30 | 5–15 | < 5 |
| Docs with ≥1 finding | > 80% | 30–60% | < 20% |

If pmc_de looks like wiki_de (very high findings/doc), the issue is
the multi-token statistical pattern over-fires on any narrative German,
not specifically Wikipedia. If pmc_de looks closer to clean, the issue
is encyclopedic-narrative-specific and case reports are fine.

## Customising the search

```bash
make -C bench/corpora/pmc_de data MAX_DOCS=500
# or a custom query:
python3 bench/corpora/pmc_de/download.py \
  --out bench/corpora/pmc_de/data/corpus.jsonl \
  --query "(case reports[PT]) AND ger[Language] AND cardiology[MeSH]"
```
