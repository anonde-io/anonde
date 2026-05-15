"""Take the truly-pre-merge prediction snapshot, apply MergeAdjacentSameType
in pure Python (mirroring anonymizer.MergeAdjacentSameType exactly), and
recompute strict-P/R/F1 on PROFESSION. This isolates the deterministic
effect of the merge from any run-to-run model non-determinism."""

import json
from pathlib import Path

REPO = Path(__file__).resolve().parents[3]
PROBE = Path(__file__).resolve().parent


def load_jsonl(path):
    return [json.loads(l) for l in open(path) if l.strip()]


def only_ws(text, a, b):
    if a >= b or a < 0 or b > len(text):
        return False
    return all(c in " \t\n\r" for c in text[a:b])


def merge_same_type(findings, text):
    if len(findings) < 2:
        return findings
    f = sorted(findings, key=lambda f: f["start"])
    out = [dict(f[0])]
    for r in f[1:]:
        last = out[-1]
        if (
            r["type"] == last["type"]
            and r["start"] > last["end"]
            and r["start"] <= len(text)
            and only_ws(text, last["end"], r["start"])
        ):
            last["end"] = r["end"]
            if r["score"] > last["score"]:
                last["score"] = r["score"]
            continue
        out.append(dict(r))
    return out


def score(corpus_path, pred_path, etype="PROFESSION"):
    corpus = {d["id"]: d for d in load_jsonl(corpus_path)}
    preds = load_jsonl(pred_path)

    tp_no = fp_no = fn_no = 0
    tp_m = fp_m = fn_m = 0

    # Diagnostic counts.
    merge_events_total = 0
    merge_events_per_type = {}
    n_merges_creating_gold_match = 0
    n_merges_creating_fp = 0
    n_merges_destroying_match = 0
    n_merges_destroying_fp = 0
    examples = []

    for rec in preds:
        gid = rec["id"]
        doc = corpus[gid]
        text = doc["text"]
        gold = doc["entities"]
        gold_by_type = {}
        for e in gold:
            gold_by_type.setdefault(e["type"], set()).add((e["start"], e["end"]))

        # Apply merge to the full prediction set (all types).
        before = rec["findings"]
        after = merge_same_type(before, text)
        before_set = set((f["start"], f["end"], f["type"]) for f in before)
        after_set = set((f["start"], f["end"], f["type"]) for f in after)

        dropped = before_set - after_set
        created = after_set - before_set

        # Count merge events: each created (start, end, type) typically
        # corresponds to a merge of 2+ dropped spans of the same type.
        for c in created:
            ctyp = c[2]
            merge_events_total += 1
            merge_events_per_type[ctyp] = merge_events_per_type.get(ctyp, 0) + 1
            gold_set = gold_by_type.get(ctyp, set())
            if (c[0], c[1]) in gold_set:
                n_merges_creating_gold_match += 1
            else:
                n_merges_creating_fp += 1
            if ctyp == "PROFESSION" and len(examples) < 8:
                # Find the dropped spans that produced this merge.
                contained = sorted(
                    [d for d in dropped if d[2] == ctyp and d[0] >= c[0] and d[1] <= c[1]],
                    key=lambda d: d[0],
                )
                examples.append(
                    {
                        "doc": gid,
                        "merged": (c[0], c[1], text[c[0]:c[1]]),
                        "components": [(d[0], d[1], text[d[0]:d[1]]) for d in contained],
                        "matches_gold": (c[0], c[1]) in gold_set,
                        "any_component_matched_gold": any(
                            (d[0], d[1]) in gold_set for d in contained
                        ),
                        "doc_gold_for_type": sorted(gold_set),
                    }
                )

        for d in dropped:
            dtyp = d[2]
            gold_set = gold_by_type.get(dtyp, set())
            if (d[0], d[1]) in gold_set:
                n_merges_destroying_match += 1
            else:
                n_merges_destroying_fp += 1

        # Compute strict-F1 contributions for PROFESSION only.
        gold_p = gold_by_type.get(etype, set())
        before_p = set((f["start"], f["end"]) for f in before if f["type"] == etype)
        after_p = set((f["start"], f["end"]) for f in after if f["type"] == etype)
        tp_no += len(gold_p & before_p)
        fp_no += len(before_p - gold_p)
        fn_no += len(gold_p - before_p)
        tp_m += len(gold_p & after_p)
        fp_m += len(after_p - gold_p)
        fn_m += len(gold_p - after_p)

    return {
        "no_merge": (tp_no, fp_no, fn_no),
        "merge": (tp_m, fp_m, fn_m),
        "merge_events_total": merge_events_total,
        "merge_events_per_type": merge_events_per_type,
        "merges_creating_gold_match": n_merges_creating_gold_match,
        "merges_creating_fp": n_merges_creating_fp,
        "merges_destroying_match": n_merges_destroying_match,
        "merges_destroying_fp": n_merges_destroying_fp,
        "examples": examples,
    }


def f1(tp, fp, fn):
    p = tp / (tp + fp) if (tp + fp) else 0
    r = tp / (tp + fn) if (tp + fn) else 0
    return p, r, (2 * p * r / (p + r)) if (p + r) else 0


for name in ("legal_de", "finance_de"):
    s = score(
        REPO / "bench" / "corpora" / name / "data" / "corpus.jsonl",
        PROBE / f"{name}_truly_premerge.jsonl",
    )
    tp_no, fp_no, fn_no = s["no_merge"]
    tp_m, fp_m, fn_m = s["merge"]
    p_no, r_no, f1_no = f1(tp_no, fp_no, fn_no)
    p_m, r_m, f1_m = f1(tp_m, fp_m, fn_m)
    print(f"=== {name} (PROFESSION) ===")
    print(f"  no-merge:   tp={tp_no} fp={fp_no} fn={fn_no} P={p_no:.3f} R={r_no:.3f} F1={f1_no:.3f}")
    print(f"  with-merge: tp={tp_m} fp={fp_m} fn={fn_m} P={p_m:.3f} R={r_m:.3f} F1={f1_m:.3f}")
    print(f"  merge events: total={s['merge_events_total']} per_type={s['merge_events_per_type']}")
    print(f"  merge created gold-match: {s['merges_creating_gold_match']}  / created FP: {s['merges_creating_fp']}")
    print(f"  merge destroyed gold-match: {s['merges_destroying_match']} / destroyed FP: {s['merges_destroying_fp']}")
    print(f"  PROFESSION merge events (first 8):")
    for ex in s["examples"]:
        print(f"    doc={ex['doc']}  merged={ex['merged']!r}")
        print(f"      components={ex['components']!r}")
        print(f"      matches_gold={ex['matches_gold']}  any_component_matched={ex['any_component_matched_gold']}")
    print()
