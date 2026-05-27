"""Simulate the effect of MergeAdjacentSameType on PROFESSION-only F1
deterministically. We:

  1. Take the checked-in prediction snapshot.
  2. Apply the exact MergeAdjacentSameType logic on each doc.
  3. Compute strict-F1 before vs after the merge — same predictions
     dataset, only difference is the merge pass.

This isolates the regression cleanly: any F1 delta is purely the merge,
not run-to-run model non-determinism.
"""

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
    """Reimplements anonymizer.MergeAdjacentSameType on a findings list.

    The findings list is sorted by start, then walked left-to-right: if
    the current span has the same type as the head of `out`, starts
    after head.end, and the gap between head.end and current.start is
    only ASCII whitespace, we extend head.end."""
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


def strict_pr(gold, pred, etype="PROFESSION"):
    g = set((e["start"], e["end"]) for e in gold if e.get("type") == etype)
    p = set((f["start"], f["end"]) for f in pred if f.get("type") == etype)
    tp = len(g & p)
    fp = len(p - g)
    fn = len(g - p)
    prec = tp / (tp + fp) if (tp + fp) else 0
    rec = tp / (tp + fn) if (tp + fn) else 0
    f1 = (2 * prec * rec / (prec + rec)) if (prec + rec) else 0
    return tp, fp, fn, prec, rec, f1


def main():
    for name in ("legal_de", "finance_de"):
        corpus = {
            d["id"]: d
            for d in load_jsonl(REPO / "bench" / "corpora" / name / "data" / "corpus.jsonl")
        }
        # Use the post-merge file as the "raw findings" source; we will
        # also reverse the merge to a deduplicated single-token view by
        # splitting on whitespace, then re-merge to get a clean baseline.
        # Simpler: just use both snapshots and compare the "what does
        # merging do to this snapshot" answer for each.
        for snap_path, label in (
            (PROBE / f"{name}_premerge.jsonl", "premerge_snapshot"),
            (REPO / "bench" / "corpora" / name / "data" / "anonde_anonde-ner.jsonl", "current_run"),
        ):
            data = load_jsonl(snap_path)
            tp_no = fp_no = fn_no = 0
            tp_m = fp_m = fn_m = 0
            n_merge_events = 0
            n_merge_helps = 0  # merge produces a new gold-strict-match
            n_merge_hurts_fp = 0  # merge produces a new FP
            n_merge_hurts_fn = 0  # merge removes a previously matching span
            for rec in data:
                gid = rec["id"]
                doc = corpus[gid]
                text = doc["text"]
                gold_set = set((e["start"], e["end"]) for e in doc["entities"] if e.get("type") == "PROFESSION")

                # Profession predictions before merge.
                pre = [f for f in rec["findings"] if f["type"] == "PROFESSION"]
                # The recognizer already merges, so split nothing — we
                # simulate merging an already-merged list (idempotent).
                # But if we want to test the merge effect, we need to
                # compare against a *non-merged* view. The recognizer's
                # output IS post-merge. So simulate the merge by applying
                # again — should be a no-op.
                post = merge_same_type(pre, text)

                pre_set = set((f["start"], f["end"]) for f in pre)
                post_set = set((f["start"], f["end"]) for f in post)

                for ps in pre_set - post_set:
                    if ps in gold_set:
                        n_merge_hurts_fn += 1
                for ps in post_set - pre_set:
                    n_merge_events += 1
                    if ps in gold_set:
                        n_merge_helps += 1
                    else:
                        n_merge_hurts_fp += 1

                tp_no += len(gold_set & pre_set)
                fp_no += len(pre_set - gold_set)
                fn_no += len(gold_set - pre_set)
                tp_m += len(gold_set & post_set)
                fp_m += len(post_set - gold_set)
                fn_m += len(gold_set - post_set)

            for label_in, t, f, n in [("no-merge", tp_no, fp_no, fn_no), ("post-merge", tp_m, fp_m, fn_m)]:
                p = t / (t + f) if (t + f) else 0
                r = t / (t + n) if (t + n) else 0
                f1 = (2 * p * r / (p + r)) if (p + r) else 0
                print(f"{name} [{label}] {label_in}: tp={t} fp={f} fn={n} P={p:.3f} R={r:.3f} F1={f1:.3f}")
            print(f"{name} [{label}] merge_events={n_merge_events} helps={n_merge_helps} hurts_fp={n_merge_hurts_fp} hurts_fn={n_merge_hurts_fn}")
            print()


if __name__ == "__main__":
    main()
