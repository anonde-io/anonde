#!/usr/bin/env python3
"""Convert the GraSCCo_PHI JSON export to our corpus.jsonl schema.

GraSCCo_PHI ships as INCEpTION UIMA-CAS JSON inside
grascco_phi_annotation_json.zip on Zenodo. Each document JSON has:

  - one feature structure of type uima.cas.Sofa carrying sofaString
    (the German clinical text)
  - many feature structures of type webanno.custom.PHI with `begin`,
    `end`, and a `kind` feature naming the PHI sub-type
    (NAME_PATIENT, DATE, LOCATION_STREET, ID, …)

Output (one JSON object per line):

    {"id": "<file-stem>", "text": "<sofaString>",
     "entities": [{"start": N, "end": M, "type": "<kind>"}]}

Offsets are codepoint indices (NFC normalised) — same convention the Go
runner converts byte offsets back to before emitting predictions.
"""

from __future__ import annotations

import argparse
import json
import sys
import unicodedata
import zipfile
from pathlib import Path
from typing import Iterable

K_FS = "%FEATURE_STRUCTURES"
K_TYPE = "%TYPE"
SOFA_TYPE = "uima.cas.Sofa"

# Feature names where INCEpTION/GeMTeX stores the PHI sub-type. `kind`
# is the GraSCCo_PHI convention; the others are defensive fallbacks for
# other GeMTeX exports.
LABEL_FEATURE_CANDIDATES = ("kind", "label", "value", "identifier", "PHI_Type")

DEFAULT_DROP_TYPES = {
    "uima.tcas.DocumentAnnotation",
    "de.tudarmstadt.ukp.dkpro.core.api.metadata.type.DocumentMetaData",
    "de.tudarmstadt.ukp.dkpro.core.api.segmentation.type.Sentence",
    "de.tudarmstadt.ukp.dkpro.core.api.segmentation.type.Token",
    "de.tudarmstadt.ukp.clarin.webanno.api.type.FeatureDefinition",
    "de.tudarmstadt.ukp.clarin.webanno.api.type.LayerDefinition",
    "de.tudarmstadt.ukp.dkpro.core.api.metadata.type.TagsetDescription",
}


def _iter_cas(input_path: Path) -> Iterable[tuple[str, dict]]:
    if input_path.is_dir():
        for p in sorted(input_path.rglob("*.json")):
            with p.open("r", encoding="utf-8") as f:
                yield p.stem, json.load(f)
        return
    if input_path.suffix == ".zip":
        with zipfile.ZipFile(input_path) as zf:
            for name in sorted(zf.namelist()):
                if not name.endswith(".json") or name.endswith("/"):
                    continue
                with zf.open(name) as fh:
                    yield Path(name).stem, json.load(fh)
        return
    raise SystemExit(f"expected zip or directory, got: {input_path}")


def _sofa(cas: dict) -> str | None:
    for fs in cas.get(K_FS, []):
        if fs.get(K_TYPE) == SOFA_TYPE:
            return fs.get("sofaString")
    return None


def _label(fs: dict) -> str:
    for k in LABEL_FEATURE_CANDIDATES:
        v = fs.get(k)
        if isinstance(v, str) and v:
            return v
    ttype = fs.get(K_TYPE, "")
    return ttype.rsplit(".", 1)[-1] or "UNKNOWN"


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--in", dest="in_path", required=True,
                    help="grascco_phi_annotation_json.zip OR unzipped directory")
    ap.add_argument("--out", dest="out_path", required=True)
    ap.add_argument("--normalize", choices=("none", "NFC"), default="NFC")
    args = ap.parse_args()

    in_path = Path(args.in_path)
    out_path = Path(args.out_path)
    out_path.parent.mkdir(parents=True, exist_ok=True)

    docs, spans_total = 0, 0
    with out_path.open("w", encoding="utf-8") as fout:
        for doc_id, cas in _iter_cas(in_path):
            text = _sofa(cas)
            if not text:
                print(f"skip {doc_id}: no Sofa", file=sys.stderr)
                continue
            if args.normalize == "NFC":
                text = unicodedata.normalize("NFC", text)
            spans = []
            for fs in cas.get(K_FS, []):
                ttype = fs.get(K_TYPE, "")
                if ttype in DEFAULT_DROP_TYPES or ttype == SOFA_TYPE:
                    continue
                begin, end = fs.get("begin"), fs.get("end")
                if not (isinstance(begin, int) and isinstance(end, int)):
                    continue
                if not (0 <= begin < end <= len(text)):
                    continue
                spans.append({"start": begin, "end": end, "type": _label(fs)})
            fout.write(json.dumps(
                {"id": doc_id, "text": text, "entities": spans},
                ensure_ascii=False,
            ) + "\n")
            docs += 1
            spans_total += len(spans)
    print(f"wrote {out_path}: {docs} docs, {spans_total} gold spans",
          file=sys.stderr)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
