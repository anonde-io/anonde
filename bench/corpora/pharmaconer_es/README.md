# bench/corpora/pharmaconer_es

Spanish clinical NER as a **precision probe** on real (not synthetic)
case-report prose. Adds Spanish-language coverage to the bench, which
was previously DE + EN only.

## Why

Every other corpus in the bench is German (`openmed`, `wiki_de`,
`pmc_de`, `synth_clinical`, `finance_de`, `legal_de`, `wikiann_de`,
`ggponc_de`) or English (`ai4privacy_en`). Anonde claims local-first
PII coverage; without a Spanish cell we have no answer to "does it
generalise to a third language with similar Romance morphology to
nothing in the training mix?".

PharmaCoNER is a natural fit:

- 1000 real clinical case reports manually annotated in 2019
- Open license (CC-BY 4.0) — distributable
- Already on Hugging Face (parquet conversion accessible)
- Spanish biomedical sublanguage — interesting drift from anonde's
  training prior

## Caveats — read before quoting numbers

- PharmaCoNER's gold annotation layer is **chemical / pharmacological
  substances + proteins only**. It carries NO PERSON / LOCATION /
  ORGANIZATION spans. The clinical case reports are de-identified
  before publication, so there's no actual PHI in the source text
  either.
- This corpus therefore **cannot** answer "does anonde catch Spanish
  PHI?" — only "does anonde over-fire on Spanish clinical prose?".
  Concretely a precision probe in the same family as `wiki_de` and
  `pmc_de`.
- Expected number on this corpus: very low findings/doc. Any PERSON
  or LOCATION finding is almost certainly a false positive (or, in
  rare cases, a real de-id leak the original annotators missed —
  worth surfacing).
- The chemical / drug mentions ARE emitted in `entities[].type =
  "OTHER"`. The bench scorer recognises `OTHER` as a non-canonical
  bucket (see `bench/scoring/compare.py`) — it won't be matched
  against PERSON / LOCATION / ORGANIZATION findings, but it remains
  available for a follow-up analysis of "where do anonde's FPs sit
  in chemical-name space?".
- Protein spans (`PROTEINAS`) and ambiguous spans (`UNCLEAR`) are
  dropped during conversion — they aren't PHI and aren't useful as
  noise overlap.

## Read the result

Use the same scale as `wiki_de` / `pmc_de`:

| Metric                | Suspicious | OK     | Great |
|-----------------------|------------|--------|------:|
| Avg findings / doc    | > 30       | 5-15   | < 5   |
| Docs with >=1 finding | > 80%      | 30-60% | < 20% |

If `pharmaconer_es` numbers track `pmc_de` closely, that's evidence
the model behaviour generalises across European clinical
sublanguages. Big divergence between the two suggests Spanish-specific
over-firing (likely on the longer Spanish noun phrases or on chemical
names that mimic proper-noun casing).

## Run

```bash
# fetch + emit corpus.jsonl (200 docs, deterministic)
make -C bench/corpora/pharmaconer_es data

# include in the matrix (the parent bench/Makefile must list this
# corpus in its DE/EN partition — added separately, not here)
make -C bench corpus-pharmaconer_es
```

## Data provenance

- **Source:** PharmaCoNER (IberLEF 2019 shared task), curated by the
  Barcelona Supercomputing Center NLP group / Plan-TL.
- **Zenodo (canonical):**
  <https://zenodo.org/records/4270158> — "PharmaCoNER corpus: gold
  standard annotations of Pharmacological Substances, Compounds and
  proteins in Spanish clinical case reports".
- **License:** Creative Commons Attribution 4.0 International (CC-BY 4.0).
- **Hugging Face mirror used:** `bigbio/pharmaconer`, config
  `pharmaconer_bigbio_kb`, loaded via the parquet conversion at
  `refs/convert/parquet/pharmaconer_bigbio_kb/{train,validation}/0000.parquet`.
  The `PlanTL-GOB-ES/pharmaconer` and `bigbio/pharmaconer` repos both
  ship as legacy loading scripts that `datasets >= 4` refuses to
  execute; the parquet conversion is the supported path. No copy of
  the corpus is committed — the loader streams from HF at build
  time.
- **Paper:** Gonzalez-Agirre et al. (2019), "PharmaCoNER:
  Pharmacological Substances, Compounds and proteins Named Entity
  Recognition track", BioNLP-OST workshop at EMNLP-IJCNLP 2019.
  <https://aclanthology.org/D19-5701/>.

## Sample document

```json
{"id":"pharmaconer-es-0000",
 "text":"Paciente de 70 años de edad, minero jubilado, ...",
 "entities":[
   {"start":907,"end":920,"type":"OTHER"}
 ]}
```

The `"OTHER"` span here is the chemical mention `triglicéridos`. A
fully clean run would emit zero anonde findings on this document.
