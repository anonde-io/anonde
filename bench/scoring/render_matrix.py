#!/usr/bin/env python3
"""render_matrix — combine per-cell findings JSONLs into a single
cross-corpus, cross-engine report.

Cells are discovered on disk under:

    <corpora-root>/<corpus>/data/corpus.jsonl              (gold)
    <corpora-root>/<corpus>/data/anonde_<engine>.jsonl     (per-cell preds)

For each (corpus, engine) pair we compute precision / recall / F1 in the
three views compare.py uses (strict, partial, type-agnostic), plus
leak-rate and latency. Then we emit a single REPORT_MATRIX.md with:

  * Headline: per-corpus "does production (anonde-gliner) win?" summary
  * F1 grid per view
  * Leak-rate grid
  * Latency grid (median / mean / p99)
  * Language coverage matrix (which cells produced data, which were
    skipped because the engine doesn't speak that language)
  * Top-3 disagreements per corpus (highest |F1(gliner) - F1(baseline)|)

The CSV writes one row per (corpus, engine, entity, view) so downstream
analysis can pivot.
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
    type: str


def _load_jsonl(path: Path) -> dict[str, dict]:
    out: dict[str, dict] = {}
    if not path.exists():
        return out
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


def _strict(gold, pred):
    out = defaultdict(lambda: [0, 0, 0])
    gset, pset = set(gold), set(pred)
    for t in {s.type for s in gset | pset}:
        gt = {s for s in gset if s.type == t}
        pt = {s for s in pset if s.type == t}
        out[t] = [len(gt & pt), len(pt - gt), len(gt - pt)]
    return out


def _partial(gold, pred):
    out = defaultdict(lambda: [0, 0, 0])
    for t in {s.type for s in gold} | {s.type for s in pred}:
        gt = [s for s in gold if s.type == t]
        pt = [s for s in pred if s.type == t]
        matched_g: set[int] = set()
        tp = fp = 0
        for p in pt:
            hit = False
            for gi, g in enumerate(gt):
                if _overlap(p, g):
                    hit = True
                    matched_g.add(gi)
            tp += int(hit)
            fp += int(not hit)
        out[t] = [tp, fp, len(gt) - len(matched_g)]
    return out


def _type_only(gold, pred):
    out = defaultdict(lambda: [0, 0, 0])
    for p in pred:
        if any(_overlap(p, g) for g in gold):
            out[p.type][0] += 1
        else:
            out[p.type][1] += 1
    for g in gold:
        if not any(_overlap(p, g) for p in pred):
            out[g.type][2] += 1
    return out


def _leak_rate(gold, pred):
    if not gold:
        return 0, 0
    leaked = sum(1 for g in gold if not any(_overlap(p, g) for p in pred))
    return leaked, len(gold)


def _evaluate(gold_docs, pred_docs, gmap, pmap, canon_set):
    strict = defaultdict(lambda: [0, 0, 0])
    partial = defaultdict(lambda: [0, 0, 0])
    typeonly = defaultdict(lambda: [0, 0, 0])
    leaked, total_gold = 0, 0
    durations = []
    for doc_id, gdoc in gold_docs.items():
        g = _gold_spans(gdoc, gmap, canon_set)
        pdoc = pred_docs.get(doc_id) or {"findings": []}
        p = _pred_spans(pdoc, pmap, canon_set)
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


def _pmap_for(engine: str, label_map: dict) -> dict:
    """Same routing convention as compare.py — engine-name prefix picks
    the right label_map section.
    """
    if engine.startswith("anonde"):
        return label_map.get("anonde", {}) or {}
    if engine.startswith("openmed"):
        return label_map.get("openmed", {}) or {}
    if engine.startswith("presidio"):
        return label_map.get("presidio", {}) or {}
    if engine.startswith("gliner-py") or engine.startswith("gliner_py"):
        return label_map.get("gliner-py", {}) or {}
    if engine.startswith("openai-pf") or engine.startswith("openai_pf"):
        return label_map.get("openai-pf", {}) or {}
    return label_map.get(engine, {}) or {}


def _f1_overall(view_tally: dict) -> float:
    """Micro-F1 across all entity types in a view."""
    tp = sum(v[0] for v in view_tally.values())
    fp = sum(v[1] for v in view_tally.values())
    fn = sum(v[2] for v in view_tally.values())
    _, _, f = _prf(tp, fp, fn)
    return f


def _render(rows, label_map, corpora, engines):
    """rows: dict[(corpus, engine)] = evaluate-result-or-None."""
    canonical = list(label_map.get("canonical", []))
    out: list[str] = []

    out.append("# anonde bench matrix\n")
    out.append(
        f"Cross-corpus, cross-engine F1 over {len(corpora)} corpora × {len(engines)} engines.\n"
        f"Engines and corpora that produced no findings (e.g. Presidio on German) are reported "
        f"as `–` so the language-coverage view is explicit.\n"
    )

    # ---- headline: anonde-gliner vs best baseline, per corpus ----------
    out.append("## Headline: anonde-gliner vs best baseline\n")
    out.append("Strict-F1 (micro across canonical entities). Production wins each row where "
               "`anonde-gliner F1 ≥ best-baseline F1`.\n")
    out.append("| Corpus | anonde-gliner | best baseline | Δ | win? |")
    out.append("|---|---:|---:|---:|:--:|")
    for c in corpora:
        gliner_cell = rows.get((c, "anonde-gliner"))
        if gliner_cell is None:
            continue
        gliner_f = _f1_overall(gliner_cell["strict"])
        best_baseline_name = "-"
        best_baseline_f = -1.0
        for e in engines:
            if e == "anonde-gliner":
                continue
            cell = rows.get((c, e))
            if cell is None:
                continue
            f = _f1_overall(cell["strict"])
            if f > best_baseline_f:
                best_baseline_f = f
                best_baseline_name = e
        if best_baseline_f < 0:
            out.append(f"| {c} | {gliner_f:.3f} | – | – | – |")
            continue
        delta = gliner_f - best_baseline_f
        win = "✓" if delta >= 0 else "✗"
        out.append(
            f"| {c} | **{gliner_f:.3f}** | {best_baseline_name} ({best_baseline_f:.3f}) | "
            f"{delta:+.3f} | {win} |"
        )
    out.append("")

    # ---- F1 grid per view ----------------------------------------------
    for view, title in (
        ("strict", "Strict F1 (exact start+end+type)"),
        ("partial", "Partial F1 (overlap, same canonical type)"),
        ("type_only", "Type-agnostic F1"),
    ):
        out.append(f"## {title}\n")
        out.append("Micro-F1 across canonical entity types. Empty = engine not run on that corpus.\n")
        header = "| Corpus | " + " | ".join(engines) + " |"
        sep = "|---|" + "---:|" * len(engines)
        out.append(header); out.append(sep)
        for c in corpora:
            cells = [f"| {c} |"]
            for e in engines:
                cell = rows.get((c, e))
                if cell is None:
                    cells.append("– |")
                    continue
                f = _f1_overall(cell[view])
                cells.append(f"{f:.3f} |")
            out.append(" ".join(cells))
        out.append("")

    # ---- Leak rate grid -------------------------------------------------
    out.append("## Anonymisation leak rate\n")
    out.append("Lower is better. A gold PHI span is leaked when no predicted span overlaps it.\n")
    header = "| Corpus | " + " | ".join(engines) + " |"
    sep = "|---|" + "---:|" * len(engines)
    out.append(header); out.append(sep)
    for c in corpora:
        cells = [f"| {c} |"]
        for e in engines:
            cell = rows.get((c, e))
            if cell is None or cell["total_gold"] == 0:
                cells.append("– |")
                continue
            rate = cell["leaked"] / cell["total_gold"]
            cells.append(f"{rate:.2%} |")
        out.append(" ".join(cells))
    out.append("")

    # ---- Latency grid ---------------------------------------------------
    out.append("## Latency — median / mean / p99 (ms)\n")
    header = "| Corpus | " + " | ".join(engines) + " |"
    sep = "|---|" + "---:|" * len(engines)
    out.append(header); out.append(sep)
    for c in corpora:
        cells = [f"| {c} |"]
        for e in engines:
            cell = rows.get((c, e))
            if cell is None or not cell["durations"]:
                cells.append("– |")
                continue
            lat = _latency(cell["durations"])
            cells.append(f"{lat['median']:.1f} / {lat['mean']:.1f} / {lat['p99']:.1f} |")
        out.append(" ".join(cells))
    out.append("")

    # ---- Coverage matrix ------------------------------------------------
    out.append("## Language coverage\n")
    out.append("`✓` = cell produced findings; `–` = skipped (engine not run on this corpus).\n")
    header = "| Corpus | " + " | ".join(engines) + " |"
    sep = "|---|" + ":-:|" * len(engines)
    out.append(header); out.append(sep)
    for c in corpora:
        cells = [f"| {c} |"]
        for e in engines:
            cell = rows.get((c, e))
            cells.append("✓ |" if cell is not None else "– |")
        out.append(" ".join(cells))
    out.append("")

    # ---- Disagreements per corpus --------------------------------------
    out.append("## Top disagreements per corpus\n")
    out.append("Per corpus: top 3 baselines ranked by |strict-F1(gliner) − strict-F1(baseline)|.\n")
    for c in corpora:
        gliner_cell = rows.get((c, "anonde-gliner"))
        if gliner_cell is None:
            continue
        gliner_f = _f1_overall(gliner_cell["strict"])
        deltas = []
        for e in engines:
            if e == "anonde-gliner":
                continue
            cell = rows.get((c, e))
            if cell is None:
                continue
            ef = _f1_overall(cell["strict"])
            deltas.append((e, gliner_f - ef, ef))
        deltas.sort(key=lambda x: abs(x[1]), reverse=True)
        if not deltas:
            continue
        out.append(f"### {c}\n")
        out.append("| Baseline | Δ vs anonde-gliner | baseline F1 |")
        out.append("|---|---:|---:|")
        for e, d, ef in deltas[:3]:
            out.append(f"| {e} | {d:+.3f} | {ef:.3f} |")
        out.append("")

    out.append("---\n")
    out.append(f"Generated by `bench/scoring/render_matrix.py` over {len(rows)} cells.\n")
    return "\n".join(out)


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--corpora-root", required=True,
                    help="root containing <corpus>/data/{corpus.jsonl,anonde_<engine>.jsonl}")
    ap.add_argument("--corpus", action="append", required=True,
                    help="repeat: --corpus openmed (positional-like list)")
    ap.add_argument("--engine", action="append", required=True,
                    help="repeat: --engine anonde-gliner ...")
    ap.add_argument("--label-map", required=True)
    ap.add_argument("--out", required=True)
    ap.add_argument("--csv", default="")
    args = ap.parse_args()

    # --corpus / --engine can be passed once with several space-separated
    # names (the way the top-level Makefile invokes them).
    corpora: list[str] = []
    for x in args.corpus:
        corpora.extend(x.split())
    engines: list[str] = []
    for x in args.engine:
        engines.extend(x.split())

    label_map = _load_label_map(Path(args.label_map))
    canonical = list(label_map.get("canonical", []))
    canon_set = set(canonical) | {"OTHER"}
    gmap = label_map.get("gold", {}) or {}

    root = Path(args.corpora_root)
    rows: dict[tuple[str, str], dict] = {}
    for c in corpora:
        gold_path = root / c / "data" / "corpus.jsonl"
        if not gold_path.exists():
            print(f"skip corpus {c}: no gold at {gold_path}", file=sys.stderr)
            continue
        gold = _load_jsonl(gold_path)
        for e in engines:
            pred_path = root / c / "data" / f"anonde_{e}.jsonl"
            if not pred_path.exists():
                continue
            pmap = _pmap_for(e, label_map)
            preds = _load_jsonl(pred_path)
            rows[(c, e)] = _evaluate(gold, preds, gmap, pmap, canon_set)

    Path(args.out).write_text(_render(rows, label_map, corpora, engines), encoding="utf-8")
    print(f"wrote {args.out}", file=sys.stderr)

    if args.csv:
        with open(args.csv, "w", newline="", encoding="utf-8") as fh:
            w = csv.writer(fh)
            w.writerow(["corpus", "engine", "view", "entity", "tp", "fp", "fn",
                        "precision", "recall", "f1"])
            for (c, e), res in rows.items():
                for view in ("strict", "partial", "type_only"):
                    for t, (tp, fp, fn) in res[view].items():
                        p, r, f = _prf(tp, fp, fn)
                        w.writerow([c, e, view, t, tp, fp, fn,
                                    f"{p:.4f}", f"{r:.4f}", f"{f:.4f}"])
        print(f"wrote {args.csv}", file=sys.stderr)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
