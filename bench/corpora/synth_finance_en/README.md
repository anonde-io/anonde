# bench/corpora/synth_finance_* — synthetic financial bench, 5 languages

Phase 3 of the multilingual bench expansion. Five corpora —
`synth_finance_{en,de,es,fr,it}` — of synthetic financial documents,
one per language. Financial documents are locale-specific (IBAN country,
postcode shape, date format, bank names), so each language gets its own
corpus rather than a single mixed one.

This directory (`synth_finance_en`) is the **canonical home of the
shared generator**: `generate.py` + `generators.py` + `templates.py`.
The four sibling corpora (`synth_finance_{de,es,fr,it}`) are thin
Makefile wrappers that invoke this same generator with a different
`--language`. There is no per-language copy of the generator — this
mirrors the `ai4privacy_*` / `mapa_*` shared-loader pattern.

Four document types per language:

| Key | Doc type | Characteristic |
|---|---|---|
| `invoice` | Invoice | bill-to person + address, payment IBAN |
| `statement` | Bank statement | account holder + IBAN, N transaction rows |
| `kyc` | KYC / onboarding record | DOB, employer, profession, IBAN, card |
| `confirmation` | Transaction confirmation | payer + beneficiary, card, references |

## Why synthetic and not real

Real financial documents with gold-labelled PII are not publicly
available — they contain live customer data. Slot-based synthesis is the
only way to ship a meaningfully sized, gold-labelled financial corpus
in-tree. `finance_de` already does this for German; Phase 3 generalises
to five languages and switches to canonical gold types (see below).

## Gold types — canonical, emitted directly

Unlike `synth_clinical` and `finance_de` (which reuse the GraSCCo
`NAME_PATIENT` / `LOCATION_HOSPITAL` vocabulary), this generator emits
the **canonical `label_map.yaml` gold types directly**. Every type below
is already a pass-through entry in the `gold:` section of
`bench/scoring/label_map.yaml`, so **no label-map changes are needed**.

| Slot concept | Gold type | Checksum |
|---|---|---|
| Account holder, applicant, beneficiary, contact | `PERSON` | — |
| Bank / employer / company | `ORGANIZATION` | — |
| Street + house number + postcode + city (one span) | `ADDRESS` | — |
| IBAN | `IBAN` | **MOD-97 valid** |
| Credit-card number | `ID` | **Luhn valid** |
| Account / customer / reference ID | `ID` | — |
| Statement / invoice / DOB / etc. dates | `DATE` | — |
| Email | `EMAIL` | — |
| Phone | `PHONE` | — |
| Profession | `PROFESSION` | — |

Because IBAN is gold-typed canonical `IBAN` (not folded into `ID` as
`finance_de` had to), this corpus gives a **genuine IBAN strict/partial
F1** — anonde's `IBANRecognizer` emits `IBAN_CODE` → canonical `IBAN`,
which matches.

### Checksum validity — the recognizers are genuinely exercised

- **IBANs are MOD-97 valid.** Generated with the same algorithm
  `analyzer/recognizers/iban.go::validateIBAN` uses to verify them, so
  the validator path is exercised end-to-end. The country code matches
  the corpus locale (DE→DE IBAN, ES→ES, FR→FR, IT→IT); the EN corpus
  issues Irish (`IE`) IBANs — IE is the natural euro-area,
  English-language choice, and its 4-letter bank code keeps the MOD-97
  letter-conversion path covered.
- **Credit-card numbers are Luhn valid**, issued under realistic
  Visa / Mastercard / Amex IIN prefixes, so anonde's `CREDIT_CARD`
  recognizer's Luhn check passes rather than rejecting them as garbage.

### Not gold-tagged

- **Monetary amounts** (`{AMOUNT}`, `{AMOUNT_NEG}`) appear in the text
  for realism but are NOT gold spans — anonde's no-monetary-PII rule.
- **Payment references** (`{REF}`) and **invoice numbers** (`{INVNO}`)
  are non-PII document metadata, not person/account identifiers, so they
  are deliberately left out of the gold set.

## Run

```bash
make -C bench/corpora/synth_finance_en all   # English
make -C bench/corpora/synth_finance_de all   # German
make -C bench/corpora/synth_finance_es all   # Spanish
make -C bench/corpora/synth_finance_fr all   # French
make -C bench/corpora/synth_finance_it all   # Italian
```

Default config: 30 docs per doc type = 120 docs per language,
deterministic (`SEED=20260512`, folded with the language so the five
corpora don't share an identical RNG stream). Override:

```bash
make -C bench/corpora/synth_finance_en all PER_DOCTYPE=100  # 400 docs
make -C bench/corpora/synth_finance_en all SEED=42
```

## Read the result

`REPORT.md` is the same shape as `bench/corpora/finance_de/REPORT.md`:

- **Strict / Partial / Type-only F1** per canonical entity.
- **Anonymisation leak rate** — share of gold spans no prediction
  overlaps. Lower is better.

## What this bench proves (and doesn't)

✅ Proves: anonde catches finance-flavoured PII (IBAN, card, account ID,
   person, address, org, date, email, phone) across five languages when
   it appears in invoice / statement / KYC / confirmation prose.

❌ Does NOT prove: anonde catches **real-world** financial PII. The
   generator is template-bounded — OCR'd PDFs, mixed-language
   correspondence, and exotic local identifier formats are out of scope.
