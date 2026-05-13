# bench/corpora/ggponc_de — GGPONC 2.0 precision probe

GGPONC 2.0 is the **German Guideline Program in Oncology NLP Corpus** —
~30k sentences of German oncology clinical guideline text with medical
NER annotations (Diagnosis, Treatment, Drug, etc.).

## What this bench tests

GGPONC documents are *authored guidelines*, not patient records — they
have **no PHI by construction**, similar to bench/corpora/wiki_de. But unlike
Wikipedia, they are dense with real medical content: drug names, dosing
schemes, study citations, eponymous diagnoses, historical figure names.

This makes it a sharper precision probe than Wikipedia for anonde:
**if the anomaly recognizer can stay quiet on dense medical guideline
prose, it's well-tuned for clinical context**. If it over-fires here
the way it does on Wikipedia, the over-firing is structural (not
encyclopedic-specific).

Note: GGPONC's own annotations are for medical *concepts* — not PHI.
We ignore them; they're just text source for us.

## Get the data

Unlike `bench/corpora/openmed` (Zenodo zip) or `bench/corpora/wiki_de`/`bench/corpora/pmc_de` (public
APIs), GGPONC is **gated behind an email registration form** at:

  https://www.leitlinienprogramm-onkologie.de/projekte/ggponc-english

You fill out an email request, agree to attribution + non-commercial
research use, and they send you a download link (usually within a few
business days).

Once you have the corpus archive, extract it under `data/raw/`:

```bash
mkdir -p bench/corpora/ggponc_de/data/raw
unzip -d bench/corpora/ggponc_de/data/raw ggponc2.0_<version>.zip
```

The repo expects one of the standard GGPONC distribution layouts —
either `plain/*.txt` (plain text per guideline) or `json/*.json`. The
loader auto-detects.

## Run

```bash
# 1. One-time: register + drop files in bench/corpora/ggponc_de/data/raw/
# 2. Run the bench (uses the same patterns-only runner as the others)
make -C bench/corpora/ggponc_de all
open bench/corpora/ggponc_de/REPORT.md
```

## Read the result

Same metrics as `bench/corpora/wiki_de` (precision-probe view — no gold PHI to F1
against):

| Metric | Suspicious | OK | Great |
|---|---|---|---:|
| Avg findings / doc | > 30 | 5–20 | < 5 |
| Docs with ≥1 finding | > 80% | 30–60% | < 20% |

## Pair with the other benches

| Bench | Has PHI? | Tells us |
|---|---|---|
| `bench/corpora/openmed/` (GraSCCo) | yes (gold) | recall on discharge letters |
| `bench/corpora/synth_clinical/` | yes (gold by construction) | recall on 4 clinical sublanguages |
| `bench/corpora/pmc_de/` | no (precision probe) | over-fire on case reports |
| `bench/corpora/wiki_de/` | no (precision probe) | over-fire on encyclopedic prose |
| `bench/corpora/ggponc_de/` | no (precision probe) | over-fire on dense medical guidelines |

If GGPONC over-fires like Wikipedia, the anomaly detector hits any
narrative German with medical jargon — the issue isn't domain, it's
the pattern itself. If GGPONC stays clean while Wikipedia over-fires,
the architecture is specifically tuned to medical text and just doesn't
recognise medical context in Wikipedia.
