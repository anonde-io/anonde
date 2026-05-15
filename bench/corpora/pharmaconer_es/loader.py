#!/usr/bin/env python3
"""loader — fetch PharmaCoNER (Spanish clinical case reports) from
Hugging Face, sample N documents, emit anonde corpus.jsonl.

PharmaCoNER is a Spanish clinical NER corpus from the IberLEF 2019
shared task (BSC NLP, Plan-TL). It carries gold annotations for
**pharmacological substances, chemical compounds and proteins** in
1000 manually annotated clinical case reports. It does NOT carry
PERSON / LOCATION / ORGANIZATION spans — case reports are
de-identified before publication.

That makes this corpus a **precision probe** for anonde on Spanish
clinical prose: "given Spanish clinical text where there is no
real PHI to find, does anonde over-fire?". The same role wiki_de and
pmc_de play for German.

We still emit the gold chemical / drug spans as `OTHER` entries (anonde
scoring already recognises an `OTHER` bucket — see
bench/scoring/compare.py line ~199). They aren't PHI, but logging them
lets a follow-up analysis answer "do anonde's false positives line up
with chemical mentions?" — which is useful since GLiNER occasionally
mistakes drug names for proper nouns.

Entity-type mapping (PharmaCoNER -> anonde canonical):
  NORMALIZABLES     -> OTHER   (chemical/drug mention, SNOMED-normalisable)
  NO_NORMALIZABLES  -> OTHER   (chemical/drug mention, not normalisable)
  UNCLEAR           -> drop    (ambiguous span)
  PROTEINAS         -> drop    (proteins; out of scope for PHI bench)

Loading strategy: the original `bigbio/pharmaconer` and
`PlanTL-GOB-ES/pharmaconer` HF datasets ship as loading scripts, which
`datasets >= 4` refuses to execute. We bypass that by loading the
parquet conversion that the HF dataset viewer auto-generates at
`refs/convert/parquet` (the same files the dataset viewer serves).
Specifically the `pharmaconer_bigbio_kb` config — it carries the
character-offset `entities` list we need.

Offsets are codepoint indices (verified against `passages[0]['text']`
above) — same convention the rest of bench/corpora/* uses.

Output schema, one JSON per line:
    {"id": "pharmaconer-es-NNNN", "text": "...", "entities": [
       {"start": int, "end": int, "type": "OTHER"}
    ]}
"""

from __future__ import annotations

import argparse
import json
import random
import sys
from pathlib import Path

# PharmaCoNER raw tag -> anonde canonical bucket. `None` drops the span.
TAG2TYPE = {
    "NORMALIZABLES": "OTHER",
    "NO_NORMALIZABLES": "OTHER",
    "UNCLEAR": None,
    "PROTEINAS": None,
}

# HF dataset id + config to try, in priority order. The bigbio mirror is
# tried first because its KB config carries clean character offsets in a
# single passage; PlanTL is the original but its parquet conversion
# uses a tokenised IOB schema that's harder to walk.
HF_PARQUET_SOURCES = [
    ("bigbio/pharmaconer",
     "pharmaconer_bigbio_kb/train/0000.parquet"),
    ("bigbio/pharmaconer",
     "pharmaconer_bigbio_kb/validation/0000.parquet"),
]


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--out", required=True)
    ap.add_argument("--n", type=int, default=200,
                    help="number of clinical case reports to sample")
    ap.add_argument("--seed", type=int, default=20260515)
    args = ap.parse_args()

    try:
        from datasets import load_dataset
    except ImportError:
        print("missing dep: pip install --user datasets", file=sys.stderr)
        return 2

    # Pull every parquet shard from HF_PARQUET_SOURCES, concatenate, then
    # reservoir-sample N docs. PharmaCoNER train+val combined is ~750
    # docs so we can just load it all into memory cheaply.
    examples: list = []
    last_err: Exception | None = None
    for repo_id, parquet_path in HF_PARQUET_SOURCES:
        url = f"hf://datasets/{repo_id}@refs/convert/parquet/{parquet_path}"
        try:
            ds = load_dataset("parquet", data_files={"train": url},
                              split="train")
            for ex in ds:
                examples.append(ex)
        except Exception as exc:  # network, missing parquet, schema drift
            last_err = exc
            print(f"warn: could not load {url}: "
                  f"{type(exc).__name__}: {exc}", file=sys.stderr)
            continue

    if not examples:
        print("error: no PharmaCoNER documents loaded; last error: "
              f"{last_err!r}", file=sys.stderr)
        print("error: check network access to huggingface.co; or pin a "
              "different HF_PARQUET_SOURCES entry in this loader.",
              file=sys.stderr)
        return 2

    # Deterministic shuffle + take.
    rng = random.Random(args.seed)
    rng.shuffle(examples)
    sample = examples[:args.n]

    out_path = Path(args.out)
    out_path.parent.mkdir(parents=True, exist_ok=True)

    n_emitted = 0
    n_gold_spans = 0
    n_phi_spans = 0  # spans we'd recognise as PHI: should be 0
    n_dropped_oob = 0
    with out_path.open("w", encoding="utf-8") as fh:
        for idx, ex in enumerate(sample):
            passages = ex.get("passages") or []
            if not passages:
                continue
            # The BigBIO KB schema we load is single-passage per doc
            # (verified: all 500 train rows have len(passages)==1). We
            # defend the multi-passage case anyway by concatenating with
            # "\n" separators and adjusting per-passage offsets.
            text_chunks: list[str] = []
            passage_offsets: list[tuple[int, int]] = []
            cursor = 0
            for p in passages:
                ptext = p["text"]
                # `text` is `Sequence(string)` in the parquet schema —
                # always one element in this corpus.
                if isinstance(ptext, list):
                    ptext = ptext[0] if ptext else ""
                if not isinstance(ptext, str):
                    continue
                if text_chunks:
                    text_chunks.append("\n")
                    cursor += 1
                # original passage offsets (relative to source) — we
                # need (orig_start, our_cursor_start) so entities can be
                # rebased.
                porig = p.get("offsets")
                if isinstance(porig, list) and porig and isinstance(porig[0], list):
                    orig_start = int(porig[0][0])
                else:
                    orig_start = 0
                passage_offsets.append((orig_start, cursor))
                text_chunks.append(ptext)
                cursor += len(ptext)
            text = "".join(text_chunks)
            if not text:
                continue

            entities = []
            for ent in ex.get("entities") or []:
                raw_type = ent.get("type")
                canonical = TAG2TYPE.get(raw_type, None)
                if canonical is None:
                    continue
                if canonical == "OTHER":
                    n_gold_spans += 1
                else:
                    n_phi_spans += 1
                # PharmaCoNER entities can have multiple offset pairs
                # (discontinuous spans). We emit each fragment.
                for off in ent.get("offsets") or []:
                    if not (isinstance(off, list) and len(off) == 2):
                        continue
                    raw_s, raw_e = int(off[0]), int(off[1])
                    # rebase by the matching passage's offset delta. For
                    # single-passage docs this is just `raw_s - orig_start
                    # + our_cursor_start` which usually evaluates to
                    # `raw_s` (orig_start is typically 0).
                    new_s, new_e = raw_s, raw_e
                    for orig_start, cursor_start in passage_offsets:
                        if raw_s >= orig_start and \
                                raw_e <= orig_start + len(text) - cursor_start:
                            new_s = raw_s - orig_start + cursor_start
                            new_e = raw_e - orig_start + cursor_start
                            break
                    # Guard: drop spans that fall outside the rebuilt
                    # text (defensive; shouldn't happen on the KB
                    # config, but keeps the JSONL invariant).
                    if new_s < 0 or new_e > len(text) or new_s >= new_e:
                        n_dropped_oob += 1
                        continue
                    entities.append({
                        "start": new_s,
                        "end": new_e,
                        "type": canonical,
                    })

            fh.write(json.dumps({
                "id": f"pharmaconer-es-{idx:04d}",
                "text": text,
                "entities": entities,
            }, ensure_ascii=False) + "\n")
            n_emitted += 1

    print(f"wrote {out_path}: {n_emitted} docs, "
          f"{n_gold_spans} gold OTHER (chemical/drug) spans, "
          f"{n_phi_spans} PHI spans, {n_dropped_oob} dropped (out-of-bounds)",
          file=sys.stderr)
    if n_phi_spans == 0:
        print("note: zero PHI gold spans — this corpus is a PRECISION "
              "PROBE on Spanish clinical text (anonde findings are "
              "expected to be near-zero; OTHER gold is informational).",
              file=sys.stderr)
    return 0


if __name__ == "__main__":
    sys.exit(main())
