# bench/corpora/meddocan_es — Spanish clinical de-identification bench (MEDDOCAN)

Phase 4 of the multilingual bench expansion. **MEDDOCAN** (Medical
Document Anonymization, [IberLEF 2019 shared
task](https://temu.bsc.es/meddocan/)) is the real Spanish clinical
de-identification gold: 1,000 clinical case reports derived from the
Spanish Clinical Case Corpus (SPACCC), **manually annotated** by domain
experts with 22 PHI entity types.

This gives the bench matrix a real-gold **Spanish clinical** recall
number — the Spanish counterpart of `openmed` (German, GraSCCo).

> Note: `pharmaconer_es` is a chemical / drug corpus, **not** a
> PHI-recall corpus. MEDDOCAN is the Spanish clinical de-id gold.

## Data — public, openly licensed

Unlike `ggponc_de` / `conll2003_de`, MEDDOCAN is **not gated**. It is
openly distributed under **CC-BY-4.0** as a Zenodo deposit:

- [`zenodo.org/records/4279323`](https://zenodo.org/records/4279323) —
  `meddocan.zip` (~11.7 MB).

`make data` curls that archive into `data/raw/` and runs the loader —
no registration, no DUA.

The loader is nonetheless **forward-compatible**: if the archive is
absent (Zenodo unreachable, an offline CI box, or a future move behind
registration), `loader.py` exits cleanly with **code 2** and a message
explaining how to obtain the data. The bench harness treats exit-2 as
"corpus unavailable, skip" — the same contract `conll2003_de` uses.

`meddocan_es` **is** in the auto-matrix `ES_CORPORA` list and the
`bench-full.yml` CI matrix — it is the real-gold Spanish clinical recall
corpus, so the headline needs it. The exit-2 contract is what makes that
safe: a transient Zenodo failure shows the cell as missing (the CI
matrix is `fail-fast: false` with a tolerant renderer) rather than
reddening the whole run. Run it explicitly:

```bash
make -C bench/corpora/meddocan_es all
```

## Format

MEDDOCAN ships in **BRAT standoff format** — paired `<doc>.txt` /
`<doc>.ann` files under `train/` (500 docs), `dev/` (250) and `test/`
(250) `brat/` subdirectories. The loader reads the **test** split by
default (250 docs — MEDDOCAN's canonical evaluation split, matching the
~250-300-doc sample size of the other bench corpora). Override with
`SPLIT=train` or `SPLIT=dev`.

`.ann` text-bound annotations carry **codepoint** offsets (verified —
they index `text[start:end]` directly), the same schema the rest of
`bench/corpora/*/data/corpus.jsonl` uses.

## Label mapping

MEDDOCAN's 22 PHI labels are pre-mapped to the canonical anonde
vocabulary by `loader.py` **before** gold is written (mirrors the
`mapa_*` loader's `COARSE_MAP`). The full mapping is also documented in
`bench/scoring/label_map.yaml` (`# MEDDOCAN PHI gold` block) as an audit
trail.

| MEDDOCAN label | Canonical | |
|---|---|---|
| `NOMBRE_SUJETO_ASISTENCIA` | PERSON | patient name |
| `NOMBRE_PERSONAL_SANITARIO` | PERSON | clinician name |
| `FAMILIARES_SUJETO_ASISTENCIA` | PERSON | named relative |
| `CALLE` | ADDRESS | street address |
| `TERRITORIO` | LOCATION | city / region / postcode |
| `PAIS` | LOCATION | country |
| `HOSPITAL` | ORGANIZATION | |
| `INSTITUCION` | ORGANIZATION | |
| `CENTRO_SALUD` | ORGANIZATION | health centre |
| `FECHAS` | DATE | |
| `EDAD_SUJETO_ASISTENCIA` | AGE | |
| `ID_SUJETO_ASISTENCIA` | ID | patient record number |
| `ID_TITULACION_PERSONAL_SANITARIO` | ID | clinician licence id |
| `ID_ASEGURAMIENTO` | ID | insurance id |
| `ID_CONTACTO_ASISTENCIAL` | ID | care-contact id |
| `ID_EMPLEO_PERSONAL_SANITARIO` | ID | clinician employment id |
| `NUMERO_TELEFONO` | PHONE | |
| `NUMERO_FAX` | PHONE | |
| `CORREO_ELECTRONICO` | EMAIL | |
| `PROFESION` | PROFESSION | |
| `SEXO_SUJETO_ASISTENCIA` | *(dropped)* | gender — NRP-class, no canonical type |
| `OTROS_SUJETO_ASISTENCIA` | *(dropped)* | catch-all "other PHI" — no single type |

The two dropped labels (gender, catch-all "other") have no clean
canonical PHI type — dropping them keeps a correct non-flag from being
counted as a false negative, the same treatment `mapa_*` gives
`MAPA_NATIONALITY` / `MAPA_MARITAL_STATUS`.

## Run

```bash
# fetch + load (curls meddocan.zip from Zenodo; exits 2 if unreachable)
make -C bench/corpora/meddocan_es data

# run anonde (patterns + GLiNER is the production stack)
make -C bench/corpora/meddocan_es anonde ANONDE_BACKEND=gliner

# run Presidio (needs the Spanish spaCy model)
python -m spacy download es_core_news_lg
make -C bench/corpora/meddocan_es presidio

# compare -> REPORT.md + results.csv
make -C bench/corpora/meddocan_es report
```

## Read the result

`REPORT.md` is the same shape as `bench/corpora/openmed/REPORT.md`:

- **Strict / Partial / Type-only F1** per canonical entity.
- **Anonymisation leak rate** — share of gold spans no prediction
  overlaps. Lower is better.

## Layout

```
bench/corpora/meddocan_es/
├── README.md           ← this file
├── Makefile            ← curls Zenodo zip, runs loader + runners
├── loader.py           ← BRAT standoff -> data/corpus.jsonl
└── data/               ← (gitignored) raw zip + corpus + findings
```

## Data provenance

- Source: MEDDOCAN corpus, IberLEF 2019 shared task. 1,000 SPACCC
  clinical case reports, expert-annotated with 22 PHI types.
- Distribution: Zenodo record
  [`4279323`](https://zenodo.org/records/4279323), CC-BY-4.0.
- Citation: Marimon et al. 2019, *Automatic De-identification of Medical
  Texts in Spanish: the MEDDOCAN Track, Corpus, Guidelines, Methods and
  Evaluation of Results* (IberLEF@SEPLN).

## What this bench proves (and doesn't)

✅ Proves: anonde's recall on **real, expert-annotated Spanish clinical
   PHI** — patient/clinician names, addresses, dates, ages, record IDs,
   insurance IDs, phone/fax, email, profession.

❌ Does NOT prove: anything about chemical/drug detection (that is
   `pharmaconer_es`), nor coverage of the two dropped non-canonical
   labels (gender, "other").
