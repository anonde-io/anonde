#!/usr/bin/env python3
"""loader — fetch GermEval 2014 NER (test split), sample N sentences,
emit anonde corpus.jsonl with gold PER / LOC / ORG spans.

GermEval-14 (Benikova et al., 2014) is the open-license German NER
benchmark: ~31k sentences from German Wikipedia and online news, with
hand-annotated PER / LOC / ORG / OTH spans plus *derived* and *part-of*
sub-tags (PERderiv / LOCpart / …). The dataset is distributed under
CC-BY 4.0, no DUA, no auth — the closest open German equivalent of
CoNLL-2003 EN.

We collapse all sub-tag variants to their base type (PERderiv → PER,
LOCpart → LOC, etc.) and drop OTH (no canonical anonde mapping). This
matches the practical convention used by every German NER paper that
cites this corpus.

Output schema matches the rest of bench/corpora/* — one JSON per line:

    {"id": "germeval-14-NNNN", "text": "...", "entities": [
      {"start": int, "end": int, "type": "PERSON" | "LOCATION" | "ORGANIZATION"}
    ]}
"""

from __future__ import annotations

import argparse
import json
import random
import sys
from pathlib import Path

# Base-tag → anonde canonical. We strip "deriv" / "part" suffixes before
# this lookup so PERderiv and PERpart both land in PERSON.
BASE2TYPE = {
    "PER": "PERSON",
    "LOC": "LOCATION",
    "ORG": "ORGANIZATION",
    # OTH → intentionally dropped (no canonical anonde mapping).
}

# Cap streaming work — test split is ~5100 sentences; 8000 is a safe
# upper bound that prevents runaway iteration on unexpected splits.
MAX_STREAM = 8000

# Preferred dataset id + split. We try `test` first (canonical eval
# split) and fall back to `validation` then `train` if test isn't
# materialised. The dataset is small (~31k sentences total) so eager
# loading would be fine too, but we keep streaming for consistency with
# the other corpora.
CANDIDATES: list[tuple[str, str | None, str]] = [
    ("gwlms/germeval2014", None, "test"),
    ("gwlms/germeval2014", None, "validation"),
    ("gwlms/germeval2014", None, "train"),
]


def _base_tag(tag: str) -> tuple[str | None, str | None]:
    """Strip BIO prefix + deriv/part suffix. Returns (b_or_i, base_tag).

    Examples:
        "O"          -> (None, None)
        "B-PER"      -> ("B", "PER")
        "I-LOCderiv" -> ("I", "LOC")
        "B-OTHpart"  -> ("B", "OTH")
    """
    if tag == "O" or not tag:
        return (None, None)
    if "-" not in tag:
        return (None, None)
    bi, rest = tag.split("-", 1)
    # Strip deriv / part suffixes if present.
    for suf in ("deriv", "part"):
        if rest.endswith(suf):
            rest = rest[: -len(suf)]
            break
    return (bi, rest)


# Hardcoded id-to-tag list for gwlms/germeval2014. Verified against the
# dataset's ClassLabel features (25 tags). Order is the standard German
# BIO encoding: O + (B/I × {LOC, LOCderiv, LOCpart, ORG, ORGderiv,
# ORGpart, OTH, OTHderiv, OTHpart, PER, PERderiv, PERpart}).
ID2TAG = [
    "O",
    "B-LOC", "I-LOC",
    "B-LOCderiv", "I-LOCderiv",
    "B-LOCpart", "I-LOCpart",
    "B-ORG", "I-ORG",
    "B-ORGderiv", "I-ORGderiv",
    "B-ORGpart", "I-ORGpart",
    "B-OTH", "I-OTH",
    "B-OTHderiv", "I-OTHderiv",
    "B-OTHpart", "I-OTHpart",
    "B-PER", "I-PER",
    "B-PERderiv", "I-PERderiv",
    "B-PERpart", "I-PERpart",
]


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--out", required=True)
    ap.add_argument("--n", type=int, default=300,
                    help="number of sentences to sample")
    ap.add_argument("--seed", type=int, default=20260515)
    args = ap.parse_args()

    try:
        from datasets import load_dataset
    except ImportError:
        print("missing dep: pip install datasets", file=sys.stderr)
        return 2

    rng = random.Random(args.seed)

    ds = None
    chosen: tuple[str, str | None, str] | None = None
    errors: list[str] = []
    for repo, cfg, split in CANDIDATES:
        try:
            if cfg is None:
                ds = load_dataset(repo, split=split, streaming=True)
            else:
                ds = load_dataset(repo, cfg, split=split, streaming=True)
            chosen = (repo, cfg, split)
            break
        except Exception as e:  # noqa: BLE001
            errors.append(f"{repo}({cfg}/{split}): {str(e)[:160]}")
            continue
    if ds is None:
        print("germeval_14: no mirror resolved.", file=sys.stderr)
        for err in errors:
            print(f"  - {err}", file=sys.stderr)
        return 2

    reservoir: list = []
    n_seen = 0
    try:
        for i, ex in enumerate(ds):
            n_seen = i + 1
            if i < args.n:
                reservoir.append(ex)
            else:
                j = rng.randrange(0, i + 1)
                if j < args.n:
                    reservoir[j] = ex
            if i >= MAX_STREAM:
                break
    except Exception as e:  # noqa: BLE001
        print(f"stream from {chosen!r} failed after {n_seen} examples: {e}",
              file=sys.stderr)
        return 2

    if not reservoir:
        print(f"no examples streamed from {chosen!r}", file=sys.stderr)
        return 2

    out_path = Path(args.out)
    out_path.parent.mkdir(parents=True, exist_ok=True)
    n_emitted = 0
    n_spans = 0
    with out_path.open("w", encoding="utf-8") as fh:
        for idx, ex in enumerate(reservoir):
            tokens = ex.get("tokens") or []
            tag_ids = ex.get("ner_tags") or []
            if not tokens or len(tokens) != len(tag_ids):
                continue

            # Build text + char-offset map (codepoint indices).
            text_parts: list[str] = []
            tok_starts: list[int] = []
            cursor = 0
            for ti, tok in enumerate(tokens):
                if ti > 0:
                    text_parts.append(" ")
                    cursor += 1
                tok_starts.append(cursor)
                text_parts.append(tok)
                cursor += len(tok)
            text = "".join(text_parts)
            tok_ends = [s + len(tokens[i]) for i, s in enumerate(tok_starts)]

            # Walk BIO tags into spans on the *base* tag (deriv / part
            # suffixes collapsed). A span starts at B-X and runs over
            # contiguous I-X tags of the same base type. Adjacent spans
            # of different base types break.
            entities = []
            i = 0
            while i < len(tag_ids):
                tid = tag_ids[i]
                tag = ID2TAG[tid] if 0 <= tid < len(ID2TAG) else "O"
                _bi, base = _base_tag(tag)
                if base is None:
                    i += 1
                    continue
                start_tok = i
                j = i + 1
                while j < len(tag_ids):
                    ntid = tag_ids[j]
                    nxt = ID2TAG[ntid] if 0 <= ntid < len(ID2TAG) else "O"
                    nbi, nbase = _base_tag(nxt)
                    if nbi == "I" and nbase == base:
                        j += 1
                        continue
                    break
                entity_type = BASE2TYPE.get(base)
                if entity_type:
                    entities.append({
                        "start": tok_starts[start_tok],
                        "end": tok_ends[j - 1],
                        "type": entity_type,
                    })
                    n_spans += 1
                i = j

            fh.write(json.dumps({
                "id": f"germeval-14-{idx:04d}",
                "text": text,
                "entities": entities,
            }, ensure_ascii=False) + "\n")
            n_emitted += 1

    print(f"wrote {out_path}: {n_emitted} docs, {n_spans} gold spans "
          f"(avg {n_spans/max(n_emitted,1):.1f} spans/doc) "
          f"[mirror: {chosen[0]}/{chosen[1]}/{chosen[2]}]",
          file=sys.stderr)
    return 0


if __name__ == "__main__":
    sys.exit(main())
