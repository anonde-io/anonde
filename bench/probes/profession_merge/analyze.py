"""Quantify the PROFESSION strict-F1 regression caused by NER-level
MergeAdjacentSameType. For each corpus we:

  * load gold and prediction JSONLs
  * pull PROFESSION-canonical entities on each side (gold maps PROFESSION
    only as PROFESSION; predictions emit anonde-internal PROFESSION)
  * for each doc, classify every gold PROFESSION FN as either
    'merge-induced' (a single prediction span fully covers >= 2 gold
    PROFESSION spans, with the gap between consecutive gold spans being
    only ASCII whitespace) or 'real-miss' (no prediction overlaps, or the
    prediction labels it as a different type, or the boundaries miss).
  * dump per-doc stats + 5 example documents to a JSON next to this script.

A pair (gold, prediction) is a strict match iff start, end, type are equal.

Output: profession_findings.json + a short stdout summary.
"""

import json
import os
import sys
from pathlib import Path

REPO = Path(__file__).resolve().parents[3]
PROBE_DIR = Path(__file__).resolve().parent


def load_jsonl(path):
    with open(path) as f:
        return [json.loads(line) for line in f if line.strip()]


def gold_profession(entities):
    return sorted(
        [(e["start"], e["end"], e["type"]) for e in entities if e.get("type") == "PROFESSION"],
        key=lambda e: e[0],
    )


def pred_profession(findings):
    return sorted(
        [(f["start"], f["end"], f["type"]) for f in findings if f.get("type") == "PROFESSION"],
        key=lambda e: e[0],
    )


def only_ascii_ws(text, a, b):
    if a >= b or a < 0 or b > len(text):
        return False
    return all(c in " \t\n\r" for c in text[a:b])


def classify_corpus(name):
    corpus = load_jsonl(REPO / "bench" / "corpora" / name / "data" / "corpus.jsonl")
    preds = load_jsonl(REPO / "bench" / "corpora" / name / "data" / "anonde_anonde-gliner.jsonl")
    preds_by_id = {p["id"]: p for p in preds}

    docs = []
    total_gold = 0
    total_pred = 0
    strict_matches = 0
    fn_merge_induced = 0
    fn_real = 0
    fp_merge_induced = 0
    fp_other = 0

    for doc in corpus:
        gid = doc["id"]
        text = doc["text"]
        g = gold_profession(doc["entities"])
        if not g:
            # not relevant for PROFESSION recall analysis; still count any
            # spurious PROFESSION predictions as FP-other.
            preds_doc = pred_profession(preds_by_id[gid]["findings"]) if gid in preds_by_id else []
            for p in preds_doc:
                fp_other += 1
                total_pred += 1
            continue
        preds_doc = pred_profession(preds_by_id[gid]["findings"]) if gid in preds_by_id else []
        total_gold += len(g)
        total_pred += len(preds_doc)

        # strict matches
        gold_set = set(g)
        pred_set = set(preds_doc)
        strict = gold_set & pred_set
        strict_matches += len(strict)

        # unmatched gold spans
        unmatched_gold = [x for x in g if x not in strict]
        # unmatched pred spans
        unmatched_pred = [x for x in preds_doc if x not in strict]

        # Now classify each unmatched_pred: does a single prediction cover
        # >= 2 unmatched_gold spans that are gold-separated only by ASCII
        # whitespace? If yes, those gold spans are merge-induced FNs and
        # the prediction is a merge-induced FP.
        used_gold = set()
        for p in unmatched_pred:
            ps, pe, _ = p
            covered = [gx for gx in unmatched_gold if gx[0] >= ps and gx[1] <= pe and gx not in used_gold]
            # Check: gaps between consecutive covered gold spans are only
            # ASCII whitespace, AND prediction boundary equals
            # min(covered start)..max(covered end) (the merge result).
            if len(covered) >= 2:
                covered.sort(key=lambda x: x[0])
                gaps_ws = True
                for a, b in zip(covered, covered[1:]):
                    if not only_ascii_ws(text, a[1], b[0]):
                        gaps_ws = False
                        break
                tight = covered[0][0] == ps and covered[-1][1] == pe
                if gaps_ws and tight:
                    used_gold.update(covered)
                    fn_merge_induced += len(covered)
                    fp_merge_induced += 1
                    docs.append(
                        {
                            "doc_id": gid,
                            "corpus": name,
                            "verdict": "merge-induced",
                            "gold": [
                                {"start": s, "end": e, "text": text[s:e]}
                                for s, e, _ in covered
                            ],
                            "prediction": {
                                "start": ps,
                                "end": pe,
                                "text": text[ps:pe],
                            },
                        }
                    )
                    continue
            # Not merge-induced; this prediction is either a real wrong
            # boundary or a stray PROFESSION span. Either way, FP-other.
            fp_other += 1

        # Remaining unmatched gold spans (not subsumed by a merge-induced
        # prediction) are real misses.
        for gx in unmatched_gold:
            if gx in used_gold:
                continue
            # Try to see if some prediction has a non-trivial overlap (any
            # bytes overlap) with this gold — record that as 'real miss
            # but overlapping pred'. If nothing overlaps at all, it's a
            # 'no prediction' miss.
            overlap = None
            for p in preds_doc:
                ps, pe, _ = p
                if ps < gx[1] and pe > gx[0]:
                    overlap = p
                    break
            fn_real += 1
            docs.append(
                {
                    "doc_id": gid,
                    "corpus": name,
                    "verdict": "real-miss",
                    "gold": [{"start": gx[0], "end": gx[1], "text": text[gx[0]:gx[1]]}],
                    "prediction": (
                        {"start": overlap[0], "end": overlap[1], "text": text[overlap[0]:overlap[1]], "type": overlap[2]}
                        if overlap
                        else None
                    ),
                }
            )

    return {
        "corpus": name,
        "total_gold_profession": total_gold,
        "total_pred_profession": total_pred,
        "strict_matches": strict_matches,
        "fn_merge_induced": fn_merge_induced,
        "fn_real": fn_real,
        "fp_merge_induced": fp_merge_induced,
        "fp_other": fp_other,
        "examples": docs,
    }


def main():
    summary = {}
    for name in ("legal_de", "finance_de"):
        s = classify_corpus(name)
        summary[name] = s
        # short stdout view
        tp = s["strict_matches"]
        fn_m = s["fn_merge_induced"]
        fn_r = s["fn_real"]
        fp_m = s["fp_merge_induced"]
        fp_o = s["fp_other"]
        # Strict P/R/F1 on PROFESSION only.
        p_den = tp + fp_m + fp_o
        r_den = tp + fn_m + fn_r
        prec = tp / p_den if p_den else 0
        rec = tp / r_den if r_den else 0
        f1 = (2 * prec * rec / (prec + rec)) if (prec + rec) else 0
        # Hypothetical: if merge-induced misses were strict-matched, what
        # would F1 be?
        tp_h = tp + fn_m
        p_den_h = tp_h + fp_o  # merge-induced FP collapsed back into many matches
        r_den_h = tp_h + fn_r
        prec_h = tp_h / p_den_h if p_den_h else 0
        rec_h = tp_h / r_den_h if r_den_h else 0
        f1_h = (2 * prec_h * rec_h / (prec_h + rec_h)) if (prec_h + rec_h) else 0
        print(
            f"{name}: gold={s['total_gold_profession']} pred={s['total_pred_profession']} "
            f"strict_tp={tp} fn_merge={fn_m} fn_real={fn_r} fp_merge={fp_m} fp_other={fp_o} "
            f"strict_F1={f1:.3f} hypothetical_no_merge_F1={f1_h:.3f}"
        )
    with open(PROBE_DIR / "profession_findings.json", "w") as f:
        json.dump(summary, f, indent=2, ensure_ascii=False)


if __name__ == "__main__":
    main()
