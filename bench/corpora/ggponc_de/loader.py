#!/usr/bin/env python3
"""Convert GGPONC 2.0 distribution files to our corpus.jsonl schema.

GGPONC ships in a couple of layouts depending on the version:

  (a) `plain/<doc-id>.txt`     — one plain-text guideline per file
  (b) `json/<doc-id>.json`     — JSON with `text` and `annotations`
  (c) `<doc-id>.txt` at top    — older flat layout

This loader detects the layout and emits:

    {"id": "ggponc-<doc-id>", "text": "...", "entities": []}

with empty entities — GGPONC's own annotations are medical concepts,
not PHI, so we drop them. Findings on this corpus are evaluated as a
precision probe, not against gold spans.
"""

from __future__ import annotations

import argparse
import json
import sys
import unicodedata
from pathlib import Path


def _iter_plain(root: Path):
    for p in sorted(root.rglob("*.txt")):
        # Skip BRAT annotation files that share .txt extension
        if p.with_suffix(".ann").exists() and p.parent.name == "ann":
            continue
        yield p.stem, p.read_text(encoding="utf-8")


def _iter_json(root: Path):
    for p in sorted(root.rglob("*.json")):
        try:
            d = json.loads(p.read_text(encoding="utf-8"))
        except json.JSONDecodeError as exc:
            print(f"  skip {p}: {exc}", file=sys.stderr)
            continue
        text = d.get("text") or d.get("sofaString") or ""
        if not text:
            continue
        yield p.stem, text


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--in", dest="in_root", required=True,
                    help="path to extracted GGPONC corpus")
    ap.add_argument("--out", dest="out_path", required=True)
    ap.add_argument("--min-chars", type=int, default=500)
    args = ap.parse_args()

    root = Path(args.in_root)
    if not root.exists():
        print(
            f"ERROR: {root} does not exist.\n"
            "Register for GGPONC 2.0 at "
            "https://www.leitlinienprogramm-onkologie.de/projekte/ggponc-english\n"
            "then drop the extracted corpus under this path.",
            file=sys.stderr,
        )
        return 2

    plain_dir = root / "plain"
    json_dir = root / "json"
    if plain_dir.is_dir():
        iterator = _iter_plain(plain_dir)
        layout = "plain"
    elif json_dir.is_dir():
        iterator = _iter_json(json_dir)
        layout = "json"
    elif any(root.glob("*.txt")):
        iterator = _iter_plain(root)
        layout = "flat-text"
    elif any(root.glob("*.json")):
        iterator = _iter_json(root)
        layout = "flat-json"
    else:
        print(f"ERROR: no .txt or .json files under {root} or known subdirs",
              file=sys.stderr)
        return 2

    out_path = Path(args.out_path)
    out_path.parent.mkdir(parents=True, exist_ok=True)

    docs = 0
    n_chars = 0
    with out_path.open("w", encoding="utf-8") as fout:
        for doc_id, raw in iterator:
            text = unicodedata.normalize("NFC", raw).strip()
            if len(text) < args.min_chars:
                continue
            fout.write(json.dumps(
                {"id": f"ggponc-{doc_id}", "text": text, "entities": []},
                ensure_ascii=False,
            ) + "\n")
            docs += 1
            n_chars += len(text)
    print(f"wrote {out_path}: {docs} docs ({layout}), {n_chars/1024:.0f} KB",
          file=sys.stderr)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
