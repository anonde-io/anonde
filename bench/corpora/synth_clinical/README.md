# bench/corpora/synth_clinical — synthetic German clinical bench across 4 sublanguages

GraSCCo is one clinical sublanguage: hospital discharge letters. This
bench tests whether anonde generalises to **other** clinical writing
styles, with **gold by construction** — slot-based templates so every
piece of PHI in the generated text has a known (start, end, type).

Four sublanguages, all in German:

| Key | Sublanguage | Characteristic |
|---|---|---|
| `ed_triage` | Notaufnahme triage notes | short, bulleted, dense PHI in header |
| `op_report` | OP-Berichte (surgical reports) | structured operative narrative |
| `radiology` | Radiologie-Befunde | findings prose with referring doctor |
| `rehab_discharge` | Reha-Entlassungsberichte | longest, like GraSCCo but rehab-style |

## Why synthetic and not real

We cover this in `bench/corpora/pmc_de` — real, gold-labelled German clinical text
without a DUA is genuinely scarce. GraSCCo itself is synthetic (60
docs); bench/corpora/openmed tests recall against it. This bench complements that
by **broadening the sublanguage**: same gold-by-construction approach,
4× the variety in style and structure.

## Run

```bash
make -C bench/corpora/synth_clinical all
open bench/corpora/synth_clinical/REPORT.md
```

Default config: 30 docs per sublanguage = 120 docs total, deterministic
(`SEED=20260512`). Override:

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
  names than our 50-entry list.

## What this bench proves (and doesn't)

✅ Proves: anonde catches GraSCCo-style PHI when re-styled to ED triage,
   surgery, radiology, rehab sublanguages.

❌ Does NOT prove: anonde catches **real-world** German clinical PHI.
   For that, bench/corpora/openmed (real GraSCCo data) is the canonical bench.
   This corpus is generator-bounded — it can only test the patterns the
   generator can produce.
