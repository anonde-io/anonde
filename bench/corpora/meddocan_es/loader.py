#!/usr/bin/env python3
"""loader — convert the MEDDOCAN corpus to our corpus.jsonl schema.

MEDDOCAN (Medical Document Anonymization, IberLEF 2019 shared task) is a
PHI-annotated Spanish clinical de-identification corpus: 1,000 clinical
case reports derived from the Spanish Clinical Case Corpus (SPACCC),
manually annotated with 22 PHI entity types. It is the real Spanish
clinical de-id gold — note that pharmaconer_es is a chemical/drug
corpus, NOT a PHI-recall corpus.

DATA SOURCE — public, unauthenticated. Unlike ggponc_de / conll2003_de,
MEDDOCAN is openly distributed (CC-BY-4.0) as a Zenodo deposit:

    https://zenodo.org/records/4279323/files/meddocan.zip

The Makefile curls that archive into data/raw/. This loader accepts
either the .zip itself or an already-extracted directory tree.

FORWARD-COMPATIBLE GATING. If the raw archive is absent (Zenodo
unreachable, offline CI, or a future move behind registration) the
loader exits cleanly with code 2 and a clear message — the same
"corpus unavailable, skip" contract the bench harness already honours
for ggponc_de / conll2003_de. The bench harness treats exit-2 as skip
rather than a hard failure.

MEDDOCAN ships in BRAT standoff format — paired `<doc>.txt` /
`<doc>.ann` files under train/ dev/ test/ `brat/` subdirectories. We
load the **test** split (250 docs) to match the ~250-300-doc sample
size of the other bench corpora; the test split is MEDDOCAN's canonical
evaluation split. Override with --split.

BRAT `.ann` text-bound annotations look like:

    T1<TAB>FECHAS 215 225<TAB>03/03/1946

The offsets are CODEPOINT indices (verified against the corpus — they
index text[start:end] directly, not byte offsets), which is exactly the
schema bench/corpora/*/data/corpus.jsonl uses. Discontinuous spans
(`5 10;15 20`) do not occur in MEDDOCAN; if one ever appears it is
skipped with a warning.

LABEL MAPPING — this loader pre-maps MEDDOCAN's 22 PHI labels to the
canonical anonde vocabulary BEFORE writing gold (mirrors the mapa_*
loader's COARSE_MAP approach). The full mapping is mirrored in
bench/scoring/label_map.yaml's gold: section as an audit trail. Labels
with no clean canonical PHI type (SEXO_SUJETO_ASISTENCIA — gender;
OTROS_SUJETO_ASISTENCIA — catch-all "other") are dropped, so a correct
non-flag of them is not counted as a false negative.

Output (one JSON object per line):

    {"id": "meddocan-<doc-id>", "text": "...", "entities": [
      {"start": int, "end": int, "type": "<canonical-type>"}
    ]}
"""

from __future__ import annotations

import argparse
import collections
import json
import sys
import unicodedata
import zipfile
from pathlib import Path

# MEDDOCAN PHI label -> canonical anonde type. The canonical vocabulary
# is PERSON / LOCATION / ADDRESS / ORGANIZATION / DATE / AGE / ID /
# PHONE / EMAIL / PROFESSION (see bench/scoring/label_map.yaml
# `canonical:`). A value of None drops the span from gold.
#
# This is the single source of truth for the mapping; the label_map.yaml
# block under `# MEDDOCAN PHI gold` documents the same decisions.
LABEL_MAP: dict[str, str | None] = {
    # --- People -----------------------------------------------------
    "NOMBRE_SUJETO_ASISTENCIA": "PERSON",     # patient name
    "NOMBRE_PERSONAL_SANITARIO": "PERSON",    # clinician name
    "FAMILIARES_SUJETO_ASISTENCIA": "PERSON",  # named relative
    # --- Address / location ----------------------------------------
    "CALLE": "ADDRESS",                       # street address
    "TERRITORIO": "LOCATION",                 # city / region / postcode
    "PAIS": "LOCATION",                       # country
    # --- Organisations ---------------------------------------------
    "HOSPITAL": "ORGANIZATION",
    "INSTITUCION": "ORGANIZATION",
    "CENTRO_SALUD": "ORGANIZATION",           # health centre
    # --- Dates / age -----------------------------------------------
    "FECHAS": "DATE",
    "EDAD_SUJETO_ASISTENCIA": "AGE",
    # --- Identifiers (all severity-10 in label_map.yaml) -----------
    "ID_SUJETO_ASISTENCIA": "ID",             # patient record number
    "ID_TITULACION_PERSONAL_SANITARIO": "ID",  # clinician licence id
    "ID_ASEGURAMIENTO": "ID",                 # insurance id
    "ID_CONTACTO_ASISTENCIAL": "ID",          # care-contact id
    "ID_EMPLEO_PERSONAL_SANITARIO": "ID",     # clinician employment id
    # --- Contact ---------------------------------------------------
    "NUMERO_TELEFONO": "PHONE",
    "NUMERO_FAX": "PHONE",
    "CORREO_ELECTRONICO": "EMAIL",
    # --- Profession ------------------------------------------------
    "PROFESION": "PROFESSION",
    # --- Dropped: no clean canonical PHI type ----------------------
    # SEXO_SUJETO_ASISTENCIA is gender ("H"/"M"/"varón"/…) — an NRP-class
    # quasi-identifier with no canonical anonde type; dropping it keeps a
    # correct non-flag from counting as a false negative (same treatment
    # as MAPA's MAPA_NATIONALITY / MAPA_MARITAL_STATUS).
    "SEXO_SUJETO_ASISTENCIA": None,
    # OTROS_SUJETO_ASISTENCIA is MEDDOCAN's catch-all "other PHI" bucket
    # — heterogeneous, no single canonical type; dropped.
    "OTROS_SUJETO_ASISTENCIA": None,
}


def _find_brat_root(root: Path, split: str) -> Path | None:
    """Locate the BRAT directory for `split` inside an extracted tree.

    MEDDOCAN's zip extracts to meddocan/<split>/brat/. We also tolerate
    the split dir or the brat dir being passed directly."""
    candidates = [
        root / "meddocan" / split / "brat",
        root / split / "brat",
        root / "brat",
        root,
    ]
    for c in candidates:
        if c.is_dir() and any(c.glob("*.ann")):
            return c
    return None


def _parse_ann(ann_path: Path) -> list[tuple[str, int, int]]:
    """Parse a BRAT .ann file → list of (meddocan_label, start, end).

    Only text-bound annotations (lines starting with `T`) are kept.
    Discontinuous spans (offset field containing `;`) are skipped."""
    out: list[tuple[str, int, int]] = []
    for raw in ann_path.read_text(encoding="utf-8").splitlines():
        if not raw or not raw.startswith("T"):
            continue
        # T<id><TAB><LABEL> <start> <end><TAB><surface>
        try:
            _tid, mid, _surface = raw.split("\t", 2)
        except ValueError:
            continue
        parts = mid.split(" ")
        if len(parts) < 3:
            continue
        label = parts[0]
        offsets = " ".join(parts[1:])
        if ";" in offsets:
            # Discontinuous span — MEDDOCAN has none, but skip defensively.
            print(f"  skip discontinuous span in {ann_path.name}: {mid}",
                  file=sys.stderr)
            continue
        try:
            start, end = int(parts[1]), int(parts[2])
        except ValueError:
            continue
        out.append((label, start, end))
    return out


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--in", dest="in_path", required=True,
                    help="MEDDOCAN .zip or extracted corpus directory")
    ap.add_argument("--out", dest="out_path", required=True)
    ap.add_argument("--split", default="test", choices=("train", "dev", "test"),
                    help="MEDDOCAN split to load (default: test, 250 docs)")
    args = ap.parse_args()

    in_path = Path(args.in_path)
    if not in_path.exists():
        print(
            f"meddocan_es: raw corpus not found at {in_path}.\n"
            "MEDDOCAN is openly distributed (CC-BY-4.0) on Zenodo:\n"
            "  https://zenodo.org/records/4279323/files/meddocan.zip\n"
            "`make -C bench/corpora/meddocan_es data` curls it automatically.\n"
            "If Zenodo is unreachable, download meddocan.zip manually and\n"
            "drop it at bench/corpora/meddocan_es/data/raw/meddocan.zip.\n"
            "Skipping this corpus (exit 2 = corpus unavailable).",
            file=sys.stderr,
        )
        return 2

    # Resolve the BRAT directory: accept a .zip (extract to a sibling
    # dir) or an already-extracted tree.
    if zipfile.is_zipfile(in_path):
        extract_root = in_path.parent / "_extracted"
        if not extract_root.exists():
            with zipfile.ZipFile(in_path) as zf:
                zf.extractall(extract_root)
        search_root = extract_root
    else:
        search_root = in_path

    brat_dir = _find_brat_root(search_root, args.split)
    if brat_dir is None:
        print(
            f"meddocan_es: no BRAT .ann files found for split "
            f"'{args.split}' under {search_root}.\n"
            "Expected the standard MEDDOCAN layout "
            "meddocan/<split>/brat/*.{txt,ann}.\n"
            "Skipping this corpus (exit 2 = corpus unavailable).",
            file=sys.stderr,
        )
        return 2

    ann_files = sorted(brat_dir.glob("*.ann"))
    out_path = Path(args.out_path)
    out_path.parent.mkdir(parents=True, exist_ok=True)

    docs = 0
    n_spans = 0
    n_dropped = 0
    per_type: collections.Counter = collections.Counter()
    with out_path.open("w", encoding="utf-8") as fout:
        for ann_path in ann_files:
            txt_path = ann_path.with_suffix(".txt")
            if not txt_path.exists():
                continue
            # NFC-normalise to match the rest of bench/corpora/* and the
            # Go runner's offset handling. MEDDOCAN .ann offsets index
            # the raw file; NFC is a no-op for this corpus (verified) but
            # we normalise for schema consistency.
            text = unicodedata.normalize(
                "NFC", txt_path.read_text(encoding="utf-8"))

            entities: list[dict] = []
            for label, start, end in _parse_ann(ann_path):
                if end <= start or end > len(text):
                    continue
                canonical = LABEL_MAP.get(label)
                if canonical is None:
                    # Either an explicitly dropped label, or an unknown
                    # one. Unknown labels are noisy to silently ignore.
                    if label not in LABEL_MAP:
                        print(f"  unknown MEDDOCAN label {label!r} in "
                              f"{ann_path.name} — dropped", file=sys.stderr)
                    n_dropped += 1
                    continue
                entities.append({
                    "start": start, "end": end, "type": canonical,
                })
                per_type[canonical] += 1
                n_spans += 1

            # Stable, source-anchored doc id.
            doc_id = f"meddocan-{ann_path.stem}"
            fout.write(json.dumps(
                {"id": doc_id, "text": text, "entities": entities},
                ensure_ascii=False,
            ) + "\n")
            docs += 1

    type_summary = ", ".join(
        f"{k}={v}" for k, v in sorted(per_type.items(), key=lambda x: -x[1]))
    print(
        f"wrote {out_path}: {docs} docs (split={args.split}), "
        f"{n_spans} gold spans ({n_dropped} dropped — gender/other), "
        f"avg {n_spans / max(docs, 1):.1f} spans/doc; types: {type_summary}",
        file=sys.stderr,
    )
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
