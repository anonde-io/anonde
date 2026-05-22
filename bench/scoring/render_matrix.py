#!/usr/bin/env python3
"""render_matrix — combine per-cell findings JSONLs into a single
cross-corpus, cross-engine report.

Cells are discovered on disk under:

    <corpora-root>/<corpus>/data/corpus.jsonl              (gold)
    <corpora-root>/<corpus>/data/anonde_<engine>.jsonl     (per-cell preds)

For each (corpus, engine) pair we compute precision / recall / F1 in the
three views compare.py uses (strict, partial, type-agnostic), plus
leak-rate and latency. Then we emit a single simplified REPORT_MATRIX.md
focused on the load-bearing tables:

  * TL;DR headline
  * Engine profiles (tier framing for anonde-patterns / anonde-gliner)
  * Per-corpus verdict cards (leak severity flags)
  * Leak rate grid (production metric)
  * Severity-weighted leak rate (procurement metric)
  * Latency p50 / p95 (operational metric)
  * Strict F1 — overall micro-F1 only (per-entity breakdown in CSV)
  * Cost reference (self-hosted vs managed)
  * Caveats — training-data overlap
  * Glossary

The CSV writes one row per (corpus, engine, entity, view) so downstream
analysis can pivot — including the per-entity-type strict-F1 breakdown
that used to live in the report.
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


def _weighted_leak_rate(gold, pred, severity: dict):
    """Same overlap-based leak detection as `_leak_rate`, but each
    gold span contributes its `severity[type]` to both numerator
    (when leaked) and denominator (always). Missing types default to
    1.0; weight 0 drops the span from the metric entirely.

    Returns (leaked_weight, total_weight) as floats so the upstream
    sum across all docs accumulates cleanly.
    """
    if not gold:
        return 0.0, 0.0
    leaked_w = 0.0
    total_w = 0.0
    for g in gold:
        w = float(severity.get(g.type, 1.0))
        if w <= 0:
            continue
        total_w += w
        if not any(_overlap(p, g) for p in pred):
            leaked_w += w
    return leaked_w, total_w


def _evaluate(gold_docs, pred_docs, gmap, pmap, canon_set, severity):
    strict = defaultdict(lambda: [0, 0, 0])
    partial = defaultdict(lambda: [0, 0, 0])
    typeonly = defaultdict(lambda: [0, 0, 0])
    leaked, total_gold = 0, 0
    leaked_w, total_gold_w = 0.0, 0.0
    durations = []

    # Partial-doc scoring. Some engines (openai-pf at ~80 s/doc) are run on
    # a deterministic subsample, so they only emit findings for N of M gold
    # docs. Scoring such an engine over all M would count every gold span in
    # the (M - N) unscored docs as a leak — a fake ~90%+ leak rate that is
    # really just "we didn't ask it about those docs".
    #
    # Correct behaviour: score each engine over the INTERSECTION of
    # (gold doc ids) ∩ (that engine's findings doc ids). A full-coverage
    # engine returns every doc, so the intersection equals the gold set and
    # this is a no-op for it. An engine that genuinely produced an empty
    # finding list for a doc (a real "found nothing" result) still appears
    # in pred_docs with `findings: []`, so it is correctly scored — only
    # docs the engine never saw are excluded.
    scored_ids = [doc_id for doc_id in gold_docs if doc_id in pred_docs]
    corpus_docs = len(gold_docs)
    scored_docs = len(scored_ids)

    for doc_id in scored_ids:
        gdoc = gold_docs[doc_id]
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
        lkw, totw = _weighted_leak_rate(g, p, severity)
        leaked_w += lkw; total_gold_w += totw
        if "duration_ms" in pdoc:
            durations.append(float(pdoc["duration_ms"]))
    return {
        "strict": dict(strict), "partial": dict(partial), "type_only": dict(typeonly),
        "leaked": leaked, "total_gold": total_gold,
        "leaked_weighted": leaked_w, "total_gold_weighted": total_gold_w,
        "durations": durations,
        # Coverage: how many of the corpus's gold docs this engine actually
        # returned findings for. scored_docs < corpus_docs ⇒ subsampled
        # engine (e.g. openai-pf --max-docs); the metrics above are computed
        # over scored_docs only, and _render footnotes the partial coverage.
        "scored_docs": scored_docs, "corpus_docs": corpus_docs,
    }


def _percentile(sorted_vals: list[float], q: float) -> float:
    """Nearest-rank percentile on a pre-sorted list (q in [0, 1]).

    Uses ceil(q * N) - 1 (0-indexed), clamped to [0, N-1]. Matches what
    most monitoring tools call p50/p95/p99.
    """
    if not sorted_vals:
        return 0.0
    import math
    idx = max(0, min(len(sorted_vals) - 1, math.ceil(q * len(sorted_vals)) - 1))
    return sorted_vals[idx]


def _latency(durations):
    if not durations:
        return {"median": 0.0, "mean": 0.0, "p50": 0.0, "p95": 0.0, "p99": 0.0}
    s = sorted(durations)
    return {
        "median": statistics.median(s),
        "mean": statistics.fmean(s),
        "p50": _percentile(s, 0.50),
        "p95": _percentile(s, 0.95),
        "p99": _percentile(s, 0.99),
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


def _fmt_latency(ms: float) -> str:
    """Mixed-unit latency: <1s as ms, >=1s as s, >=60s as min."""
    if ms < 1000:
        return f"{ms:.0f} ms"
    if ms < 60_000:
        return f"{ms / 1000:.1f} s"
    return f"{ms / 60_000:.1f} min"


def _fmt_leak_bar(rate: float) -> str:
    """Visual indicator of leak rate severity. 1 block per 10pp."""
    if rate < 0.05:
        return "🟢"
    if rate < 0.15:
        return "🟡"
    if rate < 0.30:
        return "🟠"
    if rate < 0.60:
        return "🔴"
    return "💀"  # essentially blind (>60% leak)


def _render(rows, label_map, corpora, engines):
    """rows: dict[(corpus, engine)] = evaluate-result-or-None.

    Simplified layout (per-entity strict-F1 breakdown lives in
    results_matrix.csv, not in this report):

      1. TL;DR (one-paragraph headline conclusion)
      2. Engine profiles (tier framing)
      3. Per-corpus verdict (leak severity flags)
      4. Leak rate (production metric)
      5. Severity-weighted leak rate (procurement metric)
      6. Latency p50 / p95 (operational metric)
      7. Strict F1 — overall micro-F1 only (per-entity in CSV)
      8. Cost reference (managed-service anchor; self-hosted framing)
      9. Caveats — training-data overlap
     10. Glossary
    """
    out: list[str] = []

    # ---- compute headline stats first so the TL;DR can quote them ------
    # For each corpus, find (winner_engine, winner_leak_rate, gap_vs_best_baseline).
    per_corpus_verdict: list[dict] = []
    for c in corpora:
        gold_cell = next((rows[(c, e)] for e in engines if (c, e) in rows), None)
        if gold_cell is None or gold_cell["total_gold"] == 0:
            per_corpus_verdict.append({"corpus": c, "scorable": False})
            continue
        engine_leaks: list[tuple[str, float, dict]] = []
        for e in engines:
            cell = rows.get((c, e))
            if cell is None or cell["total_gold"] == 0:
                continue
            rate = cell["leaked"] / cell["total_gold"]
            engine_leaks.append((e, rate, cell))
        if not engine_leaks:
            per_corpus_verdict.append({"corpus": c, "scorable": False})
            continue
        engine_leaks.sort(key=lambda x: x[1])
        winner = engine_leaks[0]
        # Best baseline = best non-anonde-gliner engine.
        gliner_row = next((x for x in engine_leaks if x[0] == "anonde-gliner"), None)
        baseline_row = next((x for x in engine_leaks if x[0] != "anonde-gliner"), None)
        per_corpus_verdict.append({
            "corpus": c,
            "scorable": True,
            "winner": winner,
            "gliner": gliner_row,
            "best_baseline": baseline_row,
            "engine_leaks": engine_leaks,
        })

    scorable = [v for v in per_corpus_verdict if v["scorable"]]
    gliner_wins = sum(
        1 for v in scorable
        if v["gliner"] is not None and v["winner"][0] == "anonde-gliner"
    )
    n_scorable = len(scorable)

    # ---- title + TL;DR ----------------------------------------------
    out.append("# 🛡️ anonde bench matrix\n")
    if n_scorable > 0:
        # Largest gliner-vs-baseline pp delta for the headline.
        biggest_pp = max(
            (v["best_baseline"][1] - v["gliner"][1])
            for v in scorable
            if v["gliner"] is not None and v["best_baseline"] is not None
        ) if any(v["gliner"] and v["best_baseline"] for v in scorable) else 0.0

        tldr = (
            f"> **TL;DR** — `anonde-gliner` (production) is the lowest-leak engine on "
            f"**{gliner_wins} of {n_scorable}** gold-annotated corpora. "
            f"Biggest absolute improvement over the best baseline: **{biggest_pp * 100:+.1f}pp** "
            f"in leak rate. Strict F1 trades exact-byte alignment for catching more PHI "
            f"— the right trade-off for a redactor, not a benchmark gaming exercise.\n"
        )
        out.append(tldr)
    else:
        out.append(
            "> **TL;DR** — no gold-annotated corpora available to score. "
            "Add a corpus with `entities: [...]` in its `corpus.jsonl` to enable F1 + leak-rate metrics.\n"
        )

    # ---- Engine profiles --------------------------------------------
    # Anonde-patterns and anonde-gliner are NOT two competing tools —
    # they're two deployment profiles of the same toolkit. Patterns is
    # the no-ML / no-CGO / 12 MB image baseline; gliner is the +470 MB
    # ML-backed production stack. Surfacing this up front so a reader
    # interprets the leak-rate tables as "compare across rows" not
    # "anonde-patterns ought to beat anonde-gliner".
    out.append("## Engine profiles\n")
    out.append("Engines below are not all competitors. `anonde-patterns` and "
               "`anonde-gliner` are two deployment tiers of the same anonde "
               "binary; compare *across the row* for the trade-off, not against "
               "each other for "
               "a winner.\n")
    out.append("| Engine | Profile | Image | CGO | Cold start | Best fit |")
    out.append("|---|---|---|---|---|---|")
    out.append("| `anonde-patterns` | regex / no-ML baseline (anonde tier 1) | "
               "~12 MB | not required | <1 s | structured slot-gen text (forms, "
               "logs, finance/legal docs) — wins F1 on PHONE, EMAIL, DATE, "
               "PROFESSION when the regex shape is tight |")
    out.append("| `anonde-gliner` | GLiNER PII + patterns (anonde tier 2, "
               "**production**) | ~470 MB | required | 5-30 s warmup | natural "
               "text + multilingual PHI; wins leak rate on most gold corpora |")
    out.append("| `presidio` | Microsoft Presidio (spaCy NER + regex) | "
               "~1 GB | not required | 3-10 s | well-formed English "
               "(strong on EN newswire-shaped text where spaCy was trained) |")
    out.append("| `gliner-py` | GLiNER via PyTorch + safetensors (FP32) | "
               "~3 GB | not required | 10-30 s | reference implementation; "
               "parity check vs anonde-gliner's INT8 ONNX path |")
    out.append("")

    # ---- per-corpus verdict cards -----------------------------------
    out.append("## Per-corpus verdict\n")
    out.append("`🟢/🟡/🟠/🔴/💀` flags the production engine's leak severity on each corpus. "
               "`⚪` corpora produced text but no span-level gold, so F1/leak are not measurable.\n")
    for v in per_corpus_verdict:
        c = v["corpus"]
        if not v["scorable"]:
            out.append(f"- ⚪ **`{c}`** — no gold annotations; precision-probe only "
                       f"(see `bench/corpora/{c}/README.md`).")
            continue
        gliner_row = v["gliner"]
        baseline_row = v["best_baseline"]
        if gliner_row is None:
            out.append(f"- ❔ **`{c}`** — `anonde-gliner` did not run on this corpus.")
            continue
        gliner_rate = gliner_row[1]
        flag = _fmt_leak_bar(gliner_rate)
        line = f"- {flag} **`{c}`** — `anonde-gliner` leaks **{gliner_rate:.1%}**"
        if baseline_row is not None:
            be, br, _ = baseline_row
            delta_pp = (br - gliner_rate) * 100
            arrow = "↓" if delta_pp > 0 else "↑"
            line += (
                f" vs the best baseline `{be}` at **{br:.1%}** "
                f"({arrow} **{abs(delta_pp):.1f}pp** {'better' if delta_pp > 0 else 'worse'})."
            )
        else:
            line += " — no comparable baseline ran on this corpus."
        out.append(line)
    out.append("")

    # ---- Leak rate grid (the load-bearing metric) -------------------
    out.append("## Leak rate · lower is better\n")
    out.append("A gold PHI span is *leaked* when **no** predicted span overlaps it. "
               "This is the metric that matters for redaction: 'did we miss a name?'\n")
    out.append("| Corpus | " + " | ".join(f"`{e}`" for e in engines) + " |")
    out.append("|---|" + "---:|" * len(engines))
    for c in corpora:
        scorable_cell = next((rows[(c, e)] for e in engines
                              if (c, e) in rows and rows[(c, e)]["total_gold"] > 0), None)
        if scorable_cell is None:
            out.append(f"| `{c}` |" + " – |" * len(engines))
            continue
        cells = [f"| `{c}` |"]
        # Find the best (lowest) leak for highlighting.
        best_rate = float("inf")
        rates = []
        for e in engines:
            cell = rows.get((c, e))
            if cell is None or cell["total_gold"] == 0:
                rates.append(None)
            else:
                r = cell["leaked"] / cell["total_gold"]
                rates.append(r)
                best_rate = min(best_rate, r)
        for r in rates:
            if r is None:
                cells.append("– |")
                continue
            txt = f"{r:.1%}"
            if abs(r - best_rate) < 1e-9:
                txt = f"**{txt}** 🥇"
            cells.append(f"{txt} |")
        out.append(" ".join(cells))
    out.append("")

    # ---- Partial-coverage footnote ----------------------------------
    # An engine that was run on a deterministic subsample (e.g. openai-pf
    # via --max-docs) only scored some of the corpus's gold docs. Every
    # metric above is computed over just those docs — call that out so a
    # reader does not compare a 40-doc sample against a 512-doc full run
    # as if they were the same population.
    coverage_notes: list[str] = []
    for c in corpora:
        for e in engines:
            cell = rows.get((c, e))
            if cell is None:
                continue
            scored = cell.get("scored_docs", 0)
            total = cell.get("corpus_docs", 0)
            if total > 0 and scored < total:
                coverage_notes.append(
                    f"- `{e}` on `{c}`: scored on **{scored}/{total} docs** "
                    f"(deterministic subsample — metrics above are over those "
                    f"{scored} docs only, not the full corpus)."
                )
    if coverage_notes:
        out.append("> **Partial coverage** — some engines were benchmarked "
                   "on a fixed subsample, not every gold doc:\n>")
        for note in coverage_notes:
            out.append("> " + note)
        out.append("")

    # ---- Severity-weighted leak rate --------------------------------
    # A leaked SSN is not equivalent to a leaked city. Weighted leak
    # multiplies each gold span by its severity weight (label_map.yaml
    # `severity:` section) before computing the ratio. Direct
    # identifiers (PERSON, PHONE, EMAIL, ADDRESS, dates) weigh 5;
    # high-stakes IDs (SSN/MRN/IBAN) weigh 10; quasi-identifiers
    # (LOCATION, ORG, PROFESSION, generic URL/AGE) weigh 1.
    #
    # Why this column exists: compliance teams already think in tiers,
    # and the raw leak rate flattens that signal. A bench that scores
    # tools only on raw recall rewards "catch everything that's easy"
    # over "catch the things that matter." This row weights toward the
    # things that matter.
    sev = label_map.get("severity") or {}
    if sev:
        out.append("## Severity-weighted leak rate · lower is better\n")
        out.append("Raw leak rate weights every span equally; severity-weighted leak "
                   "multiplies each by its compliance impact tier — direct identifiers "
                   "(PERSON, EMAIL, PHONE, ADDRESS, DOB) = 5, high-stakes IDs "
                   "(SSN/MRN/IBAN) = 10, quasi-identifiers (LOCATION, ORG, PROFESSION) "
                   "= 1. Defaults in `label_map.yaml::severity` — override per use case.\n")
        out.append("| Corpus | " + " | ".join(f"`{e}`" for e in engines) + " |")
        out.append("|---|" + "---:|" * len(engines))
        for c in corpora:
            scorable_cell = next((rows[(c, e)] for e in engines
                                  if (c, e) in rows and rows[(c, e)]["total_gold_weighted"] > 0), None)
            if scorable_cell is None:
                out.append(f"| `{c}` |" + " – |" * len(engines))
                continue
            cells = [f"| `{c}` |"]
            best_rate = float("inf")
            rates: list[float | None] = []
            for e in engines:
                cell = rows.get((c, e))
                if cell is None or cell["total_gold_weighted"] == 0:
                    rates.append(None)
                else:
                    r = cell["leaked_weighted"] / cell["total_gold_weighted"]
                    rates.append(r)
                    best_rate = min(best_rate, r)
            for r in rates:
                if r is None:
                    cells.append("– |")
                    continue
                txt = f"{r:.1%}"
                if abs(r - best_rate) < 1e-9:
                    txt = f"**{txt}** 🥇"
                cells.append(f"{txt} |")
            out.append(" ".join(cells))
        out.append("")

    # ---- Latency grid (mixed units, p50 + p95) ----------------------
    # Two columns per engine — p50 for steady-state UX, p95 for the
    # tail. Mean + p99 are in results_matrix.csv. We bias to p95 over
    # p99 because at sample sizes 100-1000 (typical bench corpus), p99
    # is dominated by a handful of outliers and unstable run-to-run.
    out.append("## Latency · per-document p50 / p95\n")
    out.append("Wall-clock per `engine.Analyze(doc)` call. p50 = steady-state, p95 = tail. "
               "Mean + p99 in `results_matrix.csv`. For redaction services, p95 is the SLO "
               "knob — the latency a customer waiting on `/v1/ingest` actually feels.\n")
    out.append("| Corpus | " + " | ".join(f"`{e}` p50 / p95" for e in engines) + " |")
    out.append("|---|" + "---:|" * len(engines))
    for c in corpora:
        cells = [f"| `{c}` |"]
        for e in engines:
            cell = rows.get((c, e))
            if cell is None or not cell["durations"]:
                cells.append("– |")
                continue
            lat = _latency(cell["durations"])
            cells.append(
                f"{_fmt_latency(lat['p50'])} / {_fmt_latency(lat['p95'])} |"
            )
        out.append(" ".join(cells))
    out.append("")

    # ---- Strict F1 (CoNLL-style: exact span + type) ----------------
    # This is the metric every NER paper publishes. We surface it here
    # (not just CSV) so you can cite a number that compares apples-to-
    # apples with academic baselines. Note: strict will be uniformly
    # lower than leak-derived metrics — exact-byte alignment is harder
    # than overlap, and many recognizers emit "Elena Rossi" vs gold's
    # ["Elena"] + ["Rossi"]. For a redactor that's not a bug.
    out.append("## Strict F1 · CoNLL exact span + type\n")
    out.append("Predicted span counts only if `(start, end, type)` matches gold exactly after "
               "label normalisation. The number every NER paper publishes; useful for direct "
               "comparison to academic baselines, less useful as a production metric "
               "(strict scoring penalises broader-or-narrower spans that still redact the PHI).\n")
    out.append("> Per-entity-type strict F1 (PERSON, LOCATION, ORG, DATE, AGE, PHONE, "
               "EMAIL, URL, ID, ADDRESS, PROFESSION, …) is in `results_matrix.csv` "
               "— that's the right place to triage which entity types each engine struggles with.\n")
    out.append("| Corpus | " + " | ".join(f"`{e}`" for e in engines) + " |")
    out.append("|---|" + "---:|" * len(engines))
    for c in corpora:
        cells = [f"| `{c}` |"]
        # Compute strict F1 per engine first so we can highlight the winner.
        f1s: list[float | None] = []
        for e in engines:
            cell = rows.get((c, e))
            if cell is None:
                f1s.append(None)
                continue
            f1s.append(_f1_overall(cell["strict"]))
        best = max((f for f in f1s if f is not None), default=None)
        for f in f1s:
            if f is None:
                cells.append("– |")
                continue
            txt = f"{f:.3f}"
            if best is not None and abs(f - best) < 1e-9 and best > 0:
                txt = f"**{txt}** 🥇"
            cells.append(f"{txt} |")
        out.append(" ".join(cells))
    out.append("")

    # ---- Cost reference ---------------------------------------------
    # All engines we benchmark are self-hostable, so per-cell cost columns
    # would be $0 across the board — uninteresting. Instead we anchor the
    # bench to managed-service market prices so a reader can compare what
    # they'd otherwise be paying. Prices are pinned with a "verified as of"
    # date; vendor pricing pages change, so re-verify before quoting.
    out.append("## Cost reference · USD per million characters\n")
    out.append("All engines in this matrix run on your hardware — no per-call charge. "
               "For procurement context, here is what the closest managed-service "
               "alternatives cost on their public pricing pages (verified 2026-05-15; "
               "vendor pricing drifts, re-check before quoting):\n")
    out.append("| Engine | Hosting | $/M chars | Notes |")
    out.append("|---|---|---:|---|")
    out.append("| `anonde-patterns` | self-host (small commodity VM) | "
               "~**$0.0005** | Patterns-only; runs on ~256 MB RAM. "
               "Amortised cost dominated by infra base. |")
    out.append("| `anonde-gliner` | self-host (~2 GB RAM VM) | "
               "~**$0.001** | GLiNER PII baked into image. ~2 GB RAM is enough; "
               "CPU-only, runs on any commodity cloud VM. |")
    out.append("| `presidio` | self-host (open-source) | **$0** marginal | "
               "Microsoft Presidio. spaCy backend, English-focused. |")
    out.append("| `gliner-py` | self-host (open-source) | **$0** marginal | "
               "Same GLiNER PII model via Python sidecar. |")
    out.append("| Google Cloud DLP (inspect) | managed | ~$1 / GB ≈ **$1.00** | "
               "1st GB/mo free; cheapest managed option by far. "
               "[pricing](https://cloud.google.com/sensitive-data-protection/pricing) |")
    out.append("| Azure AI Language PII | managed | ~$1 / 1k records ≈ **~$1.00** | "
               "Record = 1 000 chars. 5 000 records/mo free. "
               "[pricing](https://azure.microsoft.com/en-us/pricing/details/language/) |")
    out.append("| AWS Comprehend Medical (DetectPHI) | managed | $0.01 / 100 chars = "
               "**$100** | Tier 1; drops at volume. PHI-grade NER, English only. "
               "[pricing](https://aws.amazon.com/comprehend/medical/pricing/) |")
    out.append("")
    out.append("> Self-hosting anonde is **roughly 1 000–100 000× cheaper per million "
               "characters** than the managed alternatives — and the data never leaves "
               "your network. The leak-rate and F1 numbers in the tables above are how "
               "you tell if the quality tradeoff is acceptable.\n")

    # ---- Caveats / training-data biases ----------------------------
    # Some corpora are training-data-adjacent to the NLP backends we
    # bench. Calling that out keeps the matrix honest: a high score
    # on a corpus the engine was trained on is not the same as a
    # high score on a held-out one.
    out.append("## Caveats — training-data overlap\n")
    out.append("""\
A "win" on a corpus an engine was trained on (or trained near) is
weaker evidence than a win on a held-out one. Known overlaps in this
matrix:

- **`conll2003_en` × `presidio`** — Presidio's NER backend is spaCy's
  `en_core_web_lg`, trained on OntoNotes 5.0 with annotation
  guidelines derived from CoNLL-2003. The CoNLL-2003 EN test split is
  essentially home turf; Presidio's strict-F1 numbers here should be
  read as a *ceiling* on the model's accuracy, not as portable
  evidence that Presidio outperforms on EN PHI more broadly.
- **`germeval_14` / `wikiann_de` × `presidio`** — Same pattern in
  reverse: spaCy's `de_core_news_lg` is trained partly on TIGER and
  GermEval data. A high Presidio score here similarly reflects
  training-data adjacency.
- **`conll2003_en` / `wnut_17` × `anonde-gliner`** — anonde-gliner
  uses the INT8-quantised ONNX (`model_quint8.onnx`, ~196 MB) while
  `gliner-py` loads the FP32 safetensors via PyTorch. Both load the
  same upstream model; quantization appears to bite on noisy English
  NER even though it's invisible on clean German clinical text. See
  `TODO.md` for the planned FP32 ONNX comparison cell.

Held-out corpora with no known overlap for any of the four engines
listed in this matrix: `openmed` (GraSCCo PHI), `synth_clinical`,
`finance_de`, `legal_de`, `adversarial_de`, `ai4privacy_en`,
`pharmaconer_es`. Numbers there transfer most cleanly.
""")

    # ---- Glossary ---------------------------------------------------
    out.append("## What does this mean?\n")
    out.append("""\
- **Leak rate** = the fraction of gold PHI spans no predicted span overlaps. The single most
  important number for a PII redactor: each leaked span is a real piece of PHI we'd have
  missed in production.
- **Severity-weighted leak rate** = the same metric, but each leaked span contributes its
  compliance-impact weight (defaults: 10 for IDs / IBAN, 5 for PERSON / contact / DOB / street,
  1 for LOCATION / ORG / PROFESSION / generic URL, 0 to drop entirely). Use this when
  comparing tools for a procurement / compliance decision — flat leak rate over-rewards
  catching the easy quasi-identifiers and under-counts missing the hard ones.
- **Strict F1** = exact start, end, and type match against gold. The CoNLL-style metric every
  NER paper publishes; useful for direct academic comparison. Less useful as a redaction
  metric, since a span that's 11 chars vs gold's 5 still successfully tokenises (the
  cleartext is gone either way) — but every leaked span is one we'd have shipped in prod.
  Per-entity-type strict F1 and partial / type-agnostic F1 views are in `results_matrix.csv`.
- **`–` cells** = engine not run on that corpus. Reasons: language mismatch (Presidio is EN
  only), or corpus requires manual DUA registration (`ggponc_de`).
- **Partial coverage** = an engine scored on a deterministic subsample, not the full corpus.
  `openai-pf` is ~80 s/doc on CPU, so it is benchmarked on the first N docs (sorted by id) —
  see the "Partial coverage" footnote after the leak-rate grid. Its metrics are computed over
  only the docs it scored, so they are comparable in *kind* but not on the same doc population.
- **⚪ corpora** = precision-probe only (no span-level gold annotations). Useful for "does the
  engine over-redact ordinary prose?" checks, not for F1 / leak rate.
""")

    out.append(f"---\n*Generated by `bench/scoring/render_matrix.py` over "
               f"{len(rows)} cells. Full per-entity-type breakdown in `results_matrix.csv`.*\n")
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
    severity = label_map.get("severity", {}) or {}

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
            rows[(c, e)] = _evaluate(gold, preds, gmap, pmap, canon_set, severity)

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
