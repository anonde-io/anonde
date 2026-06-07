# SCAFFOLD — finance / legal label-set eval (NOT a runnable corpus)

This directory is a **placeholder**, not a corpus — it documents where a real
finance/legal eval corpus plugs into the matrix once one exists. The leading
underscore keeps it out of the `$(ALL_CORPORA)` partitions in `bench/Makefile`,
so `make matrix` ignores it.

## Why there is no finance/legal number yet

The runner, `bench/Makefile`, and `bench-full.yml` accept `LABEL_SET=finance`
and `LABEL_SET=legal` end-to-end (`recognizers.FinancePIILabels` /
`LegalPIILabels` are wired). What's missing is a **real eval corpus with gold
spans** for those entity types — without gold there's nothing to score, and we
do not synthesize one. The eval is blocked on a real corpus landing.

Candidate sources:
  - finance: a real-gold financial-document de-id set (invoices, statements,
    KYC). `synth_finance_*` exist but are gold-by-construction, not real-world.
  - legal: `mapa_*` (real-gold) and synthetic `legal_de` exist — a probe could
    run against MAPA first, but its gold schema must be checked against
    `LegalPIILabelToEntity` before the number is trustworthy.

## How to wire a real corpus when it lands

1. Create `bench/corpora/<NAME>/` with a loader + gold (mirror an existing
   corpus, e.g. `bench/corpora/synth_finance_en/` or `bench/corpora/mapa_en/`).
   Gold entity types must map through `bench/scoring/label_map.yaml` to the
   same canonical types `FinancePIILabelToEntity` / `LegalPIILabelToEntity`
   emit, or the score is meaningless.
2. Add `<NAME>` to the matching `*_CORPORA` partition in `bench/Makefile`.
3. Give its cell the right label set. The cleanest hook is a per-corpus
   `LABEL_SET_FOR` macro keyed on the corpus name (sketch is in the
   FINANCE/LEGAL SCAFFOLD block in `bench/Makefile`), defaulting to the
   global `LABEL_SET` so every existing clinical cell is unaffected.
4. In `.github/workflows/bench-full.yml`, add `<NAME>` to the `bench-cell`
   matrix and (if the cell needs a non-clinical set) pass `LABEL_SET=...`
   on the `make -C bench corpus-<NAME>` line, the same way the clinical
   corpora pin `LABEL_SET=clinical` today.

## Ad-hoc probe (diagnostic only, not a scored matrix number)

```
make -C bench/corpora/<existing-corpus> all \
  ANONDE_BACKEND=gliner LABEL_SET=finance
```

This runs the finance label set against an existing corpus's text, but the
score is only meaningful if that corpus's gold matches the finance label
schema — otherwise treat the output as a prompt-coverage smoke test, not a
leak-rate measurement.
