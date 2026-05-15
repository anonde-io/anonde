#!/usr/bin/env python3
"""loader — fetch German CoNLL-2003 NER (test split), sample N sentences,
emit anonde corpus.jsonl with gold PER / LOC / ORG spans.

Status: gated. The German split of CoNLL-2003 (Frankfurter Rundschau,
1992) is distributed by the LDC under a research-only license that
requires registration. At time of writing there is no public Hugging
Face mirror that hosts the gold annotations.

This loader tries a list of community-mirror candidates and falls back
gracefully — if none resolve, it prints a clear message and exits with
code 2. The bench harness treats exit-2 as "corpus unavailable, skip"
rather than a hard failure.

When a mirror does become available, simply add its HF repo id to
`MIRROR_CANDIDATES` below. The rest of the loader (BIO tag walk, JSONL
emit) is identical to `conll2003_en/loader.py` and stays correct as
long as the mirror exposes the canonical CoNLL-2003 BIO tag schema
(`tokens` + `ner_tags` with the standard 9-class label set).

Output schema matches the rest of bench/corpora/* — one JSON per line:

    {"id": "conll2003-de-NNNN", "text": "...", "entities": [
      {"start": int, "end": int, "type": "PERSON" | "LOCATION" | "ORGANIZATION"}
    ]}

Entity-type mapping: CoNLL-2003 uses PER / LOC / ORG / MISC →
PERSON / LOCATION / ORGANIZATION; MISC is dropped (no clean anonde
canonical mapping).
"""

from __future__ import annotations

import argparse
import json
import random
import sys
from pathlib import Path

TAG2TYPE = {
    "PER": "PERSON",
    "LOC": "LOCATION",
    "ORG": "ORGANIZATION",
    # MISC → intentionally dropped (no canonical anonde mapping).
}

# Canonical CoNLL-2003 BIO id-to-tag list. Verified against the dataset
# features of multiple mirrors (e.g. `tomaarsen/conll2003`). Hardcoded
# because streaming-mode hides the features schema.
ID2TAG = [
    "O",
    "B-PER", "I-PER",
    "B-ORG", "I-ORG",
    "B-LOC", "I-LOC",
    "B-MISC", "I-MISC",
]

# Cap streaming work — CoNLL-2003 DE test split is ~3.0k sentences, so
# 5000 is a safe upper bound that prevents runaway iteration if HF
# returns a different split shape than expected.
MAX_STREAM = 5000

# Mirror candidates, in preference order. Each entry is
# (repo_id, config_name_or_none, split). Configs are tried first
# (e.g. `tomaarsen/conll2003` historically had a `de` config); plain
# repos that ship DE as their default config are listed after.
#
# As of 2026-05, none of these resolve publicly:
#   - `tomaarsen/conll2003` ships only the English `default` config.
#   - `MalumaDev/conll2003-german`, `flozi00/conll2003_german`,
#     `severo/conll2003_german`, `PaDaS-Lab/conll2003-german`,
#     `tomaarsen/conll2003-german`, `darentang/conll2003-de` all
#     return "Dataset doesn't exist or cannot be accessed."
# The loader still tries them so this file Just Works the moment any
# mirror appears.
MIRROR_CANDIDATES: list[tuple[str, str | None, str]] = [
    ("tomaarsen/conll2003", "de", "test"),
    ("tomaarsen/conll2003", "de", "train"),
    ("MalumaDev/conll2003-german", None, "test"),
    ("flozi00/conll2003_german", None, "test"),
    ("severo/conll2003_german", None, "test"),
    ("PaDaS-Lab/conll2003-german", None, "test"),
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
    for repo, cfg, split in MIRROR_CANDIDATES:
        try:
            if cfg is None:
                ds = load_dataset(repo, split=split, streaming=True)
            else:
                ds = load_dataset(repo, cfg, split=split, streaming=True)
            chosen = (repo, cfg, split)
            break
        except Exception as e:  # noqa: BLE001 — graceful failure surface
            errors.append(f"{repo}({cfg}/{split}): {str(e)[:160]}")
            continue

    if ds is None:
        print(
            "conll2003_de: no public HF mirror resolved.\n"
            "The original German CoNLL-2003 corpus (Frankfurter Rundschau, "
            "1992) is LDC-gated and requires registration; community "
            "mirrors have come and gone but none currently host the gold "
            "annotations.\n\n"
            "If you have local access to the licensed corpus, drop a\n"
            "`data/corpus.jsonl` with {id,text,entities} into this folder\n"
            "and the bench will pick it up.\n\n"
            "Otherwise prefer `bench/corpora/germeval_14/` — open-license\n"
            "German NER (CC-BY 4.0) with the same PER/LOC/ORG entity types.\n",
            file=sys.stderr,
        )
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

            # Walk BIO tags into spans. Identical logic to
            # conll2003_en/loader.py — keep the two in sync.
            entities = []
            i = 0
            while i < len(tag_ids):
                tid = tag_ids[i]
                tag = ID2TAG[tid] if 0 <= tid < len(ID2TAG) else "O"
                if tag == "O":
                    i += 1
                    continue
                kind = tag.split("-", 1)[1]
                start_tok = i
                j = i + 1
                while j < len(tag_ids):
                    ntid = tag_ids[j]
                    nxt = ID2TAG[ntid] if 0 <= ntid < len(ID2TAG) else "O"
                    if nxt == f"I-{kind}":
                        j += 1
                        continue
                    break
                entity_type = TAG2TYPE.get(kind)
                if entity_type:
                    entities.append({
                        "start": tok_starts[start_tok],
                        "end": tok_ends[j - 1],
                        "type": entity_type,
                    })
                    n_spans += 1
                i = j

            fh.write(json.dumps({
                "id": f"conll2003-de-{idx:04d}",
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
