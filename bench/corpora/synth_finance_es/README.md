# bench/corpora/synth_finance_es — Spanish slice of the synthetic financial bench

Phase 3 of the multilingual bench expansion. This corpus is a thin
wrapper: the generator, templates and vocab are **shared** with every
`synth_finance_*` corpus and live in `../synth_finance_en/`. This
directory only carries a Makefile that invokes the shared generator with
`--language es`.

See **[`../synth_finance_en/README.md`](../synth_finance_en/README.md)**
for the full design: doc types, canonical gold types, the MOD-97 / Luhn
checksum guarantees, and the no-monetary-PII rule.

```bash
make -C bench/corpora/synth_finance_es all
open bench/corpora/synth_finance_es/REPORT.md
```
