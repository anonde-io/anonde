"""Inspect the gold corpus for adjacent PROFESSION pairs separated only by
ASCII whitespace. If there are none, the merge cannot induce strict-FNs
on PROFESSION by collapsing two gold spans into a single predicted span.
"""

import json
from pathlib import Path

REPO = Path(__file__).resolve().parents[3]


def load(path):
    return [json.loads(l) for l in open(path) if l.strip()]


for name in ("legal_de", "finance_de"):
    corpus = load(REPO / "bench" / "corpora" / name / "data" / "corpus.jsonl")
    adjacent = 0
    examples = []
    for d in corpus:
        text = d["text"]
        prof = sorted(
            [(e["start"], e["end"]) for e in d["entities"] if e.get("type") == "PROFESSION"]
        )
        for a, b in zip(prof, prof[1:]):
            mid = text[a[1]:b[0]]
            if all(c in " \t\n\r" for c in mid):
                adjacent += 1
                examples.append(
                    {
                        "doc": d["id"],
                        "first": text[a[0]:a[1]],
                        "second": text[b[0]:b[1]],
                        "gap": repr(mid),
                    }
                )

    print(f"{name}: adjacent-gold-PROFESSION pairs (whitespace-only gap) = {adjacent}")
    for ex in examples[:5]:
        print(f"  {ex}")
