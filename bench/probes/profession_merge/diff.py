"""Diff pre-merge vs post-merge PROFESSION predictions per doc to see
which spans were newly created by the merge."""

import json
from pathlib import Path

REPO = Path(__file__).resolve().parents[3]
PROBE = Path(__file__).resolve().parent


def load(path):
    return {p["id"]: p for p in (json.loads(line) for line in open(path) if line.strip())}


def prof(rec):
    return sorted(
        [(f["start"], f["end"]) for f in rec["findings"] if f.get("type") == "PROFESSION"]
    )


for name in ("legal_de", "finance_de"):
    pre = load(PROBE / f"{name}_premerge.jsonl")
    post = load(REPO / "bench" / "corpora" / name / "data" / "anonde_anonde-ner.jsonl")
    corpus = {
        d["id"]: d
        for d in (
            json.loads(line)
            for line in open(REPO / "bench" / "corpora" / name / "data" / "corpus.jsonl")
            if line.strip()
        )
    }

    print(f"=== {name} ===")
    n_added = 0
    n_dropped = 0
    n_replaced = 0
    examples = []
    for gid in pre:
        p1 = set(prof(pre[gid]))
        p2 = set(prof(post[gid]))
        added = sorted(p2 - p1)
        dropped = sorted(p1 - p2)
        if not (added or dropped):
            continue
        # Try to match: each added span [a, b] should contain >= 2 dropped
        # spans separated by ASCII whitespace.
        text = corpus[gid]["text"]
        for a, b in added:
            contained = [(s, e) for s, e in dropped if s >= a and e <= b]
            contained.sort()
            merged = False
            if len(contained) >= 2:
                gaps_ws = True
                for (sa, ea), (sb, eb) in zip(contained, contained[1:]):
                    if not all(c in " \t\n\r" for c in text[ea:sb]):
                        gaps_ws = False
                        break
                if gaps_ws and contained[0][0] == a and contained[-1][1] == b:
                    n_replaced += 1
                    examples.append(
                        (gid, text[a:b], [text[s:e] for s, e in contained])
                    )
                    merged = True
            if not merged:
                n_added += 1
        # dropped that aren't part of a merge
        for s, e in dropped:
            still_dropped = True
            for a, b in added:
                if a <= s and e <= b:
                    still_dropped = False
                    break
            if still_dropped:
                n_dropped += 1

    print(f"  added (not-from-merge):  {n_added}")
    print(f"  dropped (not-into-merge): {n_dropped}")
    print(f"  merge events (N>=2 dropped → 1 added covering them): {n_replaced}")
    print(f"  example merge events (first 8):")
    for gid, merged_text, parts in examples[:8]:
        print(f"    {gid}: {parts!r} -> {merged_text!r}")
