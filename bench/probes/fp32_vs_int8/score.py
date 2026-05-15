#!/usr/bin/env python3
"""Score FP32 vs INT8 ONNX runs for anonde-gliner on conll2003_en + wnut_17.

Reads gold corpora and per-cell findings JSONLs, applies the same
label_map normalisation as bench/scoring/compare.py, and prints a
markdown table with leak rate + strict F1 (overall and per entity type)
for each (corpus, variant) pair.

Stand-alone — does not import compare.py to keep the probe self-contained
and independent from any future API drift in the comparator. Uses the
same primitives (Span dataclass, _normalize, _strict, _leak_rate).

Outputs a `REPORT.md` next to itself when run with --out; otherwise prints
to stdout.
"""

from __future__ import annotations

import argparse
import json
import sys
from collections import defaultdict
from dataclasses import dataclass
from pathlib import Path

try:
    import yaml
except ImportError:
    print("PyYAML required: pip install pyyaml", file=sys.stderr)
    raise


REPO_ROOT = Path(__file__).resolve().parents[3]
CORPORA = REPO_ROOT / "bench" / "corpora"
LABEL_MAP = REPO_ROOT / "bench" / "scoring" / "label_map.yaml"
PROBE_DIR = Path(__file__).resolve().parent


@dataclass(frozen=True)
class Span:
    start: int
    end: int
    type: str


def load_jsonl(path: Path) -> dict[str, dict]:
    out: dict[str, dict] = {}
    with path.open("r", encoding="utf-8") as f:
        for line in f:
            line = line.strip()
            if not line:
                continue
            d = json.loads(line)
            out[d["id"]] = d
    return out


def load_label_map() -> tuple[set[str], dict, dict]:
    with LABEL_MAP.open("r", encoding="utf-8") as f:
        lm = yaml.safe_load(f)
    canonical = set(lm.get("canonical", []))
    gmap = lm.get("gold", {}) or {}
    pmap = lm.get("anonde", {}) or {}
    return canonical, gmap, pmap


def normalize(label: str, mapping: dict, canonical: set[str]) -> str | None:
    if label not in mapping:
        return "OTHER"
    target = mapping[label]
    if target is None:
        return None
    if target not in canonical:
        return "OTHER"
    return target


def gold_spans(doc: dict, gmap: dict, canonical: set[str]) -> list[Span]:
    out = []
    for e in doc.get("entities") or []:
        t = normalize(e["type"], gmap, canonical)
        if t is None:
            continue
        out.append(Span(int(e["start"]), int(e["end"]), t))
    return out


def pred_spans(doc: dict, pmap: dict, canonical: set[str]) -> list[Span]:
    out = []
    for f in doc.get("findings") or []:
        t = normalize(f["type"], pmap, canonical)
        if t is None:
            continue
        out.append(Span(int(f["start"]), int(f["end"]), t))
    return out


def overlap(a: Span, b: Span) -> bool:
    return a.start < b.end and a.end > b.start


def prf(tp: int, fp: int, fn: int) -> tuple[float, float, float]:
    p = tp / (tp + fp) if (tp + fp) else 0.0
    r = tp / (tp + fn) if (tp + fn) else 0.0
    f1 = 2 * p * r / (p + r) if (p + r) else 0.0
    return p, r, f1


def strict(gold: list[Span], pred: list[Span]) -> dict[str, list[int]]:
    out: dict[str, list[int]] = defaultdict(lambda: [0, 0, 0])
    gset, pset = set(gold), set(pred)
    for t in {s.type for s in gset | pset}:
        gt = {s for s in gset if s.type == t}
        pt = {s for s in pset if s.type == t}
        out[t] = [len(gt & pt), len(pt - gt), len(gt - pt)]
    return out


def leak_rate(gold: list[Span], pred: list[Span]) -> tuple[int, int]:
    if not gold:
        return 0, 0
    leaked = sum(1 for g in gold if not any(overlap(p, g) for p in pred))
    return leaked, len(gold)


def evaluate(gold_docs: dict, pred_docs: dict,
             gmap: dict, pmap: dict, canon: set[str]) -> dict:
    strict_tally: dict[str, list[int]] = defaultdict(lambda: [0, 0, 0])
    leaked, total_gold = 0, 0
    durations: list[float] = []
    for doc_id, gdoc in gold_docs.items():
        g = gold_spans(gdoc, gmap, canon)
        pdoc = pred_docs.get(doc_id) or {"findings": []}
        p = pred_spans(pdoc, pmap, canon)
        for t, (tp, fp, fn) in strict(g, p).items():
            strict_tally[t][0] += tp
            strict_tally[t][1] += fp
            strict_tally[t][2] += fn
        lk, tot = leak_rate(g, p)
        leaked += lk
        total_gold += tot
        if "duration_ms" in pdoc:
            durations.append(float(pdoc["duration_ms"]))
    return {
        "strict": dict(strict_tally),
        "leaked": leaked,
        "total_gold": total_gold,
        "durations": durations,
    }


def overall_strict_f1(stats: dict) -> float:
    tp = sum(v[0] for v in stats["strict"].values())
    fp = sum(v[1] for v in stats["strict"].values())
    fn = sum(v[2] for v in stats["strict"].values())
    _, _, f1 = prf(tp, fp, fn)
    return f1


def latency_p50_p95(durations: list[float]) -> tuple[float, float]:
    if not durations:
        return 0.0, 0.0
    s = sorted(durations)
    p50 = s[len(s) // 2]
    p95 = s[min(len(s) - 1, max(0, int(0.95 * len(s)) - 1))]
    return p50, p95


def score_cell(gold_path: Path, pred_path: Path) -> dict:
    canon, gmap, pmap = load_label_map()
    gold = load_jsonl(gold_path)
    pred = load_jsonl(pred_path)
    canon_plus = canon | {"OTHER"}
    return evaluate(gold, pred, gmap, pmap, canon_plus)


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--corpus", action="append", required=True,
                    help="repeat: --corpus conll2003_en")
    ap.add_argument("--int8-suffix", default="_anonde-gliner_int8.jsonl")
    ap.add_argument("--fp32-suffix", default="_anonde-gliner_fp32.jsonl")
    ap.add_argument("--probe-dir", default=str(PROBE_DIR))
    args = ap.parse_args()

    probe = Path(args.probe_dir)
    rows = []
    per_entity = []
    for corpus in args.corpus:
        gold_path = CORPORA / corpus / "data" / "corpus.jsonl"
        int8_path = probe / f"{corpus}{args.int8_suffix}"
        fp32_path = probe / f"{corpus}{args.fp32_suffix}"
        if not gold_path.exists():
            print(f"missing gold: {gold_path}", file=sys.stderr)
            return 2
        if not int8_path.exists():
            print(f"missing INT8 file: {int8_path}", file=sys.stderr)
            return 2
        if not fp32_path.exists():
            print(f"missing FP32 file: {fp32_path}", file=sys.stderr)
            return 2

        int8 = score_cell(gold_path, int8_path)
        fp32 = score_cell(gold_path, fp32_path)

        int8_leak = (int8["leaked"] / int8["total_gold"]) if int8["total_gold"] else 0.0
        fp32_leak = (fp32["leaked"] / fp32["total_gold"]) if fp32["total_gold"] else 0.0
        delta_pp = (fp32_leak - int8_leak) * 100.0  # percentage points

        int8_f1 = overall_strict_f1(int8)
        fp32_f1 = overall_strict_f1(fp32)

        int8_p50, int8_p95 = latency_p50_p95(int8["durations"])
        fp32_p50, fp32_p95 = latency_p50_p95(fp32["durations"])

        rows.append({
            "corpus": corpus,
            "int8_leak": int8_leak,
            "fp32_leak": fp32_leak,
            "delta_pp": delta_pp,
            "int8_f1": int8_f1,
            "fp32_f1": fp32_f1,
            "int8_leaked": int8["leaked"],
            "fp32_leaked": fp32["leaked"],
            "total_gold": int8["total_gold"],
            "int8_p50": int8_p50,
            "int8_p95": int8_p95,
            "fp32_p50": fp32_p50,
            "fp32_p95": fp32_p95,
        })

        # Per-entity strict F1 breakdown
        entity_types = sorted(set(int8["strict"].keys()) | set(fp32["strict"].keys()))
        for t in entity_types:
            i_tp, i_fp, i_fn = int8["strict"].get(t, [0, 0, 0])
            f_tp, f_fp, f_fn = fp32["strict"].get(t, [0, 0, 0])
            _, _, i_f = prf(i_tp, i_fp, i_fn)
            _, _, f_f = prf(f_tp, f_fp, f_fn)
            per_entity.append({
                "corpus": corpus,
                "entity": t,
                "int8_tp": i_tp, "int8_fp": i_fp, "int8_fn": i_fn,
                "fp32_tp": f_tp, "fp32_fp": f_fp, "fp32_fn": f_fn,
                "int8_f1": i_f,
                "fp32_f1": f_f,
            })

    # Emit markdown
    def verdict(d: float) -> str:
        if d <= -5.0:
            return "FP32 closes >=5pp"
        if d <= -1.0:
            return "FP32 modest gain"
        if -1.0 < d < 1.0:
            return "negligible"
        return "FP32 worse"

    print("## Leak rate (overall)\n")
    print("| Corpus | INT8 leak | FP32 leak | delta (pp) | Strict F1 INT8 | Strict F1 FP32 | Verdict |")
    print("|---|---:|---:|---:|---:|---:|---|")
    for r in rows:
        print(f"| `{r['corpus']}` | {r['int8_leak']:.1%} | {r['fp32_leak']:.1%} | "
              f"{r['delta_pp']:+.2f} | {r['int8_f1']:.3f} | {r['fp32_f1']:.3f} | "
              f"{verdict(r['delta_pp'])} |")

    print("\n## Raw counts\n")
    print("| Corpus | INT8 leaked / total | FP32 leaked / total |")
    print("|---|---|---|")
    for r in rows:
        print(f"| `{r['corpus']}` | {r['int8_leaked']} / {r['total_gold']} | "
              f"{r['fp32_leaked']} / {r['total_gold']} |")

    print("\n## Latency (per-doc ms)\n")
    print("| Corpus | INT8 p50 / p95 | FP32 p50 / p95 |")
    print("|---|---|---|")
    for r in rows:
        print(f"| `{r['corpus']}` | {r['int8_p50']:.1f} / {r['int8_p95']:.1f} | "
              f"{r['fp32_p50']:.1f} / {r['fp32_p95']:.1f} |")

    print("\n## Strict F1 per entity type\n")
    print("| Corpus | Entity | INT8 (tp/fp/fn -> F1) | FP32 (tp/fp/fn -> F1) | delta F1 |")
    print("|---|---|---|---|---:|")
    for r in per_entity:
        d = r["fp32_f1"] - r["int8_f1"]
        print(f"| `{r['corpus']}` | {r['entity']} | "
              f"{r['int8_tp']}/{r['int8_fp']}/{r['int8_fn']} -> {r['int8_f1']:.3f} | "
              f"{r['fp32_tp']}/{r['fp32_fp']}/{r['fp32_fn']} -> {r['fp32_f1']:.3f} | "
              f"{d:+.3f} |")

    return 0


if __name__ == "__main__":
    sys.exit(main())
