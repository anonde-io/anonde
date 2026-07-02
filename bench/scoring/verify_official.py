#!/usr/bin/env python3
"""verify_official.py — independent cross-check of the anonde bench scorer.

Purpose
-------
``compare.py`` is anonde's own span-based scorer: entity-level **strict**
(exact start+end+type) and **partial** (overlap, same canonical type)
precision / recall / F1, plus a type-agnostic **leak rate**. That methodology
is the de-identification standard — but it is *our* implementation, so a
reviewer cannot yet confirm the numbers against a published tool. This script
re-scores the SAME ``(gold jsonl, engine-prediction jsonl)`` inputs with a
widely-cited external library and reports the delta, so the claim becomes:

    "anonde's strict/partial F1 reproduces <standard tool> within epsilon."

Methodology lineage
-------------------
* **i2b2 2014 / n2c2 2014 de-identification shared tasks** — the reference
  clinical de-id evaluation: span-based entity matching scored two ways,
  *strict* (exact offset + type) and *relaxed* (token/overlap), alongside a
  binary HIPAA recall (was each PHI token redacted at all). ``compare.py``'s
  strict, partial and leak-rate views mirror this split directly.
* **SemEval-2013 Task 9.1 / MUC-5 scoring** — the four span-evaluation schemes
  (strict, exact, partial, type) that generalise the i2b2 strict/relaxed
  split. These are exactly what the chosen library implements.
* **``nervaluate``** (chosen library) — a small, widely-cited, dependency-light
  Python package implementing the SemEval'13 / MUC span schemes directly over
  **character offsets**. Chosen over ``seqeval`` (CoNLL token-IOB entity F1)
  because our detectors emit char spans: seqeval would require a lossy
  char -> token -> IOB re-encoding (tokenizer choice + sub-token alignment)
  that introduces its own error term and defeats the point of an *independent*
  check. nervaluate consumes our ``{type,start,end}`` spans natively — no
  tokenization, no alignment.

Schema correspondence (what the cross-check maps):

    compare.py "strict"  (exact start+end+type)  <->  nervaluate "strict"
    compare.py "partial" (overlap, same type)    <->  nervaluate "ent_type"

nervaluate additionally reports ``exact`` (boundary-only) and ``partial``
(boundary-only, 0.5 partial credit); both are surfaced as extra citable
columns but have no compare.py counterpart.

Why the micro deltas should be ~0 (and per-type may wobble): both tools score
per canonical type over the same half-open ``[start, end)`` char offsets. On a
cross-type boundary overlap (a PERSON prediction landing on a LOCATION gold),
compare.py books one FP under PERSON and one FN under LOCATION, while
nervaluate books a single "incorrect" that lifts both the precision and the
recall denominator with no correct — algebraically the same shift on the
micro-average, but attributed to different type rows. The other epsilon source
is spans that *touch* (gold ``[5,10)`` immediately followed by pred
``[10,15)``): compare.py's half-open offsets treat these as non-overlapping,
but nervaluate (>= 1.0) builds an inclusive per-span index set and books them
as an overlap. This shifts only the ``ent_type`` / ``partial`` columns and only
where gold and predicted spans are exactly adjacent — never ``strict`` (which
compares boundaries exactly, invariant to the interpretation). The conversion
below is a deliberate *naive* char-offset pass-through so that an independent
reviewer who runs nervaluate the obvious way on our JSONL reproduces these
numbers; the residual is reported, not hidden.

Degrade / self-test
-------------------
nervaluate is an optional dep and is usually not installed (system Python is
PEP-668 externally-managed). This script therefore:

  * runs a synthetic **self-test** (``--selftest``) validating the
    char-span -> nervaluate-format conversion AND compare.py's own math against
    a hand-computed example, with **no external library required**; and
  * on a real run, prints the pip line and skips the external comparison
    cleanly if nervaluate is absent (compare.py's own numbers still print).

Usage
-----
    # once, to get the real cross-check numbers:
    pip install nervaluate

    python3 bench/scoring/verify_official.py \\
        --gold   bench/corpora/openmed/data/corpus.jsonl \\
        --engine anonde-ner=bench/corpora/openmed/data/anonde_anonde-ner.jsonl

    # conversion + math self-test, no external lib:
    python3 bench/scoring/verify_official.py --selftest

Or via make (opt-in, NOT part of the default matrix):
    make -C bench verify-official CORPUS=openmed ENGINE=anonde-ner
    make -C bench verify-official-selftest
"""

from __future__ import annotations

import argparse
import sys
from pathlib import Path

# compare.py is the single source of truth for label canonicalisation AND the
# strict/partial tallies we are cross-checking. Import it (module-level imports
# are side-effect free; main() is guarded) so this script never re-implements
# the math it is trying to verify.
sys.path.insert(0, str(Path(__file__).parent))
import compare  # noqa: E402

NERVALUATE_PIP = "pip install nervaluate"

# compare.py view  ->  nervaluate schema key. See module docstring.
VIEW_TO_NERVALUATE = {"strict": "strict", "partial": "ent_type"}
# All nervaluate schemes we read; the first two are the cross-checked pair, the
# last two are extra boundary-only citable columns with no compare.py analogue.
NERVALUATE_SCHEMAS = ("strict", "ent_type", "exact", "partial")


# --------------------------------------------------------------------------
# char-span -> nervaluate format
# --------------------------------------------------------------------------
def span_to_nervaluate(spans) -> list[dict]:
    """Convert a list of ``compare.Span`` into nervaluate entity dicts.

    nervaluate wants, per document, a list of ``{"label","start","end"}`` over
    the same integer offset space for gold and predictions. compare.py's Span
    already carries half-open ``[start, end)`` char offsets and a canonical
    type, so this is a deliberate pure pass-through (no re-basing, no
    tokenization, no ``end`` fudge). Passing raw offsets is what makes the
    result independently reproducible; nervaluate's inclusive-``end`` overlap
    treatment of exactly-adjacent spans is left as a reported ent_type/partial
    residual rather than corrected here (see module docstring). Keeping this a
    pure function is what the self-test pins.
    """
    return [{"label": s.type, "start": s.start, "end": s.end} for s in spans]


def _pmap_for(name: str, label_map: dict) -> dict:
    """Route an engine name to its label_map prediction section.

    Mirrors the prefix routing in ``compare.py::main`` verbatim so this script
    canonicalises predictions identically to the scorer it cross-checks.
    """
    if name.startswith("anonde"):
        return label_map.get("anonde", {}) or {}
    if name.startswith("openmed"):
        return label_map.get("openmed", {}) or {}
    if name.startswith("presidio"):
        return label_map.get("presidio", {}) or {}
    if name.startswith("gliner-py") or name.startswith("gliner_py"):
        return label_map.get("gliner-py", {}) or {}
    if name.startswith("openai-pf") or name.startswith("openai_pf"):
        return label_map.get("openai-pf", {}) or {}
    return label_map.get(name, {}) or {}


def build_nervaluate_docs(gold_docs, pred_docs, gmap, pmap, canonical_set, tags):
    """Canonicalise both sides via compare.py, filter to ``tags``, convert.

    Returns ``(scored_ids, true_docs, pred_docs_nv)`` where the two doc lists
    are aligned per-document nervaluate inputs. The scored-id set is the
    gold ∩ pred intersection — identical to ``compare._evaluate`` — so a
    subsampled engine (e.g. openai-pf via --max-docs) is scored over exactly
    the docs it returned, not penalised with phantom leaks on unscored docs.
    Spans are filtered to ``tags`` (the canonical PII types) so nervaluate sees
    the same span set that the compare.py micro-sum ranges over; OTHER-typed
    spans (severity 0, no gold) are excluded from both sides symmetrically.
    """
    tagset = set(tags)
    scored_ids = [doc_id for doc_id in gold_docs if doc_id in pred_docs]
    true_docs, pred_docs_nv = [], []
    for doc_id in scored_ids:
        g = compare._gold_spans(gold_docs[doc_id], gmap, canonical_set)
        pdoc = pred_docs.get(doc_id) or {"findings": []}
        p = compare._pred_spans(pdoc, pmap, canonical_set)
        g = [s for s in g if s.type in tagset]
        p = [s for s in p if s.type in tagset]
        true_docs.append(span_to_nervaluate(g))
        pred_docs_nv.append(span_to_nervaluate(p))
    return scored_ids, true_docs, pred_docs_nv


# --------------------------------------------------------------------------
# nervaluate readout (version-tolerant)
# --------------------------------------------------------------------------
def _bget(block, key, default=0):
    """Read a field from a nervaluate schema block regardless of shape.

    nervaluate >= 1.0 returns ``EvaluationResult`` objects (attribute access);
    the 0.x line returned plain dicts (key access). Support both.
    """
    if isinstance(block, dict):
        return block.get(key, default)
    return getattr(block, key, default)


def _bhas(block, key) -> bool:
    if isinstance(block, dict):
        return key in block
    return hasattr(block, key)


def _prf_from_schema(block, schema: str) -> tuple[float, float, float]:
    """Pull (P, R, F1) from one nervaluate schema block.

    Prefers the library-provided precision/recall/f1 if present; otherwise
    derives them from the SemEval counts (correct/partial/actual/possible) so
    the reader is robust across nervaluate versions that changed whether p/r/f1
    are precomputed. Only ``partial`` awards half credit; the schemes we
    cross-check (strict, ent_type) are full-credit.
    """
    if all(_bhas(block, k) for k in ("precision", "recall", "f1")):
        return (float(_bget(block, "precision")), float(_bget(block, "recall")),
                float(_bget(block, "f1")))
    cor = _bget(block, "correct")
    par = _bget(block, "partial")
    act = _bget(block, "actual")
    pos = _bget(block, "possible")
    num = cor + 0.5 * par if schema == "partial" else cor
    p = num / act if act else 0.0
    r = num / pos if pos else 0.0
    f = 2 * p * r / (p + r) if (p + r) else 0.0
    return p, r, f


def collect_nervaluate(overall: dict, per_tag: dict) -> dict:
    """Normalise nervaluate output to ``{schema: {"__micro__": prf, TYPE: prf}}``."""
    out = {s: {} for s in NERVALUATE_SCHEMAS}
    for s in NERVALUATE_SCHEMAS:
        if isinstance(overall, dict) and s in overall:
            out[s]["__micro__"] = _prf_from_schema(overall[s], s)
    for tag, blocks in (per_tag or {}).items():
        if not isinstance(blocks, dict):
            continue
        for s in NERVALUATE_SCHEMAS:
            if s in blocks:
                out[s][tag] = _prf_from_schema(blocks[s], s)
    return out


def run_nervaluate(true_docs, pred_docs_nv, tags):
    """Run nervaluate. Returns collected dict, or raises ImportError if absent."""
    from nervaluate import Evaluator  # optional heavy dep — guarded by caller

    evaluator = Evaluator(true_docs, pred_docs_nv, tags=list(tags))
    res = evaluator.evaluate()
    # evaluate()'s return shape changed across the library's life:
    #   >= 1.0 : dict {"overall": {schema: EvaluationResult},
    #                  "entities": {tag: {schema: EvaluationResult}}, ...}
    #   0.x    : tuple (overall_dict, per_tag_dict) — schema-keyed dicts
    #   (rare) : a bare overall dict keyed by schema
    # Normalise to (overall{schema:block}, per_tag{tag:{schema:block}}).
    if isinstance(res, dict) and "overall" in res:
        overall = res["overall"]
        per_tag = res.get("entities", {}) or {}
    elif isinstance(res, dict):
        overall, per_tag = res, {}
    else:
        overall = res[0]
        per_tag = res[1] if len(res) > 1 else {}
    return collect_nervaluate(overall, per_tag)


# --------------------------------------------------------------------------
# reporting
# --------------------------------------------------------------------------
def _sum_tally(tallies: dict, tags) -> list[int]:
    tp = fp = fn = 0
    for t in tags:
        a, b, c = tallies.get(t, [0, 0, 0])
        tp += a; fp += b; fn += c
    return [tp, fp, fn]


def _shown_tags(cmp_strict, cmp_partial, canonical) -> list[str]:
    """Canonical types with any signal in either view (keeps the table tight)."""
    out = []
    for t in canonical:
        s = cmp_strict.get(t, [0, 0, 0])
        p = cmp_partial.get(t, [0, 0, 0])
        if sum(s) or sum(p):
            out.append(t)
    return out


def build_report(engine, n_gold, n_scored, canonical, cmp_strict, cmp_partial,
                 nv, have_nv) -> str:
    tags = _shown_tags(cmp_strict, cmp_partial, canonical)
    micro_strict = _sum_tally(cmp_strict, canonical)
    micro_partial = _sum_tally(cmp_partial, canonical)

    lines: list[str] = []
    lines.append(f"# Independent scorer cross-check — engine `{engine}`\n")
    lines.append(f"Scored **{n_scored}** of {n_gold} gold docs "
                 f"(gold ∩ prediction intersection). Cross-check over the "
                 f"canonical PII types (OTHER excluded — severity 0, no gold).\n")
    lines.append("Schema map: compare.py **strict** ↔ nervaluate **strict**; "
                 "compare.py **partial** ↔ nervaluate **ent_type**.\n")

    # --- reproducibility table (the headline) ---
    if have_nv:
        lines.append("## Reproducibility (our F1 vs standard-library F1)\n")
        lines.append("| Entity | our strict F1 | nervaluate strict F1 | Δ | "
                     "our partial F1 | nervaluate ent_type F1 | Δ |")
        lines.append("|---|---:|---:|---:|---:|---:|---:|")

        def row(label, s_tally, p_tally, key):
            _, _, our_s = compare._prf(*s_tally)
            _, _, our_p = compare._prf(*p_tally)
            nv_s = nv["strict"].get(key, (0.0, 0.0, 0.0))[2]
            nv_p = nv["ent_type"].get(key, (0.0, 0.0, 0.0))[2]
            return (f"| {label} | {our_s:.4f} | {nv_s:.4f} | {our_s - nv_s:+.4f} "
                    f"| {our_p:.4f} | {nv_p:.4f} | {our_p - nv_p:+.4f} |")

        for t in tags:
            lines.append(row(t, cmp_strict.get(t, [0, 0, 0]),
                             cmp_partial.get(t, [0, 0, 0]), t))
        lines.append(row("**MICRO**", micro_strict, micro_partial, "__micro__"))
        lines.append("")

        max_delta = 0.0
        for t in tags:
            _, _, our_s = compare._prf(*cmp_strict.get(t, [0, 0, 0]))
            _, _, our_p = compare._prf(*cmp_partial.get(t, [0, 0, 0]))
            nv_s = nv["strict"].get(t, (0.0, 0.0, 0.0))[2]
            nv_p = nv["ent_type"].get(t, (0.0, 0.0, 0.0))[2]
            max_delta = max(max_delta, abs(our_s - nv_s), abs(our_p - nv_p))
        _, _, m_our_s = compare._prf(*micro_strict)
        _, _, m_our_p = compare._prf(*micro_partial)
        m_nv_s = nv["strict"].get("__micro__", (0.0, 0.0, 0.0))[2]
        m_nv_p = nv["ent_type"].get("__micro__", (0.0, 0.0, 0.0))[2]
        lines.append(
            f"_Max |Δ F1| across shown per-type rows: **{max_delta:.4f}**. "
            f"Micro strict Δ = {m_our_s - m_nv_s:+.4f}, "
            f"micro partial Δ = {m_our_p - m_nv_p:+.4f}._\n")

        # --- citable nervaluate P/R/F1 ---
        lines.append("## nervaluate P/R/F1 (citable)\n")
        lines.append("| Entity | strict P | strict R | strict F1 | "
                     "ent_type P | ent_type R | ent_type F1 |")
        lines.append("|---|---:|---:|---:|---:|---:|---:|")
        for t in tags + ["__micro__"]:
            sp, sr, sf = nv["strict"].get(t, (0.0, 0.0, 0.0))
            tp_, tr, tf = nv["ent_type"].get(t, (0.0, 0.0, 0.0))
            label = "**MICRO**" if t == "__micro__" else t
            lines.append(f"| {label} | {sp:.4f} | {sr:.4f} | {sf:.4f} "
                         f"| {tp_:.4f} | {tr:.4f} | {tf:.4f} |")
        lines.append("")
        ex = nv["exact"].get("__micro__", (0.0, 0.0, 0.0))
        pa = nv["partial"].get("__micro__", (0.0, 0.0, 0.0))
        lines.append(f"_Boundary-only micro (no compare.py analogue): "
                     f"nervaluate exact F1 = {ex[2]:.4f}, "
                     f"partial F1 = {pa[2]:.4f}._\n")

    # --- compare.py reference (always) ---
    lines.append("## anonde compare.py P/R/F1 (reference)\n")
    lines.append("| Entity | strict P | strict R | strict F1 | "
                 "partial P | partial R | partial F1 |")
    lines.append("|---|---:|---:|---:|---:|---:|---:|")
    for t in tags:
        sp, sr, sf = compare._prf(*cmp_strict.get(t, [0, 0, 0]))
        pp, pr, pf = compare._prf(*cmp_partial.get(t, [0, 0, 0]))
        lines.append(f"| {t} | {sp:.4f} | {sr:.4f} | {sf:.4f} "
                     f"| {pp:.4f} | {pr:.4f} | {pf:.4f} |")
    sp, sr, sf = compare._prf(*micro_strict)
    pp, pr, pf = compare._prf(*micro_partial)
    lines.append(f"| **MICRO** | {sp:.4f} | {sr:.4f} | {sf:.4f} "
                 f"| {pp:.4f} | {pr:.4f} | {pf:.4f} |")
    lines.append("")

    if not have_nv:
        lines.append("## nervaluate not installed — cross-check skipped\n")
        lines.append("Only anonde's own numbers are shown above. To produce the "
                     "independent cross-check + reproducibility table, install "
                     "the library and re-run:\n")
        lines.append(f"    {NERVALUATE_PIP}\n")
    return "\n".join(lines)


# --------------------------------------------------------------------------
# self-test (no external library required)
# --------------------------------------------------------------------------
# Hand-built single-doc example. Half-open [start,end) char offsets; no
# cross-type overlaps and no touching-but-non-overlapping spans, so the
# expected tallies are unambiguous under either overlap convention.
#
#   GOLD:  PERSON[0,5)  PERSON[10,15)  LOCATION[20,28)  DATE[30,40)
#   PRED:  PERSON[0,5)  PERSON[11,16)  LOCATION[50,55)
#
#   PERSON[0,5)   exact-matches GOLD PERSON[0,5)     -> strict TP, partial TP
#   PERSON[11,16) overlaps GOLD PERSON[10,15)        -> strict FP, partial TP
#   LOCATION[50,55) overlaps no gold                 -> strict FP, partial FP
#   GOLD LOCATION[20,28) unmatched                   -> strict FN, partial FN
#   GOLD DATE[30,40)     unmatched                   -> strict FN, partial FN
#
# Hand-computed micro (summed over canonical types):
#   strict  = tp 1, fp 2, fn 3  -> P 1/3, R 1/4, F1 2/7 ≈ 0.285714
#   partial = tp 2, fp 1, fn 2  -> P 2/3, R 1/2, F1 4/7 ≈ 0.571429
def _selftest_spans():
    Span = compare.Span
    gold = [Span(0, 5, "PERSON"), Span(10, 15, "PERSON"),
            Span(20, 28, "LOCATION"), Span(30, 40, "DATE")]
    pred = [Span(0, 5, "PERSON"), Span(11, 16, "PERSON"),
            Span(50, 55, "LOCATION")]
    return gold, pred


def test_span_to_nervaluate_conversion():
    """The char-span -> nervaluate conversion is an exact pass-through."""
    gold, pred = _selftest_spans()
    assert span_to_nervaluate(gold) == [
        {"label": "PERSON", "start": 0, "end": 5},
        {"label": "PERSON", "start": 10, "end": 15},
        {"label": "LOCATION", "start": 20, "end": 28},
        {"label": "DATE", "start": 30, "end": 40},
    ]
    assert span_to_nervaluate(pred) == [
        {"label": "PERSON", "start": 0, "end": 5},
        {"label": "PERSON", "start": 11, "end": 16},
        {"label": "LOCATION", "start": 50, "end": 55},
    ]


def test_compare_math_matches_hand_computed():
    """compare.py's own strict/partial tallies match the hand-computed values."""
    gold, pred = _selftest_spans()
    canonical = ["PERSON", "LOCATION", "DATE"]

    strict = compare._strict(gold, pred)
    partial = compare._partial(gold, pred)

    assert _sum_tally(strict, canonical) == [1, 2, 3], strict
    assert _sum_tally(partial, canonical) == [2, 1, 2], partial

    _, _, sf = compare._prf(*_sum_tally(strict, canonical))
    _, _, pf = compare._prf(*_sum_tally(partial, canonical))
    assert abs(sf - 2 / 7) < 1e-9, sf
    assert abs(pf - 4 / 7) < 1e-9, pf


def _test_nervaluate_agrees_when_present():
    """If nervaluate is installed, its micro strict/ent_type F1 must reproduce
    compare.py's micro on the hand example (this example has no cross-type or
    touching spans, so agreement is expected to be exact within fp tolerance).
    Skipped silently when the library is absent."""
    try:
        import nervaluate  # noqa: F401
    except ImportError:
        return "skipped (nervaluate absent)"
    gold, pred = _selftest_spans()
    tags = ["PERSON", "LOCATION", "DATE"]
    nv = run_nervaluate([span_to_nervaluate(gold)], [span_to_nervaluate(pred)], tags)
    assert abs(nv["strict"]["__micro__"][2] - 2 / 7) < 1e-6, nv["strict"]["__micro__"]
    assert abs(nv["ent_type"]["__micro__"][2] - 4 / 7) < 1e-6, nv["ent_type"]["__micro__"]
    return "passed"


def run_selftest() -> int:
    try:
        test_span_to_nervaluate_conversion()
        test_compare_math_matches_hand_computed()
        nv_state = _test_nervaluate_agrees_when_present()
    except AssertionError as exc:
        print(f"SELF-TEST FAIL: {exc}", file=sys.stderr)
        return 1
    print("SELF-TEST PASS")
    print("  conversion: char-span -> nervaluate {label,start,end} pass-through OK")
    print("  compare.py math: strict micro (1,2,3) F1=2/7, "
          "partial micro (2,1,2) F1=4/7 OK")
    print(f"  nervaluate agreement check: {nv_state}")
    if nv_state.startswith("skipped"):
        print(f"  (install to enable the live cross-check: {NERVALUATE_PIP})")
    return 0


# --------------------------------------------------------------------------
# main
# --------------------------------------------------------------------------
def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__.split("\n")[0])
    ap.add_argument("--gold", help="gold corpus jsonl (data/corpus.jsonl)")
    ap.add_argument("--engine", help="name=path/to/preds.jsonl (as compare.py)")
    ap.add_argument("--label-map",
                    default=str(Path(__file__).parent / "label_map.yaml"))
    ap.add_argument("--tags", default="",
                    help="comma-separated canonical types to cross-check "
                         "(default: label_map canonical minus OTHER)")
    ap.add_argument("--out", default="",
                    help="optional path to write the markdown report "
                         "(always printed to stdout)")
    ap.add_argument("--selftest", action="store_true",
                    help="run the no-external-lib conversion + math self-test "
                         "and exit")
    args = ap.parse_args()

    if args.selftest:
        return run_selftest()

    if not args.gold or not args.engine:
        ap.error("--gold and --engine are required (or pass --selftest)")
    if "=" not in args.engine:
        ap.error(f"--engine expects name=path, got {args.engine!r}")
    engine_name, pred_path = args.engine.split("=", 1)

    label_map = compare._load_label_map(Path(args.label_map))
    canonical = list(label_map.get("canonical", []))
    canonical_set = set(canonical)
    gmap = label_map.get("gold", {}) or {}
    pmap = _pmap_for(engine_name, label_map)

    tags = ([t.strip() for t in args.tags.split(",") if t.strip()]
            if args.tags else list(canonical))

    gold_docs = compare._load_jsonl(Path(args.gold))
    pred_docs = compare._load_jsonl(Path(pred_path))

    # compare.py's authoritative tallies over the exact same inputs. Reusing
    # _evaluate guarantees the reference numbers are the scorer's own, not a
    # re-derivation. canonical_set | {"OTHER"} matches compare.py's own call.
    cmp = compare._evaluate(gold_docs, pred_docs, gmap, pmap,
                            canonical_set | {"OTHER"})
    cmp_strict = cmp["strict"]
    cmp_partial = cmp["partial"]

    scored_ids, true_docs, pred_docs_nv = build_nervaluate_docs(
        gold_docs, pred_docs, gmap, pmap, canonical_set | {"OTHER"}, tags)

    have_nv = True
    nv: dict = {}
    if not scored_ids:
        print("WARNING: gold ∩ prediction is empty — nothing to score. "
              "Check the --gold / --engine paths.", file=sys.stderr)
        have_nv = False
    else:
        try:
            nv = run_nervaluate(true_docs, pred_docs_nv, tags)
        except ImportError:
            have_nv = False
            print("=" * 70, file=sys.stderr)
            print("NOTE: nervaluate not installed — the independent cross-check "
                  "was SKIPPED.", file=sys.stderr)
            print(f"      Install it to reproduce the citable numbers: "
                  f"{NERVALUATE_PIP}", file=sys.stderr)
            print("      (anonde's own compare.py numbers still print below.)",
                  file=sys.stderr)
            print("=" * 70, file=sys.stderr)

    report = build_report(engine_name, len(gold_docs), len(scored_ids),
                          canonical, cmp_strict, cmp_partial, nv, have_nv)
    print(report)
    if args.out:
        Path(args.out).write_text(report, encoding="utf-8")
        print(f"wrote {args.out}", file=sys.stderr)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
