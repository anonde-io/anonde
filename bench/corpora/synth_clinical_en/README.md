# bench/corpora/synth_clinical_en — English slice of the synthetic clinical bench

Phase 4 of the multilingual bench expansion. This corpus is a thin
wrapper: the generator, templates and vocab are **shared** with every
`synth_clinical_*` corpus and live in `../synth_clinical/`. This
directory only carries a Makefile that invokes the shared generator with
`--language en`.

See **[`../synth_clinical/README.md`](../synth_clinical/README.md)** for
the full design: the four clinical sublanguages (ED triage, OP report,
radiology, rehab discharge), the gold-by-construction slot scheme, and
the German regression contract.

```bash
make -C bench/corpora/synth_clinical_en all
open bench/corpora/synth_clinical_en/REPORT.md
```
