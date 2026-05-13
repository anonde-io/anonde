# bench/corpora/finance_de — synthetic German financial bench

A non-clinical counterpart to `bench/corpora/synth_clinical`. Same
slot-based generator pattern, same gold-by-construction guarantees, but
the sublanguage is **German retail finance** — account statements,
transfer orders, loan applications, securities portfolios, and KYC
questionnaires.

Purpose: answer the question "does anonde generalise beyond clinical
text?". GraSCCo and synth_clinical already establish recall on
discharge-letter-style PHI; this bench probes whether the same
recognizers behave sensibly on a sublanguage with different vocabulary
(banks instead of clinics), different entity mix (IBAN/BIC/ISIN heavy,
near-zero medical jargon), and different document structure (tabular
transaction rows rather than free prose).

Five document types, all in German:

| Key | Doctype | Characteristic |
|---|---|---|
| `kontoauszug` | Kontoauszug | account statement with transaction rows |
| `ueberweisung` | Überweisungsauftrag | SEPA transfer order, IBAN/BIC dense |
| `kreditantrag` | Kreditantrag | loan application with DOB + Steuer-ID + employer |
| `depot_auszug` | Depot-Auszug | securities portfolio with ISIN holdings rows |
| `kyc_anfrage` | KYC-Anfrage | KYC questionnaire with profession + employer + source-of-funds |

## Why synthetic and not real

Real German banking documents with gold-labelled PHI are not publicly
available — they would contain live customer data. Slot-based synthesis
is the only way to get a meaningfully sized, gold-labelled financial
corpus we can ship in-tree.

## Run

```bash
make -C bench/corpora/finance_de all
open bench/corpora/finance_de/REPORT.md
```

Default config: 30 docs per doctype = 150 docs total, deterministic
(`SEED=20260512`). Override:

```bash
make -C bench/corpora/finance_de all PER_DOCTYPE=100  # 500 docs
make -C bench/corpora/finance_de all SEED=42          # different generation
```

The corpus alone:

```bash
make -C bench/corpora/finance_de data
```

## PII coverage

Per-doc PHI density varies from ~5 spans (a short transfer order) to
~25+ spans (a Kontoauszug with 8 transaction rows × 3 PHI per row + a
fully-populated header).

Gold entity types use the **GraSCCo / `gold:` vocabulary** from
`bench/scoring/label_map.yaml` (NAME_PATIENT, CONTACT_PHONE, etc.) — the
same vocabulary `synth_clinical` uses — so `compare.py` works unchanged
without any modification to `label_map.yaml`.

Mapping from financial concept to gold-section type:

| Concept | Gold type | Canonical (via label_map) |
|---|---|---|
| Account holder, applicant | `NAME_PATIENT` | PERSON |
| Beneficiary, beneficiary, contact, employee | `NAME_RELATIVE` | PERSON |
| Bank / broker / employer / company | `LOCATION_HOSPITAL` | ORGANIZATION |
| City | `LOCATION_CITY` | LOCATION |
| Street address | `LOCATION_STREET` | ADDRESS |
| Postal code | `LOCATION_ZIP` | ADDRESS |
| Statement / transfer / DOB / etc. | `DATE` / `DATE_BIRTH` | DATE |
| Age | `AGE` | AGE |
| Phone | `CONTACT_PHONE` | PHONE |
| Email | `CONTACT_EMAIL` | EMAIL |
| Profession | `PROFESSION` | PROFESSION |
| IBAN, BIC, ISIN, WKN, Steuer-ID, customer-no. | `ID` | ID |

### Trade-off: IBAN gets typed as ID, not IBAN

`anonde`'s `IBANRecognizer` emits `IBAN_CODE`, which maps to canonical
`IBAN`. The gold section of `label_map.yaml` has **no key** that maps to
canonical `IBAN`, so gold spans cannot be flagged as canonical `IBAN`
without modifying `label_map.yaml` (out of scope for this corpus).

Workaround: every numeric financial identifier in this corpus —
IBAN, BIC, ISIN, WKN, customer number, Steuer-ID — is gold-typed `ID`.
This means:

- **Strict / partial F1 for IBAN-detection** will report 0 in the report,
  because anonde canonical `IBAN` and gold canonical `ID` are different
  types. Treat these numbers as expected — not as a bug.
- **Type-only F1 and leak rate** remain meaningful: type-only counts an
  overlap regardless of canonical type, and leak rate is type-agnostic.
  These two views are the right way to read IBAN/BIC/ISIN scores for
  this corpus.

The README of the `synth_clinical` bench has a similar caveat — the
exact set of in-scope types is whatever the `gold:` section already
declares.

### Decisions worth knowing

- **IBANs are MOD-97-valid**. Generated with the exact same algorithm
  `analyzer/recognizers/iban.go::validateIBAN` uses to verify them, so
  the validator path is exercised end-to-end. Real BLZs are used (14
  major German banks) so paired IBAN + BIC fields refer to the same
  institution within a single document.
- **Steuer-IDs satisfy the full BMF spec**: ISO 7064 MOD 11,10 check
  digit *and* the uniqueness rule (one digit repeats 2-3 times in the
  first 10 positions, all others 0-1 times). Random 11-digit numbers
  fail the uniqueness rule ~70% of the time and would be silently
  rejected by anonde's recognizer.
- **Amounts are not gold-tagged**. PII detectors typically ignore
  monetary values, and tagging them would inflate leak rates
  artificially. Amounts use realistic German formatting
  ("1.234,56 EUR") to keep the documents plausible.
- **No clinical leakage**. Person names appear without "Patient"
  prefixes (financial salutations are "Herr Müller" / "Frau Schmidt",
  not "Patient Herr Müller"). Doctor honorifics are absent.

## Read the result

`REPORT.md` is the same shape as `bench/corpora/synth_clinical/REPORT.md`:

- **Strict / Partial / Type-only F1** per canonical entity.
- **Anonymisation leak rate** — share of gold spans no prediction
  overlaps. Lower is better.

Compare this number with synth_clinical's leak rate to answer the
generalisation question: a similar leak rate suggests the
detectors transfer well; a much higher leak rate flags
sublanguage-specific gaps in the recognizers.

## What this bench proves (and doesn't)

✅ Proves: anonde catches the PHI types listed above when they appear in
   German financial prose with finance-flavoured surrounding context
   (bank names, transfer references, ISINs).

❌ Does NOT prove: anonde catches **real-world** German financial PHI —
   the generator is bounded to its template variety. Edge cases in real
   docs (e.g. OCR'd PDFs, mixed-language correspondence, handwritten
   amendments) are not covered.

❌ Does NOT prove anything about non-German financial documents —
   English/EU-multilang versions are out of scope.
