#!/usr/bin/env python3
"""render_matrix — combine per-cell findings JSONLs into a single
cross-corpus, cross-engine report.

Cells are discovered on disk under:

    <corpora-root>/<corpus>/data/corpus.jsonl              (gold)
    <corpora-root>/<corpus>/data/anonde_<engine>.jsonl     (per-cell preds)

For each (corpus, engine) pair we compute precision / recall / F1 in the
three views compare.py uses (strict, partial, type-agnostic), plus
leak-rate and latency.

Phase 5 of the multilingual bench expansion: the matrix now spans ~30
corpora across 5 languages and 6 domains, so the report is GROUPED BY
domain, then by language within each domain (it used to be a single
flat per-corpus list). The (domain, language) of each corpus comes from
`corpora.yaml` (loaded via --corpora-meta); a corpus absent from that
file degrades gracefully into an `uncategorized` / `unknown` group with
a stderr warning — it is never an error.

The emitted REPORT_MATRIX.md LEADS with one scannable scorecard, then
keeps the detailed grids below it as reference:

  * TL;DR headline
  * Scorecard — one table: leak rate per engine for every populated
    (domain × language) cell, anonde-anchored, with per-domain /
    per-language / overall roll-ups and an anonde win/loss tally. This
    is the table a human reads to answer "does anonde beat presidio on
    German legal?" in five seconds.
  * Engine profiles (tier framing for anonde-patterns / anonde-gliner)
  * Domain × language coverage map (which cells exist)
  * "# Detailed breakdown" — per (domain × language): leak-rate grid +
    per-corpus verdict cards + severity-weighted leak + latency +
    strict F1 (demoted below the scorecard, not deleted)
  * Cost reference (self-hosted vs managed)
  * Caveats — training-data overlap
  * Glossary

The CSV writes one row per (corpus, engine, entity, view) — now with
leading `domain` / `language` columns so downstream analysis can pivot
on the same axes the report groups by. The per-entity-type strict-F1
breakdown that used to live in the report stays in the CSV.
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


# ---- corpus metadata (domain / language) -------------------------------
# Fallback group for any corpus not listed in corpora.yaml. Kept as
# module constants so the warning text and the grouping logic agree.
UNCATEGORIZED_DOMAIN = "uncategorized"
UNKNOWN_LANGUAGE = "unknown"


def _load_corpora_meta(path: Path) -> dict:
    """Load corpora.yaml → {corpora, domain_order, language_order,
    domain_labels, language_labels}.

    Missing file is tolerated (not every invocation passes one): the
    renderer then puts every corpus under the uncategorized group. A
    present-but-malformed file still raises — that is a real bug.
    """
    try:
        import yaml
    except ImportError:
        print("PyYAML required: pip install pyyaml", file=sys.stderr)
        raise
    if not path.exists():
        print(f"warn: corpora metadata {path} not found — every corpus "
              f"will render under '{UNCATEGORIZED_DOMAIN}'", file=sys.stderr)
        return {}
    with path.open("r", encoding="utf-8") as f:
        return yaml.safe_load(f) or {}


def _corpus_meta(corpus: str, meta: dict) -> tuple[str, str]:
    """Return (domain, language) for a corpus, degrading gracefully.

    A corpus absent from corpora.yaml (or an entry missing either key)
    falls back to UNCATEGORIZED_DOMAIN / UNKNOWN_LANGUAGE. The caller
    collects every such corpus and emits ONE consolidated warning.
    """
    entry = (meta.get("corpora") or {}).get(corpus) or {}
    domain = entry.get("domain") or UNCATEGORIZED_DOMAIN
    language = entry.get("language") or UNKNOWN_LANGUAGE
    return domain, language


def _group_corpora(corpora: list[str], meta: dict) -> tuple[list, list[str]]:
    """Group the requested corpora into ordered (domain, language, [corpus])
    buckets.

    Returns (groups, unclassified) where:
      * groups   — list of (domain, language, corpus_list) in display
                   order: domains follow corpora.yaml `domain_order`
                   (unlisted domains appended alphabetically), languages
                   follow `language_order`, and corpora within a cell
                   keep the order they were requested in.
      * unclassified — corpora that fell back to the uncategorized group,
                   for the caller's consolidated warning.
    """
    buckets: dict[tuple[str, str], list[str]] = defaultdict(list)
    unclassified: list[str] = []
    for c in corpora:
        domain, language = _corpus_meta(c, meta)
        if domain == UNCATEGORIZED_DOMAIN or language == UNKNOWN_LANGUAGE:
            unclassified.append(c)
        buckets[(domain, language)].append(c)

    domain_order = list(meta.get("domain_order") or [])
    language_order = list(meta.get("language_order") or [])

    def _domain_key(d: str) -> tuple:
        # Listed domains sort by their index; unlisted (incl.
        # uncategorized) sort after, alphabetically.
        return (domain_order.index(d), "") if d in domain_order else (
            len(domain_order), d)

    def _lang_key(lang: str) -> tuple:
        return (language_order.index(lang), "") if lang in language_order else (
            len(language_order), lang)

    groups: list[tuple[str, str, list[str]]] = []
    for (domain, language) in sorted(
            buckets, key=lambda dl: (_domain_key(dl[0]), _lang_key(dl[1]))):
        groups.append((domain, language, buckets[(domain, language)]))
    return groups, unclassified


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


def _leak_grid(out: list[str], rows: dict, corpora: list[str],
               engines: list[str]) -> None:
    """Append a raw leak-rate grid (one row per corpus) to `out`. Used
    once per (domain × language) section — `corpora` is the section's
    corpus list, not the global one.
    """
    out.append("| Corpus | " + " | ".join(f"`{e}`" for e in engines) + " |")
    out.append("|---|" + "---:|" * len(engines))
    for c in corpora:
        scorable_cell = next((rows[(c, e)] for e in engines
                              if (c, e) in rows and rows[(c, e)]["total_gold"] > 0), None)
        if scorable_cell is None:
            out.append(f"| `{c}` |" + " – |" * len(engines))
            continue
        cells = [f"| `{c}` |"]
        best_rate = float("inf")
        rates: list[float | None] = []
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


def _weighted_leak_grid(out: list[str], rows: dict, corpora: list[str],
                        engines: list[str]) -> None:
    """Append a severity-weighted leak-rate grid for one section."""
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


def _latency_grid(out: list[str], rows: dict, corpora: list[str],
                  engines: list[str]) -> None:
    """Append a p50 / p95 per-document latency grid for one section."""
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
            cells.append(f"{_fmt_latency(lat['p50'])} / {_fmt_latency(lat['p95'])} |")
        out.append(" ".join(cells))
    out.append("")


def _strict_f1_grid(out: list[str], rows: dict, corpora: list[str],
                    engines: list[str]) -> None:
    """Append a strict (CoNLL exact span+type) micro-F1 grid for one section."""
    out.append("| Corpus | " + " | ".join(f"`{e}`" for e in engines) + " |")
    out.append("|---|" + "---:|" * len(engines))
    for c in corpora:
        cells = [f"| `{c}` |"]
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


# ---- headline scorecard ------------------------------------------------
# The single table a human reads first. Rows = every populated
# (domain × language) cell; columns = the engines; cell = leak rate %.
# anonde-gliner is the production engine, so its column is the anchor:
# every row tells you at a glance whether anonde wins or loses, and the
# roll-up rows summarise per-domain / per-language without scanning.

# The anonde column the scorecard anchors on (production engine). The
# win/verdict logic stays keyed on this one engine: `anonde-gliner` is
# what actually ships (INT8 ONNX), so "does anonde beat the field?" must
# be answered for the production build, not the FP32 reference cell.
SCORECARD_ANCHOR = "anonde-gliner"

# Anonde engine columns pinned to the FRONT of the scorecard, in this
# order, so the two GLiNER quantization variants render side by side.
# `anonde-gliner` (INT8, production — the verdict anchor) leads;
# `anonde-gliner-fp32` (FP32 ONNX, same model) sits immediately after it
# so a reader sees the INT8-vs-FP32 quantization tradeoff at a glance.
# Engines here that are absent from a given run are skipped silently.
SCORECARD_FRONT = ["anonde-gliner", "anonde-gliner-fp32"]


def _is_rival(engine: str) -> bool:
    """True for engines that count as a *baseline* in the anonde verdict.

    The verdict answers "does the production engine beat the competing
    field?" — so every `anonde-*` engine is excluded. In particular
    `anonde-gliner-fp32` is the same GLiNER PII model as the production
    `anonde-gliner`, just a different ONNX quantization: it is a tracked
    reference column, not a competitor, and must not flip a ✅ to ❌.
    """
    return not engine.startswith("anonde")


def _cell_leak(rows: dict, corpus: str, engine: str) -> float | None:
    """Leak rate for one (corpus, engine) cell, or None if not scorable."""
    cell = rows.get((corpus, engine))
    if cell is None or cell["total_gold"] == 0:
        return None
    return cell["leaked"] / cell["total_gold"]


def _group_leak(rows: dict, corpora: list[str], engine: str) -> float | None:
    """Pooled leak rate for an engine over a set of corpora — leaked
    spans summed over total gold spans (a doc-weighted mean, not a
    mean-of-means, so big corpora count proportionally).
    """
    leaked = 0
    total = 0
    for c in corpora:
        cell = rows.get((c, engine))
        if cell is None or cell["total_gold"] == 0:
            continue
        leaked += cell["leaked"]
        total += cell["total_gold"]
    if total == 0:
        return None
    return leaked / total


def _fmt_rate(r: float | None, best: bool = False) -> str:
    if r is None:
        return "–"
    txt = f"{r:.1%}"
    return f"**{txt}** 🥇" if best else txt


def _anchor_verdict(anchor: float | None, others: list[float | None]) -> str:
    """One-glyph verdict for the anonde anchor vs the field of baselines.

    ✅ anonde is strictly the lowest leak; 🟰 tied for lowest;
    ❌ at least one baseline leaks less; – anchor not scorable.
    """
    if anchor is None:
        return "–"
    rivals = [o for o in others if o is not None]
    if not rivals:
        return "✅"  # only engine scored — trivially the floor
    best_rival = min(rivals)
    if anchor < best_rival - 1e-9:
        return "✅"
    if abs(anchor - best_rival) < 1e-9:
        return "🟰"
    return "❌"


def _scorecard(out: list[str], rows: dict, groups: list, engines: list[str],
               domain_name, language_name) -> None:
    """Append the headline scorecard: one row per populated
    (domain × language) cell, leak rate per engine, anonde-anchored.

    `groups` is the output of `_group_corpora` — (domain, language,
    corpus_list) in display order. Roll-up rows (per domain, per
    language, overall) are pooled leak rates so a reader sees the
    aggregate without scanning every row.
    """
    anchor = SCORECARD_ANCHOR
    # Column order: the anonde engine columns first, pinned in
    # SCORECARD_FRONT order (anonde-gliner, then anonde-gliner-fp32) so
    # the two GLiNER quantization variants render adjacent. The anchor is
    # the production INT8 engine — every row's verdict is judged against
    # it, not against the FP32 reference cell. The remaining engines
    # follow in the order they were requested.
    front = [e for e in SCORECARD_FRONT if e in engines]
    others = [e for e in engines if e not in front]
    col_engines = front + others

    out.append("## 🎯 Scorecard · leak rate by domain × language\n")
    out.append(
        "The one table. Each row is a `(domain, language)` cell; each "
        f"number is **leak rate** (fraction of gold PHI spans missed — "
        f"lower is better). `{anchor}` is the anonde production engine "
        "(INT8 ONNX) and the anchor column; **Verdict** says whether it "
        "beats the field. `anonde-gliner-fp32` is the same GLiNER PII "
        "model loaded from the FP32 ONNX — it sits next to the anchor so "
        "the INT8-vs-FP32 quantization tradeoff is visible at a glance, "
        "but the verdict is keyed on the production INT8 engine. 🥇 marks "
        "the lowest-leak engine in the row. Roll-up rows pool "
        "leaked-over-gold across the group (doc-weighted, so larger "
        "corpora count more).\n")

    header = "| Domain | Language |"
    for e in col_engines:
        if e == anchor:
            tag = " ⬅︎ anonde (INT8, prod)"
        elif e == "anonde-gliner-fp32":
            tag = " · anonde (FP32)"
        else:
            tag = ""
        header += f" `{e}`{tag} |"
    header += " Verdict |"
    out.append(header)
    out.append("|---|---|" + "---:|" * len(col_engines) + ":--:|")

    # Track corpora per domain / per language for the roll-ups.
    by_domain: dict[str, list[str]] = defaultdict(list)
    by_language: dict[str, list[str]] = defaultdict(list)
    all_corpora: list[str] = []
    domain_seq: list[str] = []
    lang_seq: list[str] = []

    for domain, language, corpora in groups:
        if domain not in domain_seq:
            domain_seq.append(domain)
        if language not in lang_seq:
            lang_seq.append(language)
        by_domain[domain].extend(corpora)
        by_language[language].extend(corpora)
        all_corpora.extend(corpora)

        # Pooled leak rate for this exact (domain, language) cell.
        rates = [_group_leak(rows, corpora, e) for e in col_engines]
        scorable = [r for r in rates if r is not None]
        if not scorable:
            # No scorable engine for this cell — still show the row so
            # the coverage map and the scorecard agree.
            row = f"| **{domain_name(domain)}** | {language_name(language)} |"
            row += " – |" * len(col_engines) + " – |"
            out.append(row)
            continue
        best = min(scorable)
        anchor_rate = rates[col_engines.index(anchor)] if anchor in col_engines else None
        verdict = _anchor_verdict(
            anchor_rate,
            [r for e, r in zip(col_engines, rates) if _is_rival(e)])
        row = f"| **{domain_name(domain)}** | {language_name(language)} |"
        for e, r in zip(col_engines, rates):
            is_best = r is not None and abs(r - best) < 1e-9
            cell = _fmt_rate(r, is_best)
            if e == anchor and r is not None:
                cell = f"_{cell}_" if "**" not in cell else cell
            row += f" {cell} |"
        row += f" {verdict} |"
        out.append(row)

    # ---- roll-up: per domain ----------------------------------------
    out.append("|" + " |" * (len(col_engines) + 3))  # visual spacer row
    for domain in domain_seq:
        corpora = by_domain[domain]
        rates = [_group_leak(rows, corpora, e) for e in col_engines]
        scorable = [r for r in rates if r is not None]
        if not scorable:
            continue
        best = min(scorable)
        anchor_rate = rates[col_engines.index(anchor)] if anchor in col_engines else None
        verdict = _anchor_verdict(
            anchor_rate,
            [r for e, r in zip(col_engines, rates) if _is_rival(e)])
        row = f"| _Σ {domain_name(domain)}_ | _all_ |"
        for r in rates:
            is_best = r is not None and abs(r - best) < 1e-9
            row += f" {_fmt_rate(r, is_best)} |"
        row += f" {verdict} |"
        out.append(row)

    # ---- roll-up: per language --------------------------------------
    out.append("|" + " |" * (len(col_engines) + 3))
    for language in lang_seq:
        corpora = by_language[language]
        rates = [_group_leak(rows, corpora, e) for e in col_engines]
        scorable = [r for r in rates if r is not None]
        if not scorable:
            continue
        best = min(scorable)
        anchor_rate = rates[col_engines.index(anchor)] if anchor in col_engines else None
        verdict = _anchor_verdict(
            anchor_rate,
            [r for e, r in zip(col_engines, rates) if _is_rival(e)])
        row = f"| _Σ all domains_ | _{language_name(language)}_ |"
        for r in rates:
            is_best = r is not None and abs(r - best) < 1e-9
            row += f" {_fmt_rate(r, is_best)} |"
        row += f" {verdict} |"
        out.append(row)

    # ---- roll-up: overall -------------------------------------------
    out.append("|" + " |" * (len(col_engines) + 3))
    rates = [_group_leak(rows, all_corpora, e) for e in col_engines]
    scorable = [r for r in rates if r is not None]
    if scorable:
        best = min(scorable)
        anchor_rate = rates[col_engines.index(anchor)] if anchor in col_engines else None
        verdict = _anchor_verdict(
            anchor_rate,
            [r for e, r in zip(col_engines, rates) if _is_rival(e)])
        row = "| **Σ ALL** | **all** |"
        for r in rates:
            is_best = r is not None and abs(r - best) < 1e-9
            # _fmt_rate already bolds the row best; only add emphasis to
            # the non-best cells so the overall row reads bold without
            # double-starring the winner into literal `****`.
            if r is None:
                row += " – |"
            elif is_best:
                row += f" {_fmt_rate(r, True)} |"
            else:
                row += f" **{_fmt_rate(r, False)}** |"
        row += f" {verdict} |"
        out.append(row)
    out.append("")

    # ---- win/loss tally for the anonde anchor -----------------------
    # Counted over the per-cell rows only (not roll-ups), so it answers
    # "in how many domain×language cells does anonde lead the field?".
    wins = ties = losses = 0
    for domain, language, corpora in groups:
        rates = {e: _group_leak(rows, corpora, e) for e in col_engines}
        anchor_rate = rates.get(anchor)
        if anchor_rate is None:
            continue
        rivals = [r for e, r in rates.items() if _is_rival(e) and r is not None]
        if not rivals:
            wins += 1
            continue
        best_rival = min(rivals)
        if anchor_rate < best_rival - 1e-9:
            wins += 1
        elif abs(anchor_rate - best_rival) < 1e-9:
            ties += 1
        else:
            losses += 1
    n_cells = wins + ties + losses
    out.append(
        f"> **Anonde scoreboard** — across the **{n_cells}** populated "
        f"`(domain, language)` cells above, `{anchor}` is the "
        f"**lowest-leak engine in {wins}**, ties in **{ties}**, and is "
        f"beaten in **{losses}**. ✅ = anonde leads · 🟰 = tied · ❌ = a "
        "baseline leaks less. Read the row to see which baseline, then "
        "the Detailed breakdown below for severity-weighted leak, "
        "latency, and strict F1. (The TL;DR's win count is per-corpus, "
        "a finer split than these per-cell rows.)\n")


def _verdict_cards(out: list[str], per_corpus_verdict: list[dict],
                   section_corpora: set[str]) -> None:
    """Append the per-corpus leak-severity verdict cards for the corpora
    in `section_corpora` (the verdict dicts are computed globally; we
    just filter to this section).
    """
    for v in per_corpus_verdict:
        c = v["corpus"]
        if c not in section_corpora:
            continue
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


def _render(rows, label_map, corpora, engines, meta=None):
    """rows: dict[(corpus, engine)] = evaluate-result-or-None.
    meta: parsed corpora.yaml (domain/language metadata); may be {}.

    Layout (per-entity strict-F1 breakdown lives in results_matrix.csv):

      1. TL;DR (one-paragraph headline conclusion)
      2. Scorecard — THE table: one row per (domain × language) cell,
         leak rate per engine, anonde-anchored, with per-domain /
         per-language / overall roll-ups + a win/loss tally
      3. Engine profiles (tier framing)
      4. Domain × language coverage map
      5. "# Detailed breakdown" — per (domain × language) section, in
         display order, each with:
           - per-corpus verdict cards (leak severity flags)
           - leak-rate grid (production metric)
           - partial-coverage footnote (if any cell was subsampled)
           - severity-weighted leak rate (procurement metric)
           - latency p50 / p95 (operational metric)
           - strict F1 — overall micro-F1 only (per-entity in CSV)
      6. Cost reference (managed-service anchor; self-hosted framing)
      7. Caveats — training-data overlap
      8. Glossary
    """
    meta = meta or {}
    out: list[str] = []

    # ---- group corpora by (domain, language) ------------------------
    groups, _unclassified = _group_corpora(corpora, meta)
    domain_labels = meta.get("domain_labels") or {}
    language_labels = meta.get("language_labels") or {}

    def _domain_name(d: str) -> str:
        return domain_labels.get(d, d.replace("_", " ").title())

    def _language_name(lang: str) -> str:
        return language_labels.get(lang, lang.upper())

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
        # gliner_row = the production engine; best_baseline = the best
        # NON-anonde engine. `anonde-gliner-fp32` is the same model as
        # production, just a different ONNX quantization — it is a
        # tracked reference column, not a competing baseline, so
        # `_is_rival` excludes it here exactly as in the scorecard
        # verdict. (The verdict card would otherwise quote FP32 as "the
        # best baseline", which is misleading.)
        gliner_row = next((x for x in engine_leaks if x[0] == "anonde-gliner"), None)
        baseline_row = next((x for x in engine_leaks if _is_rival(x[0])), None)
        per_corpus_verdict.append({
            "corpus": c,
            "scorable": True,
            "winner": winner,
            "gliner": gliner_row,
            "best_baseline": baseline_row,
            "engine_leaks": engine_leaks,
        })

    scorable = [v for v in per_corpus_verdict if v["scorable"]]
    # A "win" = the production engine (`anonde-gliner`, INT8) leaks no
    # more than every NON-anonde baseline on that corpus. Counted against
    # rivals only — `anonde-gliner-fp32` is the same model at a different
    # ONNX quantization, so it never costs production a win (mirrors the
    # scorecard verdict's `_is_rival` rule).
    gliner_wins = 0
    for v in scorable:
        if v["gliner"] is None:
            continue
        gliner_rate = v["gliner"][1]
        rival_rates = [r for (e, r, _c) in v["engine_leaks"] if _is_rival(e)]
        if not rival_rates or gliner_rate <= min(rival_rates) + 1e-9:
            gliner_wins += 1
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
            f"> **TL;DR** — `anonde-gliner` (production, INT8 ONNX) leaks no more than "
            f"every competing baseline on **{gliner_wins} of {n_scorable}** gold-annotated "
            f"corpora. Biggest absolute improvement over the best baseline: "
            f"**{biggest_pp * 100:+.1f}pp** in leak rate. The `anonde-gliner-fp32` column is "
            f"the same GLiNER PII model at FP32 instead of INT8 — it tracks the "
            f"quantization tradeoff and is not counted as a competitor. Strict F1 trades "
            f"exact-byte alignment for catching more PHI — the right trade-off for a "
            f"redactor, not a benchmark gaming exercise.\n"
        )
        out.append(tldr)
    else:
        out.append(
            "> **TL;DR** — no gold-annotated corpora available to score. "
            "Add a corpus with `entities: [...]` in its `corpus.jsonl` to enable F1 + leak-rate metrics.\n"
        )

    # ---- Headline scorecard -----------------------------------------
    # The single scannable table: leak rate per engine for every
    # populated (domain × language) cell, anonde-anchored, with
    # per-domain / per-language / overall roll-ups. This is THE table a
    # human reads — everything below it is reference detail.
    _scorecard(out, rows, groups, engines, _domain_name, _language_name)

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
               "text + multilingual PHI; wins leak rate on most gold corpora. "
               "Ships the INT8 ONNX (`model_quint8.onnx`). |")
    out.append("| `anonde-gliner-fp32` | same GLiNER PII model, FP32 ONNX "
               "(`model.onnx`) — reference column, not a separate tier | "
               "~770 MB | required | 5-30 s warmup | not a competitor: tracks the "
               "INT8-vs-FP32 quantization tradeoff vs production `anonde-gliner`. "
               "INT8 depresses GLiNER's sigmoid logits ~0.18, costing recall on "
               "multilingual legal/clinical text — this column quantifies it. |")
    out.append("| `presidio` | Microsoft Presidio (spaCy NER + regex) | "
               "~1 GB | not required | 3-10 s | well-formed English "
               "(strong on EN newswire-shaped text where spaCy was trained) |")
    out.append("| `gliner-py` | GLiNER via PyTorch + safetensors (FP32) | "
               "~3 GB | not required | 10-30 s | reference implementation; "
               "parity check vs anonde-gliner's INT8 ONNX path |")
    out.append("")

    # ---- Domain × language coverage map -----------------------------
    # The matrix now spans ~30 corpora across 6 domains and 5 languages.
    # This grid is the table of contents: each cell lists the corpora
    # that exist for that (domain, language), so a missing cell ("·") is
    # explicit rather than silently absent. The detailed metric sections
    # below follow this same domain → language grouping.
    domains_seen: list[str] = []
    for d, _lang, _cs in groups:
        if d not in domains_seen:
            domains_seen.append(d)
    langs_seen: list[str] = []
    for _d, lang, _cs in groups:
        if lang not in langs_seen:
            langs_seen.append(lang)
    cell_corpora: dict[tuple[str, str], list[str]] = {
        (d, lang): cs for d, lang, cs in groups
    }
    out.append("## Coverage map · domain × language\n")
    out.append("Which corpora populate each `(domain, language)` cell. `·` = no "
               "corpus wired for that combination yet. The metric sections below "
               "are grouped on these same two axes.\n")
    out.append("| Domain | " + " | ".join(_language_name(lang) for lang in langs_seen) + " |")
    out.append("|---|" + "---|" * len(langs_seen))
    for d in domains_seen:
        cells = [f"| **{_domain_name(d)}** |"]
        for lang in langs_seen:
            cs = cell_corpora.get((d, lang))
            cells.append((", ".join(f"`{c}`" for c in cs) if cs else "·") + " |")
        out.append(" ".join(cells))
    out.append("")

    # ---- per (domain × language) metric sections --------------------
    # Each section carries the same card content the old flat report
    # had — verdict cards, raw + severity-weighted leak rate, latency,
    # strict F1 — but scoped to one (domain, language) so the report is
    # navigable. Leak rate leads every section: it is the load-bearing
    # metric for a redactor.
    out.append("# Detailed breakdown\n")
    out.append(
        "Everything below is reference detail behind the scorecard. Each "
        "`(domain × language)` section carries the per-corpus verdict "
        "cards, the raw leak-rate grid, the severity-weighted leak rate, "
        "latency, and strict F1. The scorecard above is the answer; "
        "these tables are the working.\n")

    sev = label_map.get("severity") or {}
    section_corpora_global: set[str] = set()
    for domain, language, section_corpora in groups:
        section_corpora_global.update(section_corpora)
        heading = f"{_domain_name(domain)} · {_language_name(language)}"
        out.append(f"## {heading}\n")
        out.append(f"Corpora in this group: "
                   + ", ".join(f"`{c}`" for c in section_corpora) + ".\n")

        # Verdict cards — production-engine leak severity flags.
        out.append("### Verdict\n")
        out.append("`🟢/🟡/🟠/🔴/💀` flags `anonde-gliner`'s leak severity. "
                   "`⚪` corpora produced text but no span-level gold (F1/leak "
                   "not measurable); `❔` = `anonde-gliner` did not run.\n")
        _verdict_cards(out, per_corpus_verdict, set(section_corpora))

        # Leak rate — the load-bearing metric, leads the section.
        out.append("### Leak rate · lower is better\n")
        out.append("A gold PHI span is *leaked* when **no** predicted span overlaps it "
                   "— 'did we miss a name?'\n")
        _leak_grid(out, rows, section_corpora, engines)

        # Partial-coverage footnote, scoped to this section's cells.
        # An engine run on a deterministic subsample (openai-pf via
        # --max-docs) only scored some gold docs; its metrics here are
        # over those docs only — flag it so a 40-doc sample is not read
        # as a full-corpus number.
        coverage_notes: list[str] = []
        for c in section_corpora:
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

        # Severity-weighted leak rate — procurement metric.
        if sev:
            out.append("### Severity-weighted leak rate · lower is better\n")
            out.append("Each leaked span weighted by compliance tier — direct "
                       "identifiers (PERSON, EMAIL, PHONE, ADDRESS, DOB) = 5, "
                       "high-stakes IDs (SSN/MRN/IBAN) = 10, quasi-identifiers "
                       "(LOCATION, ORG, PROFESSION) = 1. Defaults in "
                       "`label_map.yaml::severity`.\n")
            _weighted_leak_grid(out, rows, section_corpora, engines)

        # Latency — operational metric.
        out.append("### Latency · per-document p50 / p95\n")
        out.append("Wall-clock per `engine.Analyze(doc)` call. p50 = steady-state, "
                   "p95 = tail (the SLO knob). Mean + p99 in `results_matrix.csv`.\n")
        _latency_grid(out, rows, section_corpora, engines)

        # Strict F1 — academic-comparison metric.
        out.append("### Strict F1 · CoNLL exact span + type\n")
        out.append("Predicted span counts only on an exact `(start, end, type)` match "
                   "after label normalisation. Useful for academic comparison, less so "
                   "as a production metric (it penalises broader-or-narrower spans that "
                   "still redact the PHI). Per-entity-type breakdown in "
                   "`results_matrix.csv`.\n")
        _strict_f1_grid(out, rows, section_corpora, engines)

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
  NER even though it's invisible on clean German clinical text. The
  `anonde-gliner-fp32` column makes this explicit: it is the same
  model loaded from the FP32 ONNX (`model.onnx`), so the gap
  between the `anonde-gliner` and `anonde-gliner-fp32` columns
  isolates the INT8-quantization cost from everything else.

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
- **`–` cells** = engine not run on that corpus. Reasons: the matching spaCy / model assets
  weren't installed on the runner, or the corpus requires manual DUA registration (`ggponc_de`)
  or is loader-gated (`conll2003_de`).
- **Partial coverage** = an engine scored on a deterministic subsample, not the full corpus.
  `openai-pf` is ~80 s/doc on CPU, so it is benchmarked on the first N docs (sorted by id) —
  see the per-section "Partial coverage" footnote under the leak-rate grid. Its metrics are
  computed over only the docs it scored, so they are comparable in *kind* but not on the same
  doc population.
- **⚪ corpora** = precision-probe only (no span-level gold annotations). Useful for "does the
  engine over-redact ordinary prose?" checks, not for F1 / leak rate.
- **Domain / language grouping** = the report is organised by domain (clinical, legal, finance,
  logs, general PII, academic NER, adversarial) and language within each. The mapping lives in
  `bench/scoring/corpora.yaml`; a corpus missing from it renders under an `uncategorized` group.
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
    # corpora.yaml supplies each corpus's (domain, language) so the report
    # can group by domain → language. Optional with a default: when the
    # flag is omitted it resolves to corpora.yaml next to this script, so
    # the existing Makefile / CI invocations keep working unchanged. A
    # missing file is tolerated (every corpus falls back to uncategorized).
    ap.add_argument("--corpora-meta",
                    default=str(Path(__file__).parent / "corpora.yaml"),
                    help="corpus domain/language metadata "
                         "(default: corpora.yaml beside this script)")
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

    meta = _load_corpora_meta(Path(args.corpora_meta))
    # One consolidated warning for every corpus missing from corpora.yaml,
    # rather than crashing — they still render, just under 'uncategorized'.
    _groups, unclassified = _group_corpora(corpora, meta)
    if unclassified:
        print(f"warn: {len(unclassified)} corpus/corpora not in "
              f"{args.corpora_meta} — rendering under '{UNCATEGORIZED_DOMAIN}': "
              f"{', '.join(unclassified)}", file=sys.stderr)

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

    Path(args.out).write_text(
        _render(rows, label_map, corpora, engines, meta), encoding="utf-8")
    print(f"wrote {args.out}", file=sys.stderr)

    if args.csv:
        with open(args.csv, "w", newline="", encoding="utf-8") as fh:
            w = csv.writer(fh)
            # `domain` / `language` lead the row so downstream analysis can
            # pivot on the same axes the report groups by. The remaining
            # columns are unchanged — existing consumers that index by name
            # keep working; index-by-position consumers must shift by two.
            w.writerow(["domain", "language", "corpus", "engine", "view",
                        "entity", "tp", "fp", "fn",
                        "precision", "recall", "f1"])
            for (c, e), res in rows.items():
                domain, language = _corpus_meta(c, meta)
                for view in ("strict", "partial", "type_only"):
                    for t, (tp, fp, fn) in res[view].items():
                        p, r, f = _prf(tp, fp, fn)
                        w.writerow([domain, language, c, e, view, t,
                                    tp, fp, fn,
                                    f"{p:.4f}", f"{r:.4f}", f"{f:.4f}"])
        print(f"wrote {args.csv}", file=sys.stderr)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
