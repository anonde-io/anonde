"""Look at the new PROFESSION FPs introduced between the pre snapshot and
the post run. Show context for the first 8 in each corpus."""

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


def gold_prof(doc):
    return sorted(
        [(e["start"], e["end"]) for e in doc["entities"] if e.get("type") == "PROFESSION"]
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

    print(f"=== {name}: new PROFESSION predictions in post that aren't in pre ===")
    added_examples = []
    for gid in pre:
        p1 = set(prof(pre[gid]))
        p2 = set(prof(post[gid]))
        added = sorted(p2 - p1)
        if not added:
            continue
        text = corpus[gid]["text"]
        g = gold_prof(corpus[gid])
        for s, e in added:
            # Is this added span a merge of two adjacent old spans?
            inside_old = [(a, b) for a, b in p1 if a >= s and b <= e]
            # Is the added span a strict match with gold?
            matches_gold = (s, e) in set(g)
            ctx_l = max(0, s - 30)
            ctx_r = min(len(text), e + 30)
            added_examples.append(
                {
                    "doc": gid,
                    "added": (s, e, text[s:e]),
                    "context": text[ctx_l:ctx_r],
                    "premerge_spans_inside": [(a, b, text[a:b]) for a, b in inside_old],
                    "matches_gold": matches_gold,
                    "gold_in_doc": [(a, b, text[a:b]) for a, b in g],
                }
            )

    for ex in added_examples[:8]:
        print(f"  doc={ex['doc']}")
        print(f"    added: {ex['added']!r}")
        print(f"    inside_pre: {ex['premerge_spans_inside']!r}")
        print(f"    matches_gold: {ex['matches_gold']}")
        print(f"    context: ...{ex['context']!r}...")
        print()
    print(f"  total new added: {len(added_examples)}")
    n_merge = sum(1 for e in added_examples if len(e["premerge_spans_inside"]) >= 2)
    n_match_gold = sum(1 for e in added_examples if e["matches_gold"])
    print(f"  of these, looks like a merge (>=2 pre spans inside): {n_merge}")
    print(f"  of these, matches gold (strict): {n_match_gold}")
    print()
