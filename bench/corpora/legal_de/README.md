# bench/corpora/legal_de — synthetic German legal bench across 5 document types

A counterpart to `synth_clinical`: same slot-based generation, same
gold-by-construction guarantees, but the **domain** swaps from clinical
prose to German legal prose. The goal is to answer one question the
clinical benches can't:

> Does anonde generalise from clinical text to legal text?

Five document types, all in German:

| Key | Document type | Characteristic |
|---|---|---|
| `klageschrift` | Statement of claim | structured header, dense party + court block |
| `beschluss` | Court order / ruling | short formal narrative, judge + parties + Az. |
| `vergleich` | Settlement agreement | dual-party signatures, IBAN for fee transfer |
| `vollmacht` | Power of attorney | grantor/grantee + Personalausweis + Steuer-ID |
| `anwaltsschreiben` | Lawyer's demand letter | letterhead boilerplate + deadline + IBAN |

## Why synthetic and not real

Real German legal text with gold-labelled PII annotations is essentially
unavailable: most court decisions are pseudonymised before publication
(parties anonymised to "K." / "B.", addresses removed), so there is
nothing left to recover. Pre-pseudonymisation drafts are protected by
attorney-client privilege and never released. Building a slot-based
generator is the only path to a German legal bench with **strict /
partial / type-only F1** rather than just a precision proxy.

## What's in here

```
legal_de/
├── Makefile       data / anonde / report / all / clean targets
├── README.md      this file
├── generate.py    main: walks templates, writes data/corpus.jsonl
├── generators.py  per-PII-type generators with bundled German vocab
├── templates.py   per-doctype headers/body/footer templates
└── data/          generated corpus + engine outputs (gitignored)
```

## Run

```bash
make -C bench/corpora/legal_de data           # only generate
make -C bench/corpora/legal_de all            # generate + run anonde + report
open bench/corpora/legal_de/REPORT.md
```

Default config: 30 docs per doctype = 150 docs total, deterministic
(`SEED=20260513`). Override:

```bash
make -C bench/corpora/legal_de all PER_DOCTYPE=100  # 500 docs
make -C bench/corpora/legal_de all SEED=42          # different draw
```

## PII coverage

Each generated doc carries 6–14 gold PHI spans. The generator emits
labels using the same GraSCCo-style names that `synth_clinical` does so
the existing `bench/scoring/label_map.yaml` works without
modification. Mapping to canonical types:

| Gold label (in corpus) | Canonical type (label_map.yaml) | Used for |
|---|---|---|
| `NAME_PATIENT` | PERSON | plaintiffs, defendants, clients, grantors, parties |
| `NAME_DOCTOR` | PERSON | attorneys, judges, notaries (titled professionals) |
| `NAME_RELATIVE` | PERSON | witnesses, third parties |
| `LOCATION_HOSPITAL` | ORGANIZATION | courts, law firms (institutional names) |
| `LOCATION_ORGANIZATION` | ORGANIZATION | counterparty companies (GmbH, AG, …) |
| `LOCATION_CITY` | LOCATION | court seat, party residence |
| `LOCATION_STREET` | ADDRESS | street + number ("Hauptstraße 8") |
| `LOCATION_ZIP` | ADDRESS | 5-digit Postleitzahl |
| `DATE` | DATE | case dates, judgment dates, deadlines |
| `DATE_BIRTH` | DATE | DOB |
| `CONTACT_PHONE` | PHONE | attorney + client telephone |
| `CONTACT_EMAIL` | EMAIL | attorney + client email |
| `ID` | ID | Aktenzeichen, Personalausweis-Nr., Steuer-ID, RA-Nummer |
| `PROFESSION` | PROFESSION | party occupation (Vollmacht, Anwaltsschreiben) |
| `AGE` | AGE | DOB-derived age (Klageschrift, Vollmacht) |
| `IBAN_CODE` | (OTHER) | settlement transfers + Anderkonto fees |

The Streitwert / Vergleichsbetrag / Forderung amount is **not** a PHI
category — it appears in text but no gold span is emitted, matching the
spec.

### Notes on label re-use

- `NAME_DOCTOR` is re-used for attorneys, judges and notaries. The
  semantic shape — *titled professional* — is the closest analogue in
  the existing `gold:` section to legal roles. Inventing
  `NAME_ATTORNEY` would have required editing `label_map.yaml`, which
  the spec forbids.
- `LOCATION_HOSPITAL` is re-used for courts and law firms (institutional
  entities). The canonical mapping is `ORGANIZATION`, which is exactly
  right; only the gold key name is repurposed.
- `IBAN_CODE` has no entry in `label_map.yaml::gold`, so it scores as
  `OTHER` in the gold-side normalisation. This is an acceptable
  trade-off given the "don't extend label_map" rule and IBAN's
  relatively low frequency in this corpus (Vergleich, Anwaltsschreiben).

## Aktenzeichen format

The corpus uses the canonical Bundeskonvention form
`<digits> <Roman/Letter> <digits>/<2-digit year>`, e.g. `4 O 217/23`:

- digits before the suffix: chamber / Senat number
- letter: department code (O = Zivilkammer, C = Familiensachen,
  F = Familienrecht, Ca = Arbeitsgericht-Klage, …) — the generator
  draws from a small pool that includes plain Roman numerals plus the
  common single/two-letter suffixes
- digits after the suffix: running case number within the chamber/year
- 2-digit year: 18..26

All values are random — no real Aktenzeichen is reproduced.

## Validation gates the generator enforces

1. Every span surface is non-empty (`assert text[start:end]`).
2. `end > start` for every span.
3. Spans are appended in document order so they're naturally non-decreasing.
4. JSON-encodable line-per-doc (UTF-8, `ensure_ascii=False`).

Spot-check after a run:

```bash
python3 -c "import json; [json.loads(l) for l in open('bench/corpora/legal_de/data/corpus.jsonl')]"
wc -l bench/corpora/legal_de/data/corpus.jsonl
```

## What this bench proves (and doesn't)

Proves: anonde catches PII when re-styled into five German legal
        sublanguages (claim, order, settlement, POA, demand letter).

Does NOT prove: anonde catches **real-world** German legal PII. Real
                legal text uses richer formulaic phrasing, more
                heterogenous court / chamber name patterns, and richer
                ID conventions than this generator covers. Treat this
                as a **generalisation probe**, not a production-grade
                benchmark.
