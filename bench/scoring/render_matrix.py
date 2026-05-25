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

The emitted REPORT_MATRIX.md LEADS with one *roll-ups-only* scorecard,
then keeps the detailed grids below it as reference:

  * TL;DR headline
  * Scorecard — one compact table at most 13 rows: **Σ ALL** + one row
    per domain (Σ across all languages in that domain) + one row per
    language (Σ across all domains in that language). Anonde-anchored
    on `anonde-gliner-fp32` (the production engine), with a win/loss
    tally. This is the table a human reads to answer "does anonde beat
    presidio overall?" in five seconds. Per-(domain × language) detail
    moves into the Detailed breakdown below.
  * Engine profiles (tier framing for anonde-patterns / anonde-gliner-fp32)
  * Domain × language coverage map (which cells exist)
  * "# Detailed breakdown" — leads with the dense per-(domain × language)
    leak-rate grid (the rows demoted off the scorecard), then per
    (domain × language) section with the raw leak-rate grid (and a
    severity-weighted grid only when it diverges >3pp from raw — see
    `_section_weighted_diverges`). One global latency table sits after
    the per-section blocks; per-section strict-F1 and per-section
    latency moved out of the markdown but stay in `results_matrix.csv`.
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

    Rows whose every cell is `–` (no scorable gold for any engine) are
    elided: they convey no information, and a corpus with no span-level
    gold is already flagged by the coverage map and the (now-removed)
    verdict cards. The latency grid still surfaces them globally because
    latency is meaningful even without gold.
    """
    out.append("| Corpus | " + " | ".join(f"`{e}`" for e in engines) + " |")
    out.append("|---|" + "---:|" * len(engines))
    for c in corpora:
        scorable_cell = next((rows[(c, e)] for e in engines
                              if (c, e) in rows and rows[(c, e)]["total_gold"] > 0), None)
        if scorable_cell is None:
            # All-dash row: every engine is unscoreable here. Elide rather
            # than print a noise row.
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
    """Append a severity-weighted leak-rate grid for one section.

    Rows that are all `–` (no scorable weighted gold for any engine) are
    elided — same rationale as `_leak_grid`.
    """
    out.append("| Corpus | " + " | ".join(f"`{e}`" for e in engines) + " |")
    out.append("|---|" + "---:|" * len(engines))
    for c in corpora:
        scorable_cell = next((rows[(c, e)] for e in engines
                              if (c, e) in rows and rows[(c, e)]["total_gold_weighted"] > 0), None)
        if scorable_cell is None:
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
    """Append a p50 / p95 per-document latency grid.

    Now rendered ONCE globally (at the end of the Detailed breakdown)
    rather than per-(domain × language) section: latency varies with
    corpus length, not with the domain/language axes, so 24 per-section
    copies were duplication. The global call passes the full corpus
    list; rows whose every engine has no recorded duration are elided.
    """
    out.append("| Corpus | " + " | ".join(f"`{e}` p50 / p95" for e in engines) + " |")
    out.append("|---|" + "---:|" * len(engines))
    for c in corpora:
        # Skip rows that would be all dashes — no engine recorded any
        # duration for this corpus. (Realistically a corpus skipped by
        # every engine; latency itself is the metric where most rows
        # have at least one populated cell.)
        if not any(rows.get((c, e)) and rows[(c, e)]["durations"] for e in engines):
            continue
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


def _section_weighted_diverges(rows: dict, corpora: list[str],
                               engines: list[str],
                               threshold_pp: float = 3.0) -> bool:
    """True iff the severity-weighted leak rate diverges from the raw
    leak rate by more than `threshold_pp` percentage points on at least
    one (corpus, engine) cell in this section.

    The weighted grid is otherwise a near-duplicate of the raw grid
    (typical divergence is 1-2pp, e.g. openmed 13.4% raw vs 13.3%
    weighted). Rendering it unconditionally was visual noise; render
    only when it actually says something different.
    """
    thr = threshold_pp / 100.0
    for c in corpora:
        for e in engines:
            cell = rows.get((c, e))
            if cell is None:
                continue
            if cell["total_gold"] == 0 or cell["total_gold_weighted"] == 0:
                continue
            raw = cell["leaked"] / cell["total_gold"]
            wtd = cell["leaked_weighted"] / cell["total_gold_weighted"]
            if abs(wtd - raw) > thr:
                return True
    return False


def _section_has_scorable_leak(rows: dict, corpora: list[str],
                               engines: list[str]) -> bool:
    """True iff at least one (corpus, engine) cell in this section has
    scorable gold for the raw leak grid. Used to skip whole sections
    where every row would be `–` (e.g. a corpus group of only
    precision-probe corpora).
    """
    for c in corpora:
        for e in engines:
            cell = rows.get((c, e))
            if cell is not None and cell["total_gold"] > 0:
                return True
    return False


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
# The single roll-ups-only table a human reads first. Rows = Σ ALL +
# per-domain Σ + per-language Σ (at most 13 rows total); columns = the
# engines; cell = leak rate %. anonde-gliner-fp32 is the production
# engine, so its column is the anchor: every row tells you at a glance
# whether anonde wins or loses on that domain / language slice. The
# per-(domain × language) detail rows live in the Detailed breakdown
# below the scorecard.

# The anonde column the scorecard anchors on (production engine). The
# win/verdict logic stays keyed on this one engine: `anonde-gliner-fp32`
# is what actually ships (FP32 ONNX), so "does anonde beat the field?"
# is answered for the production build. `anonde-gliner` (INT8) is kept
# as a legacy reference column for regression tracking, not the anchor.
SCORECARD_ANCHOR = "anonde-gliner-fp32"

# Anonde engine columns pinned to the FRONT of the scorecard, in this
# order, so the three GLiNER variants render side by side: production
# FP32 first (the verdict anchor), then INT8 (legacy / memory-constrained
# deployments), then the LARGE FP32 variant (3-4x parameters, probing
# whether scaling closes the remaining Romance-language cells). Engines
# here that are absent from a given run are skipped silently.
SCORECARD_FRONT = ["anonde-gliner-fp32", "anonde-gliner", "anonde-gliner-large"]


def _is_rival(engine: str) -> bool:
    """True for engines that count as a *baseline* in the anonde verdict.

    The verdict answers "does the production engine beat the competing
    field?" — so every `anonde-*` engine is excluded. In particular
    `anonde-gliner` (INT8 legacy) and `anonde-gliner-large` are the same
    GLiNER PII family as the production `anonde-gliner-fp32`, just a
    different quantization or model size: they are tracked reference
    columns, not competitors, and must not flip a ✅ to ❌.
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
    """Append the headline scorecard — roll-ups only.

    13 rows max: **Σ ALL** + one row per domain (Σ across all languages
    in that domain) + one row per language (Σ across all domains in that
    language). The per-(domain × language) detail rows that used to live
    here move into the Detailed breakdown below. Anonde-anchored on the
    production engine (`anonde-gliner-fp32`); a win/loss tally beneath
    the table summarises the per-cell verdicts counted from the
    underlying (domain × language) population.

    `groups` is the output of `_group_corpora` — (domain, language,
    corpus_list) in display order; we read it to collect the per-domain
    and per-language corpus lists for the pooled rates.
    """
    anchor = SCORECARD_ANCHOR
    # Column order: the anonde engine columns first, pinned in
    # SCORECARD_FRONT order so the three GLiNER variants render adjacent
    # (FP32 production → INT8 legacy → LARGE). The anchor is the FP32
    # production engine — every row's verdict is judged against it. The
    # remaining engines follow in the order they were requested.
    front = [e for e in SCORECARD_FRONT if e in engines]
    others = [e for e in engines if e not in front]
    col_engines = front + others

    # Collect per-domain / per-language / overall corpus lists for the
    # pooled roll-up rows.
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

    out.append("## 🎯 Scorecard · leak rate roll-ups\n")
    out.append(
        "The one table. Roll-up rows only (per domain · per language · "
        "overall); the per-(domain × language) detail grid lives in the "
        "Detailed breakdown below. Each number is **leak rate** (fraction "
        f"of gold PHI spans missed — lower is better). `{anchor}` is the "
        "anonde production engine (FP32 ONNX) and the anchor column; "
        "**Verdict** says whether it beats the field. `anonde-gliner` is "
        "the same model at INT8 quantization (kept as a legacy / memory-"
        "constrained reference); `anonde-gliner-large` is the 3-4x-larger "
        "GLiNER PII variant at FP32 (probing whether scale closes the "
        "remaining Romance-language cells). Both sit beside the anchor so "
        "the quantization and scale tradeoffs are visible at a glance, "
        "but the verdict is keyed on production. 🥇 marks the lowest-leak "
        "engine in the row. Roll-up rows pool leaked-over-gold across "
        "the group (doc-weighted, so larger corpora count more).\n")

    # ---- header --------------------------------------------------------
    header = "| Slice | Scope |"
    for e in col_engines:
        if e == anchor:
            tag = " ⬅︎ anonde (FP32, prod)"
        elif e == "anonde-gliner":
            tag = " · anonde (INT8, legacy)"
        elif e == "anonde-gliner-large":
            tag = " · anonde (LARGE)"
        else:
            tag = ""
        header += f" `{e}`{tag} |"
    header += " Verdict |"
    out.append(header)
    out.append("|---|---|" + "---:|" * len(col_engines) + ":--:|")

    def _emit_row(label_left: str, label_right: str, corpora: list[str],
                  bold_row: bool = False) -> None:
        """Render one roll-up row (per-domain, per-language, or overall)."""
        rates = [_group_leak(rows, corpora, e) for e in col_engines]
        scorable = [r for r in rates if r is not None]
        if not scorable:
            return
        best = min(scorable)
        anchor_rate = (rates[col_engines.index(anchor)]
                       if anchor in col_engines else None)
        verdict = _anchor_verdict(
            anchor_rate,
            [r for e, r in zip(col_engines, rates) if _is_rival(e)])
        row = f"| {label_left} | {label_right} |"
        for r in rates:
            if r is None:
                row += " – |"
                continue
            is_best = abs(r - best) < 1e-9
            if bold_row:
                # The overall Σ ALL row reads bold without double-
                # starring the winner into literal `****`.
                if is_best:
                    row += f" {_fmt_rate(r, True)} |"
                else:
                    row += f" **{_fmt_rate(r, False)}** |"
            else:
                row += f" {_fmt_rate(r, is_best)} |"
        row += f" {verdict} |"
        out.append(row)

    # ---- Σ ALL (overall) ----------------------------------------------
    # The one row a reader looks at if they only look at one row.
    _emit_row("**Σ ALL**", "**all**", all_corpora, bold_row=True)
    # Visual spacer between the headline overall row and the slice rows.
    out.append("|" + " |" * (len(col_engines) + 3))

    # ---- per-domain Σ -------------------------------------------------
    for domain in domain_seq:
        _emit_row(f"_Σ {domain_name(domain)}_", "_all langs_", by_domain[domain])

    # Visual spacer between the per-domain and per-language groups.
    out.append("|" + " |" * (len(col_engines) + 3))

    # ---- per-language Σ -----------------------------------------------
    for language in lang_seq:
        _emit_row("_Σ all domains_", f"_{language_name(language)}_",
                  by_language[language])

    out.append("")

    # ---- win/loss tally for the anonde anchor -------------------------
    # Counted over the underlying per-(domain × language) cells (not the
    # roll-ups above), so it answers "in how many domain × language
    # cells does production anonde lead the field?". The per-cell grid
    # itself lives in the Detailed breakdown.
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
        f"`(domain, language)` cells in the matrix, `{anchor}` is the "
        f"**lowest-leak engine in {wins}**, ties in **{ties}**, and is "
        f"beaten in **{losses}**. ✅ = anonde leads · 🟰 = tied · ❌ = a "
        "baseline leaks less. See the per-cell leak-rate grid in the "
        "Detailed breakdown below for which baseline wins where. (The "
        "TL;DR's win count is per-corpus, a finer split than these "
        "per-cell rows.)\n")


def _per_cell_leak_grid(out: list[str], rows: dict, groups: list,
                        engines: list[str],
                        domain_name, language_name) -> None:
    """Append a dense (domain × language) leak-rate grid — one row per
    populated cell.

    This is the table that used to sit in the scorecard above; the new
    13-row scorecard keeps only the Σ ALL + per-domain + per-language
    roll-ups. The detail belongs here, immediately under the Detailed
    breakdown heading, so a reader who wants to see which baseline
    actually beats anonde on a specific (domain, language) has one table
    to scan instead of walking every per-section block.

    `groups` and the column ordering match the scorecard exactly: the
    anonde columns (SCORECARD_FRONT) lead, anchor first, then the
    remaining engines in request order, then the Verdict column.
    """
    anchor = SCORECARD_ANCHOR
    front = [e for e in SCORECARD_FRONT if e in engines]
    others = [e for e in engines if e not in front]
    col_engines = front + others

    out.append("## Per-cell leak rate · domain × language\n")
    out.append(
        "Detail behind the scorecard roll-ups: one row per populated "
        "`(domain, language)` cell. Same columns, same anchor, same "
        "verdict glyph — read this to see *which* baseline wins where. "
        "Pooled leak rate across the cell's corpora.\n")

    header = "| Domain | Language |"
    for e in col_engines:
        if e == anchor:
            tag = " ⬅︎ anonde (FP32, prod)"
        elif e == "anonde-gliner":
            tag = " · anonde (INT8, legacy)"
        elif e == "anonde-gliner-large":
            tag = " · anonde (LARGE)"
        else:
            tag = ""
        header += f" `{e}`{tag} |"
    header += " Verdict |"
    out.append(header)
    out.append("|---|---|" + "---:|" * len(col_engines) + ":--:|")

    for domain, language, corpora in groups:
        rates = [_group_leak(rows, corpora, e) for e in col_engines]
        scorable = [r for r in rates if r is not None]
        if not scorable:
            # No scorable engine for this cell — still show the row so
            # the coverage map and this grid agree.
            row = f"| **{domain_name(domain)}** | {language_name(language)} |"
            row += " – |" * len(col_engines) + " – |"
            out.append(row)
            continue
        best = min(scorable)
        anchor_rate = (rates[col_engines.index(anchor)]
                       if anchor in col_engines else None)
        verdict = _anchor_verdict(
            anchor_rate,
            [r for e, r in zip(col_engines, rates) if _is_rival(e)])
        row = f"| **{domain_name(domain)}** | {language_name(language)} |"
        for r in rates:
            is_best = r is not None and abs(r - best) < 1e-9
            row += f" {_fmt_rate(r, is_best)} |"
        row += f" {verdict} |"
        out.append(row)
    out.append("")


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
            out.append(f"- ❔ **`{c}`** — `{SCORECARD_ANCHOR}` did not run on this corpus.")
            continue
        gliner_rate = gliner_row[1]
        flag = _fmt_leak_bar(gliner_rate)
        line = f"- {flag} **`{c}`** — `{SCORECARD_ANCHOR}` leaks **{gliner_rate:.1%}**"
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

    Layout — lightest-touch trim of the verbose pre-2026-05-26 report.
    Strict-F1 (per-section) and per-section latency have moved out of
    the markdown but stay intact in `results_matrix.csv`:

      1. TL;DR (one-paragraph headline conclusion)
      2. Scorecard — THE table: 13 rows max (Σ ALL + per-domain Σ +
         per-language Σ), leak rate per engine, anonde-anchored on the
         FP32 production engine, plus a win/loss tally
      3. Engine profiles (tier framing, wrapped in collapsed <details>)
      4. Domain × language coverage map
      5. "# Detailed breakdown":
         5a. Per-cell leak-rate grid (one row per (domain × language)
             cell — the detail demoted off the scorecard)
         5b. Per (domain × language) section, in display order, each
             with: raw leak-rate grid (always), partial-coverage
             footnote (if any cell was subsampled), severity-weighted
             leak rate (only when it diverges >3pp from raw on at least
             one cell). Verdict cards and per-section strict-F1 /
             latency tables removed — they were restatement and
             noise-vs-signal duplicates of the per-cell grid / global
             latency table.
         5c. ONE global latency p50 / p95 table — latency tracks corpus
             length, not the (domain, language) axes, so 24 per-section
             copies were collapsed into one.
      6. Cost reference (collapsed <details>; static)
      7. Caveats — training-data overlap (collapsed <details>; static)
      8. Glossary (collapsed <details>; static)
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
        # gliner_row = the production engine (FP32); best_baseline = the
        # best NON-anonde engine. The INT8 legacy column and the LARGE
        # variant are tracked reference columns, not competing baselines,
        # so `_is_rival` excludes them here exactly as in the scorecard
        # verdict. (The card would otherwise quote one of them as "the
        # best baseline", which is misleading.)
        gliner_row = next((x for x in engine_leaks
                           if x[0] == SCORECARD_ANCHOR), None)
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
    # A "win" = the production engine (`anonde-gliner-fp32`, FP32 ONNX)
    # leaks no more than every NON-anonde baseline on that corpus.
    # Counted against rivals only — the INT8 legacy column and the LARGE
    # variant are the same GLiNER PII family, so they never cost
    # production a win (mirrors the scorecard verdict's `_is_rival` rule).
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
            f"> **TL;DR** — `anonde-gliner-fp32` (production, FP32 ONNX) is the "
            f"lowest-leak engine on **{gliner_wins} of {n_scorable}** gold-annotated "
            f"corpora. Biggest absolute improvement over the best baseline: "
            f"**{biggest_pp * 100:+.1f}pp** in leak rate. The `anonde-gliner` column is "
            f"the same model at INT8 (legacy / memory-constrained reference); "
            f"`anonde-gliner-large` is the 3-4x-larger GLiNER PII variant at FP32 "
            f"(scaling probe). Neither is counted as a competitor — both are anonde "
            f"reference columns. Strict F1 trades exact-byte alignment for catching "
            f"more PHI — the right trade-off for a redactor, not a benchmark gaming "
            f"exercise.\n"
        )
        out.append(tldr)
    else:
        out.append(
            "> **TL;DR** — no gold-annotated corpora available to score. "
            "Add a corpus with `entities: [...]` in its `corpus.jsonl` to enable F1 + leak-rate metrics.\n"
        )

    # ---- Headline scorecard -----------------------------------------
    # The single scannable table: 13 rows (Σ ALL + per-domain Σ +
    # per-language Σ), anonde-anchored on FP32 production. The per-cell
    # detail moves into the Detailed breakdown below.
    _scorecard(out, rows, groups, engines, _domain_name, _language_name)

    # ---- Engine profiles --------------------------------------------
    # Anonde-patterns and anonde-gliner-fp32 are NOT two competing tools
    # — they're two deployment profiles of the same toolkit. Patterns is
    # the no-ML / no-CGO / 12 MB image baseline; gliner-fp32 is the
    # +770 MB ML-backed production stack. Wrapped in a collapsed
    # <details> block because the table is static between runs — a
    # reader who wants the tier framing can expand it, the default view
    # leads with the live numbers.
    out.append("<details><summary>Engine profiles · what each column means</summary>\n")
    out.append("## Engine profiles\n")
    out.append("Engines below are not all competitors. `anonde-patterns` and "
               "`anonde-gliner-fp32` are two deployment tiers of the same anonde "
               "binary; compare *across the row* for the trade-off, not against "
               "each other for a winner.\n")
    out.append("| Engine | Profile | Image | CGO | Cold start | Best fit |")
    out.append("|---|---|---|---|---|---|")
    out.append("| `anonde-patterns` | regex / no-ML baseline (anonde tier 1) | "
               "~12 MB | not required | <1 s | structured slot-gen text (forms, "
               "logs, finance/legal docs) — wins F1 on PHONE, EMAIL, DATE, "
               "PROFESSION when the regex shape is tight |")
    out.append("| `anonde-gliner-fp32` | GLiNER PII (FP32 ONNX, `model.onnx`) + "
               "patterns (anonde tier 2, **production**) | ~770 MB | required | "
               "5-30 s warmup | natural text + multilingual PHI; the lowest-leak "
               "engine on most gold corpora. Ships the FP32 ONNX. |")
    out.append("| `anonde-gliner` | same GLiNER PII model, INT8 ONNX "
               "(`model_quint8.onnx`) — legacy / memory-constrained reference | "
               "~530 MB | required | 5-30 s warmup | not a competitor: kept so "
               "the INT8-vs-FP32 quantization regression stays tracked. INT8 "
               "depresses GLiNER's sigmoid logits ~0.18, costing recall on "
               "multilingual legal/clinical text — this column quantifies it. |")
    out.append("| `anonde-gliner-large` | larger GLiNER PII variant "
               "(`knowledgator/gliner-pii-large-v1.0`, FP32) — reference column, "
               "not a separate tier | ~1.4 GB | required | 10-60 s warmup | not "
               "a competitor: scaling probe (3-4x parameters vs the production "
               "base) for the remaining Romance-language cells. |")
    out.append("| `presidio` | Microsoft Presidio (spaCy NER + regex) | "
               "~1 GB | not required | 3-10 s | well-formed English "
               "(strong on EN newswire-shaped text where spaCy was trained) |")
    out.append("| `gliner-py` | GLiNER via PyTorch + safetensors (FP32) | "
               "~3 GB | not required | 10-30 s | reference implementation; "
               "parity check vs anonde-gliner-fp32's ONNX path |")
    out.append("")
    out.append("</details>\n")

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
        "Everything below is reference detail behind the scorecard. The "
        "per-cell grid first (the detail demoted off the 13-row scorecard), "
        "then each `(domain × language)` section with its raw leak-rate "
        "grid (and severity-weighted leak only when it actually diverges "
        ">3pp from raw — otherwise the two tracked within noise). One "
        "global latency table follows. Strict-F1 and per-entity-type "
        "breakdowns live in `results_matrix.csv`. The scorecard above is "
        "the answer; these tables are the working.\n")

    # Per-cell leak-rate grid — the (domain × language) detail demoted
    # off the scorecard. One single table covering every populated cell,
    # so a reader can scan "where does anonde lose?" in one place.
    _per_cell_leak_grid(out, rows, groups, engines, _domain_name, _language_name)

    sev = label_map.get("severity") or {}
    section_corpora_global: set[str] = set()
    for domain, language, section_corpora in groups:
        section_corpora_global.update(section_corpora)
        # Auto-elide whole sections with no scorable gold anywhere. A
        # group whose only corpora are precision-probes (e.g. pmc_de /
        # wiki_de) would otherwise render an empty leak grid and an
        # empty heading; the coverage map already lists the corpora.
        if not _section_has_scorable_leak(rows, section_corpora, engines):
            continue
        heading = f"{_domain_name(domain)} · {_language_name(language)}"
        out.append(f"## {heading}\n")
        out.append(f"Corpora in this group: "
                   + ", ".join(f"`{c}`" for c in section_corpora) + ".\n")

        # Verdict cards removed — the per-cell leak-rate grid two lines
        # below already states the same verdict; the cards restated it.

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

        # Severity-weighted leak rate — procurement metric. Render only
        # when it actually diverges from raw leak by >3pp on at least
        # one cell; otherwise it's a near-duplicate table (e.g. openmed
        # 13.4% raw vs 13.3% weighted) that adds no signal. The metric
        # stays in `results_matrix.csv` regardless.
        if sev and _section_weighted_diverges(rows, section_corpora, engines):
            out.append("### Severity-weighted leak rate · lower is better\n")
            out.append("Each leaked span weighted by compliance tier — direct "
                       "identifiers (PERSON, EMAIL, PHONE, ADDRESS, DOB) = 5, "
                       "high-stakes IDs (SSN/MRN/IBAN) = 10, quasi-identifiers "
                       "(LOCATION, ORG, PROFESSION) = 1. Defaults in "
                       "`label_map.yaml::severity`. Shown only because at least "
                       "one cell here moves >3pp from raw leak; otherwise the "
                       "two tables tracked within noise.\n")
            _weighted_leak_grid(out, rows, section_corpora, engines)

        # Latency moved to a single global table after this loop —
        # latency varies with corpus length, not with the
        # (domain, language) axes, so a per-section copy was duplication.

        # Strict F1 removed from per-section blocks — the full per-cell
        # numbers (and the per-entity-type breakdown) stay in
        # `results_matrix.csv`. Strict F1 over-penalises spans that
        # redact the PHI but don't byte-align, so it's the wrong metric
        # to drive a redactor decision from anyway.

    # ---- Latency · single global table -----------------------------
    # One latency table for the whole report. Latency depends on
    # corpus length (token count per doc), not on the (domain,
    # language) axes, so the per-section copies were 24-way duplication.
    # The numbers themselves are unchanged; this is purely a layout
    # move. Rows are pooled across all sections in the order corpora
    # were requested.
    all_corpora_in_order: list[str] = []
    for _d, _l, cs in groups:
        for c in cs:
            if c not in all_corpora_in_order:
                all_corpora_in_order.append(c)
    out.append("## Latency · per-document p50 / p95\n")
    out.append("Wall-clock per `engine.Analyze(doc)` call. p50 = steady-state, "
               "p95 = tail (the SLO knob). Mean + p99 in `results_matrix.csv`. "
               "One table across every corpus — latency tracks corpus length, "
               "not domain or language.\n")
    _latency_grid(out, rows, all_corpora_in_order, engines)

    # ---- Cost reference ---------------------------------------------
    # All engines we benchmark are self-hostable, so per-cell cost columns
    # would be $0 across the board — uninteresting. Instead we anchor the
    # bench to managed-service market prices so a reader can compare what
    # they'd otherwise be paying. Prices are pinned with a "verified as of"
    # date; vendor pricing pages change, so re-verify before quoting.
    out.append("<details><summary>Cost reference · USD per million characters</summary>\n")
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
    out.append("</details>\n")

    # ---- Caveats / training-data biases ----------------------------
    # Some corpora are training-data-adjacent to the NLP backends we
    # bench. Calling that out keeps the matrix honest: a high score
    # on a corpus the engine was trained on is not the same as a
    # high score on a held-out one.
    out.append("<details><summary>Caveats — training-data overlap</summary>\n")
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
- **`conll2003_en` / `wnut_17` × `anonde-gliner` (INT8 legacy)** — the
  INT8-quantised ONNX (`model_quint8.onnx`, ~196 MB) consistently leaks
  more PII than the FP32 export (`model.onnx`) loaded by production
  `anonde-gliner-fp32` and the Python `gliner-py` reference. Quantization
  bites on noisy English NER and on multilingual legal / clinical text;
  the gap between the `anonde-gliner-fp32` and `anonde-gliner` columns
  isolates the INT8-quantization cost from everything else. The
  `anonde-gliner-large` column probes the orthogonal question: does
  scaling to a 3-4x-larger GLiNER PII variant (still FP32) close the
  remaining cells where production loses to a baseline.

Held-out corpora with no known overlap for any of the four engines
listed in this matrix: `openmed` (GraSCCo PHI), `synth_clinical`,
`finance_de`, `legal_de`, `adversarial_de`, `ai4privacy_en`,
`pharmaconer_es`. Numbers there transfer most cleanly.
""")
    out.append("</details>\n")

    # ---- Glossary ---------------------------------------------------
    out.append("<details><summary>What does this mean? (glossary)</summary>\n")
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
    out.append("</details>\n")

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
