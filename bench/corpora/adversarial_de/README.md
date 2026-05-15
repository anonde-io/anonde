# bench/corpora/adversarial_de

Adversarial / out-of-distribution probe derived from `synth_clinical` by
deterministically perturbing the input text along six axes that show up
in real production traffic but never in clean gold corpora.

## Why

Every other gold German corpus in the bench (`synth_clinical`,
`finance_de`, `legal_de`, `openmed`) ships clean well-formed text:
correctly-spelled names, ASCII-clean dates, single-space tokenisation,
canonical umlauts. Real production traffic isn't clean. Users type
*Mueller* for *Müller*, paste log lines with ANSI escape sequences, mix
English clauses into German notes, fat-finger names. A bench that only
scores on clean text overstates production recall.

This corpus re-annotates a 50-doc sample of `synth_clinical` through six
perturbations (one perturbed copy per kind × 50 inputs = 300 docs).
Entity offsets are recomputed after every perturbation so the gold
annotations track the perturbed surface form.

## Perturbations

| Kind | What it does | Why it matters |
|---|---|---|
| `typo_inside_pii` | Swap two adjacent chars at the midpoint of every PII span ≥3 chars | Tests recall when names are misspelled (Andrea Schwarz → Andrea cShwarz). NER-only engines tend to absorb the typo; strict-regex recognizers may miss the span entirely. |
| `transliteration_de_en` | ä→ae, ö→oe, ü→ue, ß→ss across the text | Tests that "Mueller" gets the same PERSON treatment as "Müller". Many German speakers ASCII-fold their names in foreign systems. |
| `case_scramble` | Flip case of every other alphabetic character | Tests robustness to ALL-CAPS / sCrAmBlEd casing that appears in logs and quick-typed notes. NER models conditioned on canonical casing degrade here. |
| `ansi_insertion` | Wrap each PII span with `\x1b[31m...\x1b[0m` | Tests `content_format=text` mode against logs that retained ANSI. Anonde's logs format strips ANSI; text format should not over-redact the escape bytes. |
| `code_switching` | Inject 3-5 English sentences between German ones | Tests German recognizers don't false-negative on English context. Multilingual model variants in particular should be robust. |
| `whitespace_normalization` | Replace one space inside each PII span with NBSP (U+00A0) | Tests that "Karin Müller" with a non-breaking space is still treated as one PERSON, not two. Copy-paste from PDFs frequently introduces NBSPs. |

## Caveats

- This is a **stress test**, not a representative sample. A 10 pp leak
  rate increase here may correspond to a 1 pp regression on actual prod
  traffic; calibrate against the other corpora.
- Offsets are recomputed but a sloppy regex recognizer that returns a
  span aligned to NFC-normalised text may see different boundaries.
  We do not normalise — perturbations are raw codepoint-level.
- Output is fully deterministic given the seed. Re-running with the
  same seed produces identical JSONL.

## Run

```bash
# Build the source corpus first if it isn't already there:
make -C bench/corpora/synth_clinical data

# Then materialise the perturbed corpus:
make -C bench/corpora/adversarial_de data

# Or via the matrix:
make -C bench corpus-adversarial_de
```

## Data provenance

Derived from `bench/corpora/synth_clinical/`, which is itself generated
by the in-tree template generator (no external download). License of
the derivation matches the source: in-tree, MIT.

## Sample document

```json
{
  "id": "adv-de-typo-0000",
  "perturbation": "typo_inside_pii",
  "text": "St. Marien-Kraknenhaus Siegen\nNotaufnahme — Triage-Notiz\n\nPatient: Andrea cShwarz\nGeb.-Datum: 01.112.007",
  "entities": [
    {"start": 0, "end": 29, "type": "LOCATION_HOSPITAL"},
    {"start": 67, "end": 81, "type": "NAME_PATIENT"},
    {"start": 88, "end": 98, "type": "DATE_BIRTH"}
  ]
}
```

Notice how the typo (`Krankenhaus` → `Kraknenhaus`, `Schwarz` →
`cShwarz`) is *inside* the entity span — that's the point. A pattern
recognizer keyed on the exact surface form will miss this; a NER model
trained on character n-grams or sub-words should still catch it.
