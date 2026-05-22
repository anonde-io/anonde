#!/usr/bin/env python3
"""Score one or more engine prediction JSONLs against gold annotations.

Three views per (engine, canonical_entity):

  * Strict     exact (start, end, type) match
  * Partial    overlap, same canonical type
  * Type-only  overlap, type attributed to prediction's type

Plus an anonymisation leak rate per engine: a gold span is leaked if no
predicted span (of any type) overlaps it.

Labels are mapped through label_map.yaml so gold's GeMTeX names
(NAME_PATIENT, LOCATION_STREET, …) and anonde's internal names
(PERSON, STREET_ADDRESS, …) compare on a shared canonical vocabulary
(PERSON, LOCATION, ADDRESS, …).
"""

from __future__ import annotations

import argparse
import csv
import json
import statistics
import sys
from collections import defaultdict
from dataclasses import dataclass
from pathlib import Path


@dataclass(frozen=True)
class Span:
    start: int
    end: int
    type: str  # canonical label


def _load_jsonl(path: Path) -> dict[str, dict]:
    out: dict[str, dict] = {}
    with path.open("r", encoding="utf-8") as f:
        for line in f:
            line = line.strip()
            if not line:
                continue
            d = json.loads(line)
            out[d["id"]] = d
    return out


def _load_label_map(path: Path) -> dict:
    try:
        import yaml
    except ImportError:
        print("PyYAML required: pip install pyyaml", file=sys.stderr)
        raise
    with path.open("r", encoding="utf-8") as f:
        return yaml.safe_load(f)


def _normalize(label: str, mapping: dict, canonical: set[str]) -> str | None:
    if label not in mapping:
        return "OTHER"
    target = mapping[label]
    if target is None:
        return None
    if target not in canonical:
        return "OTHER"
    return target


def _gold_spans(doc: dict, gmap: dict, canonical: set[str]) -> list[Span]:
    out = []
    for e in doc.get("entities") or []:
        t = _normalize(e["type"], gmap, canonical)
        if t is None:
            continue
        out.append(Span(int(e["start"]), int(e["end"]), t))
    return out


def _pred_spans(doc: dict, pmap: dict, canonical: set[str]) -> list[Span]:
    out = []
    for f in doc.get("findings") or []:
        t = _normalize(f["type"], pmap, canonical)
        if t is None:
            continue
        out.append(Span(int(f["start"]), int(f["end"]), t))
    return out


def _overlap(a: Span, b: Span) -> bool:
    return a.start < b.end and a.end > b.start


def _prf(tp: int, fp: int, fn: int) -> tuple[float, float, float]:
    p = tp / (tp + fp) if (tp + fp) else 0.0
    r = tp / (tp + fn) if (tp + fn) else 0.0
    f = 2 * p * r / (p + r) if (p + r) else 0.0
    return p, r, f


def _strict(gold: list[Span], pred: list[Span]) -> dict[str, list[int]]:
    out: dict[str, list[int]] = defaultdict(lambda: [0, 0, 0])
    gset, pset = set(gold), set(pred)
    for t in {s.type for s in gset | pset}:
        gt = {s for s in gset if s.type == t}
        pt = {s for s in pset if s.type == t}
        out[t] = [len(gt & pt), len(pt - gt), len(gt - pt)]
    return out


def _partial(gold: list[Span], pred: list[Span]) -> dict[str, list[int]]:
    out: dict[str, list[int]] = defaultdict(lambda: [0, 0, 0])
    for t in {s.type for s in gold} | {s.type for s in pred}:
        gt = [s for s in gold if s.type == t]
        pt = [s for s in pred if s.type == t]
        matched_g: set[int] = set()
        tp = 0
        fp = 0
        for p in pt:
            hit = False
            for gi, g in enumerate(gt):
                if _overlap(p, g):
                    hit = True
                    matched_g.add(gi)
            if hit:
                tp += 1
            else:
                fp += 1
        out[t] = [tp, fp, len(gt) - len(matched_g)]
    return out


def _type_only(gold: list[Span], pred: list[Span]) -> dict[str, list[int]]:
    out: dict[str, list[int]] = defaultdict(lambda: [0, 0, 0])
    for p in pred:
        if any(_overlap(p, g) for g in gold):
            out[p.type][0] += 1
        else:
            out[p.type][1] += 1
    for g in gold:
        if not any(_overlap(p, g) for p in pred):
            out[g.type][2] += 1
    return out


def _leak_rate(gold: list[Span], pred: list[Span]) -> tuple[int, int]:
    if not gold:
        return 0, 0
    leaked = sum(1 for g in gold if not any(_overlap(p, g) for p in pred))
    return leaked, len(gold)


def _evaluate(gold_docs, pred_docs, gmap, pmap, canonical_plus_other):
    strict = defaultdict(lambda: [0, 0, 0])
    partial = defaultdict(lambda: [0, 0, 0])
    typeonly = defaultdict(lambda: [0, 0, 0])
    leaked, total_gold = 0, 0
    durations = []
    # Partial-doc scoring: score each engine over the intersection of
    # (gold doc ids) ∩ (that engine's findings doc ids). An engine run on a
    # deterministic subsample (e.g. openai-pf via --max-docs) only emits
    # findings for some docs; scoring it over all gold docs would count
    # every span in the unscored docs as a leak — a fake leak rate. A
    # full-coverage engine returns every doc, so the intersection equals
    # the gold set and this is a no-op for it.
    scored_ids = [doc_id for doc_id in gold_docs if doc_id in pred_docs]
    for doc_id in scored_ids:
        gdoc = gold_docs[doc_id]
        g = _gold_spans(gdoc, gmap, canonical_plus_other)
        pdoc = pred_docs.get(doc_id) or {"findings": []}
        p = _pred_spans(pdoc, pmap, canonical_plus_other)
        for t, (tp, fp, fn) in _strict(g, p).items():
            strict[t][0] += tp; strict[t][1] += fp; strict[t][2] += fn
        for t, (tp, fp, fn) in _partial(g, p).items():
            partial[t][0] += tp; partial[t][1] += fp; partial[t][2] += fn
        for t, (tp, fp, fn) in _type_only(g, p).items():
            typeonly[t][0] += tp; typeonly[t][1] += fp; typeonly[t][2] += fn
        lk, tot = _leak_rate(g, p)
        leaked += lk; total_gold += tot
        if "duration_ms" in pdoc:
            durations.append(float(pdoc["duration_ms"]))
    return {
        "strict": dict(strict), "partial": dict(partial), "type_only": dict(typeonly),
        "leaked": leaked, "total_gold": total_gold, "durations": durations,
    }


def _latency(durations):
    if not durations:
        return {"median": 0.0, "mean": 0.0, "p99": 0.0}
    s = sorted(durations)
    return {
        "median": statistics.median(s),
        "mean": statistics.fmean(s),
        "p99": s[max(0, int(0.99 * len(s)) - 1)],
    }


def _render_table(engine_results, view, canonical):
    lines = []
    engines = list(engine_results.keys())
    header = "| Entity | " + " | ".join(f"{e} P" for e in engines) + " | " \
        + " | ".join(f"{e} R" for e in engines) + " | " \
        + " | ".join(f"{e} F1" for e in engines) + " |"
    sep = "|---|" + ("---:|" * (3 * len(engines)))
    lines.append(header); lines.append(sep)
    for t in canonical + ["OTHER"]:
        cells = [f"| {t}"]
        ps, rs, fs = [], [], []
        for e in engines:
            tally = engine_results[e][view].get(t, [0, 0, 0])
            p, r, f = _prf(*tally)
            ps.append(p); rs.append(r); fs.append(f)
        cells.extend(f"{p:.2f}" for p in ps)
        cells.extend(f"{r:.2f}" for r in rs)
        cells.extend(f"**{f:.2f}**" for f in fs)
        lines.append(" | ".join(cells) + " |")
    return lines


def render(engine_results, n_docs, canonical):
    out = []
    out.append("# Anonymisation bench\n")
    out.append(f"Per-entity precision / recall / F1 over **{n_docs}** documents. "
               f"Labels normalised via label_map.yaml.\n")
    for view, title in (
        ("strict", "Strict (exact start+end+type)"),
        ("partial", "Partial (overlap, same canonical type)"),
        ("type_only", "Type-agnostic overlap"),
    ):
        out.append(f"## {title}\n")
        out.extend(_render_table(engine_results, view, canonical))
        out.append("")
    out.append("## Anonymisation leak rate\n")
    out.append("Lower is better. A gold PHI span is leaked when no predicted "
               "span overlaps it.\n")
    out.append("| Engine | Leaked | Total gold | Leak rate |")
    out.append("|---|---:|---:|---:|")
    for e, r in engine_results.items():
        rate = (r["leaked"] / r["total_gold"]) if r["total_gold"] else 0.0
        out.append(f"| {e} | {r['leaked']} | {r['total_gold']} | {rate:.2%} |")
    out.append("")
    out.append("## Latency\n")
    out.append("| Engine | median ms | mean ms | p99 ms |")
    out.append("|---|---:|---:|---:|")
    for e, r in engine_results.items():
        lat = _latency(r["durations"])
        out.append(f"| {e} | {lat['median']:.2f} | {lat['mean']:.2f} | {lat['p99']:.2f} |")
    out.append("")
    return "\n".join(out)


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--gold", required=True)
    ap.add_argument("--engine", action="append", required=True,
                    help="repeat: --engine name=path/to/preds.jsonl")
    ap.add_argument("--label-map", default=str(Path(__file__).parent / "label_map.yaml"))
    ap.add_argument("--out", required=True)
    ap.add_argument("--csv", default="")
    args = ap.parse_args()

    label_map = _load_label_map(Path(args.label_map))
    canonical: list[str] = list(label_map.get("canonical", []))
    canonical_set = set(canonical)
    gmap = label_map.get("gold", {}) or {}

    gold = _load_jsonl(Path(args.gold))

    engine_results: dict[str, dict] = {}
    for spec in args.engine:
        if "=" not in spec:
            print(f"--engine expects name=path, got {spec!r}", file=sys.stderr)
            return 2
        name, path = spec.split("=", 1)
        # Pick the right label_map section by engine-name prefix. Engine
        # names are conventional (e.g. "anonde-gliner", "presidio-default",
        # "gliner-py", "openai-pf") — the prefix routes to the right map.
        if name.startswith("anonde"):
            pmap = label_map.get("anonde", {}) or {}
        elif name.startswith("openmed"):
            pmap = label_map.get("openmed", {}) or {}
        elif name.startswith("presidio"):
            pmap = label_map.get("presidio", {}) or {}
        elif name.startswith("gliner-py") or name.startswith("gliner_py"):
            pmap = label_map.get("gliner-py", {}) or {}
        elif name.startswith("openai-pf") or name.startswith("openai_pf"):
            pmap = label_map.get("openai-pf", {}) or {}
        else:
            pmap = label_map.get(name, {}) or {}
        engine_results[name] = _evaluate(
            gold, _load_jsonl(Path(path)), gmap, pmap, canonical_set | {"OTHER"},
        )

    Path(args.out).write_text(render(engine_results, len(gold), canonical), encoding="utf-8")
    print(f"wrote {args.out}", file=sys.stderr)

    if args.csv:
        with open(args.csv, "w", newline="", encoding="utf-8") as fh:
            w = csv.writer(fh)
            w.writerow(["engine", "view", "entity", "tp", "fp", "fn",
                        "precision", "recall", "f1"])
            for engine, res in engine_results.items():
                for view in ("strict", "partial", "type_only"):
                    for t, (tp, fp, fn) in res[view].items():
                        p, r, f = _prf(tp, fp, fn)
                        w.writerow([engine, view, t, tp, fp, fn,
                                    f"{p:.4f}", f"{r:.4f}", f"{f:.4f}"])
        print(f"wrote {args.csv}", file=sys.stderr)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
