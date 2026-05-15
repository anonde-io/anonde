#!/usr/bin/env python3
"""loader — fetch English CoNLL-2003 NER (test split), sample N sentences,
emit anonde corpus.jsonl with gold PER / LOC / ORG spans.

CoNLL-2003 stores BIO tag IDs over token streams; we walk consecutive
B-/I- runs to build {start, end, type} character spans on a re-joined
sentence. The sentence is rebuilt by joining tokens with single spaces
— this matches the convention used by both the original shared-task
evaluation tooling and the rest of the bench's corpora (wikiann_de,
germeval_14).

Output schema matches the rest of bench/corpora/* — one JSON per line:

    {"id": "conll2003-en-NNNN", "text": "...", "entities": [
      {"start": int, "end": int, "type": "PERSON" | "LOCATION" | "ORGANIZATION"}
    ]}

Entity-type mapping: CoNLL-2003 uses PER / LOC / ORG / MISC →
PERSON / LOCATION / ORGANIZATION; MISC is dropped because it has no
anonde canonical equivalent (MISC includes nationalities, events,
products — too broad to map cleanly to a single PII type).
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

# CoNLL-2003 canonical id-to-tag list. Stable across HF revisions of the
# `conll2003` dataset; we hardcode it because streaming-mode hides the
# dataset's features schema.
ID2TAG = [
    "O",
    "B-PER", "I-PER",
    "B-ORG", "I-ORG",
    "B-LOC", "I-LOC",
    "B-MISC", "I-MISC",
]

# Cap streaming work — CoNLL-2003 test split is ~3.5k sentences, so 5000
# is a safe upper bound that prevents runaway iteration if HF returns a
# different split shape than expected.
MAX_STREAM = 5000


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

    # Stream the `test` split — the standard evaluation split for
    # CoNLL-2003. Reservoir-sample to avoid loading the full split into
    # memory and to keep behaviour identical to wikiann_de's loader.
    #
    # The canonical `conll2003` HF dataset uses a deprecated loading
    # script that newer `datasets` releases refuse to run. Fall back
    # through known community mirrors that ship the same data as plain
    # Parquet. All mirrors use the canonical CoNLL-2003 BIO tag set
    # (verified via dataset features); the ID2TAG constant above is
    # the authoritative mapping.
    candidates = [
        "conll2003",
        "tomaarsen/conll2003",
        "eriktks/conll2003",
    ]
    ds = None
    last_err: Exception | None = None
    for repo in candidates:
        try:
            ds = load_dataset(repo, split="test", streaming=True)
            break
        except Exception as e:  # noqa: BLE001
            last_err = e
            continue
    if ds is None:
        print(f"failed to load conll2003 test split from any mirror "
              f"({candidates}): {last_err}", file=sys.stderr)
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
        print(f"stream failed after {n_seen} examples: {e}", file=sys.stderr)
        return 2

    if not reservoir:
        print("no examples streamed from conll2003 test split",
              file=sys.stderr)
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

            # Build text + char-offset map (codepoint indices, since
            # Python strings are unicode).
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

            # Walk BIO tags into spans. A span starts at B-X and runs
            # over contiguous I-X tags. We also tolerate consecutive
            # B-X of same type (some HF revisions of CoNLL-2003 emit
            # B- where the canonical IOB2 would emit I-). Mismatched
            # I- tags break the span.
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
                "id": f"conll2003-en-{idx:04d}",
                "text": text,
                "entities": entities,
            }, ensure_ascii=False) + "\n")
            n_emitted += 1

    print(f"wrote {out_path}: {n_emitted} docs, {n_spans} gold spans "
          f"(avg {n_spans/max(n_emitted,1):.1f} spans/doc)", file=sys.stderr)
    return 0


if __name__ == "__main__":
    sys.exit(main())
