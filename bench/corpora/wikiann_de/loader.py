#!/usr/bin/env python3
"""loader — fetch German WikiAnn NER, sample N sentences, emit anonde
corpus.jsonl with gold PER / LOC / ORG spans.

The HF dataset stores BIO tag IDs; we walk consecutive B-/I- runs to
build {start, end, type} character spans on a re-joined sentence. The
sentence is rebuilt by joining tokens with single spaces — this is the
same heuristic the dataset's own evaluation tooling assumes, and matches
how downstream tokenisers (including anonde's regex recognizers) see the
text.

Output schema matches the rest of bench/corpora/* — one JSON per line:

    {"id": "wikiann-de-NNNN", "text": "...", "entities": [
      {"start": int, "end": int, "type": "PERSON" | "LOCATION" | "ORGANIZATION"}
    ]}

Entity-type mapping: WikiAnn uses PER / LOC / ORG → mapped to
PERSON / LOCATION / ORGANIZATION (the canonical names the rest of the
bench's label_map.yaml expects in the `gold:` section).
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
}


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--out", required=True)
    ap.add_argument("--n", type=int, default=300,
                    help="number of sentences to sample")
    ap.add_argument("--seed", type=int, default=20260513)
    args = ap.parse_args()

    try:
        from datasets import load_dataset
    except ImportError:
        print("missing dep: pip install datasets", file=sys.stderr)
        return 2

    # `train` is the biggest split. Stream and reservoir-sample to avoid
    # loading the full 20 k sentences into memory.
    rng = random.Random(args.seed)
    # Repo-id candidates, in preference order. The bare `wikiann` repo
    # was deprecated upstream and `huggingface_hub >= 0.27` rejects
    # single-name repo ids outright with `HfUriError: Repository id must
    # be 'namespace/name'`. Use a namespaced mirror; fall through to
    # alternates if any single mirror is offline.
    candidates = [
        ("unimelb-nlp/wikiann", "de"),
        ("tner/wikiann", "de"),
    ]
    ds = None
    last_err: Exception | None = None
    for repo, cfg in candidates:
        try:
            ds = load_dataset(repo, cfg, split="train", streaming=True)
            print(f"wikiann_de: loaded from {repo}({cfg})", file=sys.stderr)
            break
        except Exception as err:
            last_err = err
            print(f"wikiann_de: {repo}({cfg}) unavailable: {err}", file=sys.stderr)
    if ds is None:
        print("wikiann_de: no mirror resolved; cell will be missing", file=sys.stderr)
        print(f"last error: {last_err}", file=sys.stderr)
        return 2

    # Reservoir sampling.
    reservoir: list = []
    for i, ex in enumerate(ds):
        if i < args.n:
            reservoir.append(ex)
        else:
            j = rng.randrange(0, i + 1)
            if j < args.n:
                reservoir[j] = ex
        if i >= 20_000:  # cap streaming work — train is huge
            break

    # ner_tags integer→string map. The dataset's features.feature.names
    # gives the canonical mapping but streaming-only mode hides it; we
    # hardcode the well-known wikiann tag set instead (stable across
    # all HF revisions of this dataset).
    id2tag = ["O", "B-PER", "I-PER", "B-ORG", "I-ORG", "B-LOC", "I-LOC"]

    out_path = Path(args.out)
    out_path.parent.mkdir(parents=True, exist_ok=True)
    n_emitted = 0
    n_spans = 0
    with out_path.open("w", encoding="utf-8") as fh:
        for idx, ex in enumerate(reservoir):
            tokens = ex["tokens"]
            tag_ids = ex["ner_tags"]
            if not tokens:
                continue

            # Build text + char-offset map.
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

            # Walk BIO tags into spans. A span runs from B-X over any
            # contiguous I-X (or further B-X of same type — wikiann
            # sometimes lacks I- tags). Mismatched I- tags are tolerated
            # because the upstream tagger is heuristic.
            entities = []
            i = 0
            while i < len(tag_ids):
                tag = id2tag[tag_ids[i]] if tag_ids[i] < len(id2tag) else "O"
                if tag == "O":
                    i += 1
                    continue
                # tag in {B-, I-}-{PER, ORG, LOC}
                kind = tag.split("-", 1)[1]
                start_tok = i
                j = i + 1
                while j < len(tag_ids):
                    nxt = id2tag[tag_ids[j]] if tag_ids[j] < len(id2tag) else "O"
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
                "id": f"wikiann-de-{idx:04d}",
                "text": text,
                "entities": entities,
            }, ensure_ascii=False) + "\n")
            n_emitted += 1

    print(f"wrote {out_path}: {n_emitted} docs, {n_spans} gold spans "
          f"(avg {n_spans/max(n_emitted,1):.1f} spans/doc)", file=sys.stderr)
    return 0


if __name__ == "__main__":
    sys.exit(main())
