# bench/corpora/synth_clinical — synthetic clinical bench, 4 languages

This bench tests whether anonde generalises across clinical writing
styles, with **gold by construction** — slot-based templates so every
piece of PHI in the generated text has a known (start, end, type).

Four clinical sublanguages, in four languages:

| Key | Sublanguage | Characteristic |
|---|---|---|
| `ed_triage` | Emergency-department triage notes | short, bulleted, dense PHI in header |
| `op_report` | Operative reports | structured operative narrative |
| `radiology` | Radiology findings | findings prose with referring doctor |
| `rehab_discharge` | Rehab discharge letters | longest, like GraSCCo but rehab-style |

## Phase 4: this directory is the canonical generator home

Phase 4 of the multilingual bench expansion turned `synth_clinical`
into the **canonical home of the shared clinical generator**:
`generate.py` + `generators.py` + `templates.py`. The three sibling
corpora — `synth_clinical_{en,fr,it}` — are thin Makefile wrappers that
invoke this same generator with a different `--language`. There is no
per-language copy of the generator (mirrors the `synth_finance_*` /
`ai4privacy_*` / `mapa_*` shared-loader pattern).

`synth_clinical` itself stays the **German** slice (`LANGUAGE=de`) and
is the per-push regression anchor in `.github/workflows/bench.yml`.

**There is deliberately no `synth_clinical_es`** — Spanish clinical PHI
is covered by the real-gold **MEDDOCAN** corpus,
`bench/corpora/meddocan_es` (IberLEF 2019 shared task). A synthetic
Spanish clinical corpus would be strictly weaker than real gold.

### Regression contract

`synth_clinical` (the German corpus) is a per-push regression anchor.
The Phase 4 refactor that parametrised the generator with `--language`
keeps `--language de` (the default) **byte-identical** to the
pre-refactor single-language generator at a fixed seed:

- the German RNG stream uses the bare `--seed` (no language fold), so
  the rng draw sequence is unchanged;
- en/fr/it fold the language into the seed so the four corpora don't
  share an identical RNG stream — `de` is the explicit exception;
- the German vocab and templates are copied verbatim into the `de`
  blocks of `generators.py` / `templates.py`.

## Why synthetic and not real

We cover this in `bench/corpora/pmc_de` — real, gold-labelled clinical
text without a DUA is genuinely scarce. GraSCCo itself is synthetic (60
docs); `bench/corpora/openmed` tests recall against it. This bench
complements that by **broadening the sublanguage**: same
gold-by-construction approach, 4× the variety in style and structure,
now also across 4 languages. For real clinical gold, see `openmed`
(German, GraSCCo) and `meddocan_es` (Spanish, MEDDOCAN).

## Run

```bash
make -C bench/corpora/synth_clinical    all   # German  (regression anchor)
make -C bench/corpora/synth_clinical_en all   # English
make -C bench/corpora/synth_clinical_fr all   # French
make -C bench/corpora/synth_clinical_it all   # Italian
open bench/corpora/synth_clinical/REPORT.md
```

Default config: 30 docs per sublanguage = 120 docs per language,
deterministic (`SEED=20260512`). Override:

```bash
make -C bench/corpora/synth_clinical all PER_SUBLANGUAGE=100  # 400 docs
make -C bench/corpora/synth_clinical all SEED=42              # different generation
```

## Read the result

`REPORT.md` is the same shape as `bench/corpora/openmed/REPORT.md`:

- **Strict** (exact start+end+type), **Partial** (overlap + type), and
  **Type-only** (overlap + prediction's type) F1 tables per entity.
- **Anonymisation leak rate** — share of gold spans no prediction
  overlaps. Lower is better; **GraSCCo baseline ~21.5%**.

A few interpretation notes:

- IDs and dates in this corpus are **densely placed** — the bench
  stresses the date-context fallback hard. Expect more bare-year
  date-context false positives than on GraSCCo.
- The patient address slot is split across LOCATION_STREET +
  LOCATION_ZIP + LOCATION_CITY in three adjacent gold spans. Partial F1
  is fairer than strict here.
- All "person" fills are drawn from the same name pool, so PERSON
  recall on this bench is an **upper bound** — real text contains rarer
  names than our ~50-entry list.

## What this bench proves (and doesn't)

✅ Proves: anonde catches GraSCCo-style PHI when re-styled to ED triage,
   surgery, radiology, rehab sublanguages — across German, English,
   French and Italian.

❌ Does NOT prove: anonde catches **real-world** clinical PHI. For that,
   `bench/corpora/openmed` (real GraSCCo data) and
   `bench/corpora/meddocan_es` (real MEDDOCAN data) are the canonical
   real-gold clinical benches. This corpus is generator-bounded — it can
   only test the patterns the generator can produce.
