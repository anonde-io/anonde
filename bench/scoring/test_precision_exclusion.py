#!/usr/bin/env python3
"""Self-test for the zero-gold precision-exclusion rule in
render_matrix.py.

The precision scorecard drops any `(corpus, entity-type)` cell with no
gold of that type (`tp + fn == 0`) from the precision pool, because with
no gold present every prediction there is a mechanical false positive
against absent gold (a schema gap), not real over-redaction. This test
proves the excluded cell:

  * does NOT affect the pooled precision (roll-up + single cell), yet
  * IS still present in the raw partial tally (nothing is deleted), and
  * IS counted by the exclusion-stats footnote helper.

Leak-rate / recall never touch these functions, so they are unaffected.

Run:  python3 bench/scoring/test_precision_exclusion.py
"""

import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).parent))

import render_matrix as rm  # noqa: E402


def _mk_cell(partial: dict, total_gold: int) -> dict:
    """Minimal cell shape the precision helpers read."""
    return {"partial": partial, "total_gold": total_gold}


def main() -> int:
    # corpusA: PERSON has gold (scoreable); LOCATION has ZERO gold
    #          (tp+fn==0) but the engine predicted 5 spans of it — a
    #          pure schema-gap FP group that must be excluded.
    # emptyB:  every type is zero-gold (total_gold == 0) — the whole
    #          corpus must drop out of the precision pool.
    rows = {
        ("corpusA", "eng"): _mk_cell(
            {
                "PERSON":   [8, 2, 1],   # tp+fn = 9  → scoreable
                "LOCATION": [0, 5, 0],   # tp+fn = 0  → excluded
            },
            total_gold=9,
        ),
        ("emptyB", "eng"): _mk_cell(
            {"ORG": [0, 7, 0]},          # tp+fn = 0  → excluded
            total_gold=0,
        ),
    }
    corpora = ["corpusA", "emptyB"]
    engines = ["eng"]

    # ---- raw tally still sees the zero-gold spans (nothing deleted) ----
    tp_raw, fp_raw = rm._partial_pred_tally(rows[("corpusA", "eng")],
                                            exclude_zero_gold=False)
    assert (tp_raw, fp_raw) == (8, 7), (tp_raw, fp_raw)   # 2 + 5 fp
    tp_exc, fp_exc = rm._partial_pred_tally(rows[("corpusA", "eng")],
                                            exclude_zero_gold=True)
    assert (tp_exc, fp_exc) == (8, 2), (tp_exc, fp_exc)   # LOCATION dropped

    # ---- single-cell precision: excluded cell doesn't move it ----------
    p_cell, tp_c, fp_c = rm._cell_partial_precision(rows, "corpusA", "eng")
    assert (tp_c, fp_c) == (8, 2) and abs(p_cell - 0.8) < 1e-9, p_cell

    # ---- pooled (roll-up) precision, corrected vs raw ------------------
    p_corr, tp_s, fp_s = rm._group_partial_precision(rows, corpora, "eng")
    # corpusA scoreable: tp 8 / fp 2 ; emptyB fully excluded → 8/(8+2)=0.8
    assert (tp_s, fp_s) == (8, 2), (tp_s, fp_s)
    assert abs(p_corr - 0.8) < 1e-9, p_corr

    p_raw, tp_r, fp_r = rm._group_partial_precision(
        rows, corpora, "eng", exclude_zero_gold=False)
    # raw pools everything: tp 8 / fp (2+5+7)=14 → 8/22 = 0.3636…
    assert (tp_r, fp_r) == (8, 14), (tp_r, fp_r)
    assert abs(p_raw - 8 / 22) < 1e-9, p_raw
    assert p_raw < p_corr, "exclusion must lift precision here"

    # ---- exclusion stats for the footnote ------------------------------
    n_cells, n_corp = rm._precision_exclusion_stats(rows, corpora, engines)
    # (corpusA,LOCATION) and (emptyB,ORG) → 2 type-cells; emptyB empty-gold
    assert n_cells == 2, n_cells
    assert n_corp == 1, n_corp

    print("PASS  raw=(8,7) excl=(8,2) | cell P=0.800 | "
          f"pool corr=0.800 raw={p_raw:.3f} | excluded {n_cells} cells / "
          f"{n_corp} corpora")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
