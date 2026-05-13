#!/usr/bin/env python3
"""runner_gliner_pii — emit anonde-shaped findings.jsonl from GLiNER PII.

GLiNER is an open-set NER architecture that takes the label set at inference
time. It can't be loaded through hugot's TokenClassification pipeline (uses
a non-standard `words_mask` input), so we score it via this Python sidecar
and feed the output into the same compare.py the Go bench uses.

Output schema (per line):

    {"id": "...", "engine": "gliner-pii", "findings": [
        {"start": int, "end": int, "type": "PERSON|LOCATION|...", "score": float}
    ], "duration_ms": float}

Offsets are CODEPOINT indices (Python convention) — same as the Go runner
emits after converting byte offsets, so compare.py works unchanged.

Labels: GLiNER takes them at inference; we use a curated PII set that maps
cleanly to anonde's canonical types. Adding labels here changes recall;
keep them narrow to avoid noise.

Usage:

    .venv-bench/bin/python bench/runners/gliner.py \\
        --in bench/corpora/openmed/data/corpus.jsonl \\
        --out bench/corpora/openmed/data/anonde_glinerpii.jsonl
"""

from __future__ import annotations

import argparse
import json
import sys
import time
import unicodedata
from pathlib import Path

# GLiNER label → anonde canonical entity type. Same shape as
# analyzer/recognizers/ner_hugot.go::hugotLabelToEntity but for the
# label vocabulary GLiNER PII responds well to. Tuning is per-label
# wording — GLiNER's zero-shot scoring is sensitive to label phrasing.
LABEL_TO_CANONICAL: dict[str, str] = {
    "person":            "PERSON",
    "first name":        "PERSON",
    "last name":         "PERSON",
    "full name":         "PERSON",
    "patient name":      "PERSON",
    "doctor name":       "PERSON",
    "organization":      "ORGANIZATION",
    "company":           "ORGANIZATION",
    "hospital":          "ORGANIZATION",
    "city":              "LOCATION",
    "country":           "LOCATION",
    "state":             "LOCATION",
    "address":           "ADDRESS",
    "street address":    "STREET_ADDRESS",
    "street":            "STREET_ADDRESS",
    "building number":   "STREET_ADDRESS",
    "postal code":       "POSTAL_CODE",
    "zip code":          "POSTAL_CODE",
    "date":              "DATE_TIME",
    "date of birth":     "DATE_TIME",
    "phone number":      "PHONE_NUMBER",
    "email":             "EMAIL_ADDRESS",
    "email address":     "EMAIL_ADDRESS",
    "url":               "URL",
    "credit card":       "CREDIT_CARD",
    "credit card number": "CREDIT_CARD",
    "iban":              "IBAN_CODE",
    "ssn":               "US_SSN",
    "passport":          "ID",
    "social security number": "US_SSN",
    "id number":         "ID",
    "age":               "AGE",
    "profession":        "PROFESSION",
    "job title":         "PROFESSION",
}

# Inference-time labels (the keys above, ordered for consistency).
GLINER_LABELS = list(LABEL_TO_CANONICAL.keys())


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--in", dest="inp", required=True,
                    help="input corpus jsonl (id, text)")
    ap.add_argument("--out", required=True,
                    help="output findings jsonl")
    ap.add_argument("--model", default="knowledgator/gliner-pii-base-v1.0",
                    help="GLiNER PII model id")
    ap.add_argument("--threshold", type=float, default=0.40,
                    help="GLiNER prediction threshold (0..1)")
    ap.add_argument("--engine-label", default="gliner-py",
                    help="label written to each output line (engine-name prefix "
                         "drives label_map section routing in compare.py)")
    ap.add_argument("--flat-ner", action="store_true", default=True,
                    help="prefer flat (non-overlapping) entity layout")
    args = ap.parse_args()

    # Import lazily so --help works without GLiNER installed. The import
    # cost is dominated by torch — ~1–2 s on first use.
    try:
        from gliner import GLiNER  # type: ignore
    except ImportError as e:
        print(f"gliner not installed: {e}\n"
              f"Install: .venv-bench/bin/pip install gliner",
              file=sys.stderr)
        return 2

    print(f"loading {args.model} (this can take ~30s on first run)…", file=sys.stderr)
    t0 = time.perf_counter()
    model = GLiNER.from_pretrained(args.model)
    # CPU is fine for this scale; explicit so future GPU machines don't surprise us.
    try:
        model = model.eval()  # not strictly required, but matches PyTorch idiom
    except Exception:
        pass
    print(f"model ready in {(time.perf_counter()-t0)*1000:.0f} ms",
          file=sys.stderr)

    inp = Path(args.inp)
    out = Path(args.out)
    out.parent.mkdir(parents=True, exist_ok=True)

    n_docs = 0
    n_findings = 0
    with inp.open("r", encoding="utf-8") as fin, out.open("w", encoding="utf-8") as fout:
        for line in fin:
            line = line.strip()
            if not line:
                continue
            try:
                doc = json.loads(line)
            except json.JSONDecodeError as e:
                print(f"skip malformed line: {e}", file=sys.stderr)
                continue
            doc_id = doc.get("id", "")
            text = doc.get("text", "")
            if not text:
                continue

            # Codepoint-offset bookkeeping. GLiNER returns character
            # offsets that ARE codepoints in modern transformers tokenizers,
            # but we re-derive via str slicing on the NFC-normalised text to
            # match how loader_grascco.py wrote the gold spans.
            text_nfc = unicodedata.normalize("NFC", text)

            t1 = time.perf_counter()
            try:
                entities = model.predict_entities(
                    text_nfc, GLINER_LABELS, threshold=args.threshold,
                    flat_ner=args.flat_ner,
                )
            except Exception as e:
                print(f"predict failed id={doc_id}: {e}", file=sys.stderr)
                entities = []
            dur_ms = (time.perf_counter() - t1) * 1000.0

            findings = []
            for ent in entities:
                # GLiNER returns: {start, end, label, score, text}
                label = ent.get("label", "")
                canonical = LABEL_TO_CANONICAL.get(label)
                if not canonical:
                    continue
                start = int(ent.get("start", 0))
                end = int(ent.get("end", 0))
                if end <= start:
                    continue
                findings.append({
                    "start": start,
                    "end": end,
                    "type": canonical,
                    "score": float(ent.get("score", 0.0)),
                })

            fout.write(json.dumps({
                "id": doc_id,
                "engine": args.engine_label,
                "findings": findings,
                "duration_ms": dur_ms,
            }, ensure_ascii=False) + "\n")
            n_docs += 1
            n_findings += len(findings)
            print(f"doc={n_docs} id={doc_id} spans={len(findings)} dur={dur_ms:.0f}ms",
                  file=sys.stderr)

    print(f"processed {n_docs} docs, {n_findings} findings -> {out}", file=sys.stderr)
    return 0


if __name__ == "__main__":
    sys.exit(main())
