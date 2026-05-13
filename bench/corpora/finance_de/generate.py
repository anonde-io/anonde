#!/usr/bin/env python3
"""Generate synthetic German financial text with PII gold by construction.

For each doc type (Kontoauszug, Überweisungsauftrag, Kreditantrag,
Depot-Auszug, KYC-Anfrage):

  1. Pick one header, K body paragraphs, one footer from templates.py
  2. If the doctype has a row-template (transactions, holdings), render
     M instances of that row and splice them into the assembled template
     at the `{ROWS_<KEY>}` marker — each row gets fresh slot values.
  3. Walk the resulting template's `{SLOT}` markers in order. Each PII
     slot calls a generator that returns a Slot(text, type). The surface
     is spliced into the output and we record (start, end, type) as a
     gold span using **codepoint** offsets, which is what compare.py
     compares against (consistent with synth_clinical and the openmed
     gold).

Non-PHI fills:

  * AMOUNT / AMOUNT_NEG — euro amount in German formatting (1.234,56 EUR).
    Amounts are NOT marked as gold spans — most PII detectors ignore
    monetary values, and counting them as gold would inflate leak rates.
  * QTY — share count for Depot-Auszug rows.
  * REF — reference text drawn from REF_PURPOSES.
  * SOURCE_OF_FUNDS — paragraph drawn from SOURCE_OF_FUNDS.
  * SECURITY_NAME — non-PHI half of an (ISIN, name) pair. The ISIN slot
    consumed adjacent to it is filled via gen_security so the (ISIN,
    name) pair stays consistent within a row.
"""

from __future__ import annotations

import argparse
import json
import random
import re
import sys
from pathlib import Path

# Bundle generators + templates as sibling modules.
sys.path.insert(0, str(Path(__file__).parent))
from generators import (  # noqa: E402
    GENERATORS,
    REF_PURPOSES,
    SOURCE_OF_FUNDS,
    gen_security,
)
from templates import BODY_PICK_RANGES, DOCTYPES  # noqa: E402

SLOT_RE = re.compile(r"\{([A-Z_]+)\}")


def _format_amount(rng: random.Random, negative: bool = False) -> str:
    """German amount formatting: thousands separator '.', decimal ',',
    suffix ' EUR'. Negative gets a leading minus."""
    # Bias toward small-to-mid amounts so transaction lists stay realistic.
    cents = rng.randint(500, 999999)
    euros, c = divmod(cents, 100)
    # Insert thousand separators.
    s = f"{euros:,}".replace(",", ".")
    out = f"{s},{c:02d} EUR"
    return f"-{out}" if negative else out


def _format_qty(rng: random.Random) -> str:
    return str(rng.randint(1, 500))


def render_row(
    tpl: str, rng: random.Random, doc_offset: int
) -> tuple[str, list[dict]]:
    """Render one row of a multi-row doctype (transactions / holdings).

    doc_offset is the codepoint position in the assembled document where
    this row will start; spans are emitted in absolute document offsets
    so the caller can splice them in unchanged.
    """
    return _render_with_offset(tpl, rng, doc_offset)


def _render_with_offset(
    tpl: str, rng: random.Random, base_offset: int
) -> tuple[str, list[dict]]:
    """Walk a flat template (no {ROWS_*} markers) and emit (text, spans).

    Spans are absolute codepoint offsets, base_offset + local position.
    """
    parts: list[str] = []
    spans: list[dict] = []
    cur_len = 0  # codepoint length emitted so far
    pos = 0
    for m in SLOT_RE.finditer(tpl):
        chunk = tpl[pos:m.start()]
        parts.append(chunk)
        cur_len += len(chunk)
        slot = m.group(1)
        text, span_type = _fill_slot(slot, rng)
        parts.append(text)
        if span_type is not None:
            spans.append({
                "start": base_offset + cur_len,
                "end": base_offset + cur_len + len(text),
                "type": span_type,
            })
        cur_len += len(text)
        pos = m.end()
    parts.append(tpl[pos:])
    return "".join(parts), spans


def _fill_slot(slot: str, rng: random.Random) -> tuple[str, str | None]:
    """Return (surface, gold_type) — gold_type=None means non-PHI fill."""
    if slot in GENERATORS:
        s = GENERATORS[slot](rng)
        return s.text, s.type
    if slot == "AMOUNT":
        return _format_amount(rng), None
    if slot == "AMOUNT_NEG":
        return _format_amount(rng, negative=True), None
    if slot == "QTY":
        return _format_qty(rng), None
    if slot == "REF":
        return REF_PURPOSES[rng.randrange(len(REF_PURPOSES))], None
    if slot == "SOURCE_OF_FUNDS":
        return SOURCE_OF_FUNDS[rng.randrange(len(SOURCE_OF_FUNDS))], None
    if slot == "SECURITY_NAME":
        # Paired with the {ISIN} slot that should precede it; both are
        # filled independently which is acceptable — ISINs and names
        # don't need to correlate for PII evaluation.
        _, name = gen_security(rng)
        return name, None
    # Unknown slot — render as a literal so the user can spot it.
    return f"[{slot}]", None


def _expand_rows(
    full_tpl: str,
    row_tpl: str | None,
    row_marker: str | None,
    n_rows: int,
    rng: random.Random,
) -> tuple[str, list[dict]]:
    """If the doctype has a row template, replace `{ROWS_<MARKER>}` in
    full_tpl with N rendered rows joined by newlines, and return the
    (text, spans) for the whole document. Otherwise render flat.

    The row marker is encoded as `{ROWS_<MARKER>}` so it doesn't collide
    with regular slot syntax. We expand it BEFORE walking the regular
    slots so the offsets line up.
    """
    if row_tpl is None or row_marker is None:
        return _render_with_offset(full_tpl, rng, 0)

    marker = "{ROWS_" + row_marker[len("ROWS_"):] + "}" if row_marker.startswith("ROWS_") else "{" + row_marker + "}"
    idx = full_tpl.find(marker)
    if idx < 0:
        # No marker present in this header/footer combo — just render flat.
        return _render_with_offset(full_tpl, rng, 0)

    # Render the prefix (everything before the marker) first to learn
    # how many codepoints precede the rows.
    prefix_tpl = full_tpl[:idx]
    suffix_tpl = full_tpl[idx + len(marker):]

    prefix_text, prefix_spans = _render_with_offset(prefix_tpl, rng, 0)
    rows_text_parts: list[str] = []
    rows_spans: list[dict] = []
    cur_offset = len(prefix_text)
    for i in range(n_rows):
        line, line_spans = _render_with_offset(row_tpl, rng, cur_offset)
        rows_text_parts.append(line)
        rows_spans.extend(line_spans)
        cur_offset += len(line)
        if i < n_rows - 1:
            rows_text_parts.append("\n")
            cur_offset += 1
    rows_text = "".join(rows_text_parts)
    suffix_text, suffix_spans_local = _render_with_offset(
        suffix_tpl, rng, cur_offset,
    )

    text = prefix_text + rows_text + suffix_text
    spans = prefix_spans + rows_spans + suffix_spans_local
    return text, spans


def render_doc(
    doctype: str, rng: random.Random
) -> tuple[str, list[dict]]:
    headers, body_pool, footers, row_tpl, row_marker, row_range = \
        DOCTYPES[doctype]
    body_lo, body_hi = BODY_PICK_RANGES[doctype]

    header_tpl = headers[rng.randrange(len(headers))]
    footer_tpl = footers[rng.randrange(len(footers))]
    n_body = rng.randint(body_lo, min(body_hi, len(body_pool)))
    body_lines = rng.sample(body_pool, n_body)

    full_tpl = (
        header_tpl.rstrip() + "\n\n"
        + "\n".join(body_lines) + "\n"
        + footer_tpl.lstrip()
    )

    n_rows = 0
    if row_tpl is not None:
        lo, hi = row_range
        n_rows = rng.randint(lo, hi)
    return _expand_rows(full_tpl, row_tpl, row_marker, n_rows, rng)


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--out", required=True, help="output corpus.jsonl")
    ap.add_argument(
        "--per-doctype", type=int, default=30,
        help="how many docs per doctype (5 doctypes total)",
    )
    ap.add_argument(
        "--seed", type=int, default=20260512,
        help="deterministic seed for reproducible runs",
    )
    args = ap.parse_args()

    rng = random.Random(args.seed)
    out_path = Path(args.out)
    out_path.parent.mkdir(parents=True, exist_ok=True)

    docs = 0
    spans_total = 0
    per_doctype: dict[str, int] = {}
    per_type: dict[str, int] = {}
    with out_path.open("w", encoding="utf-8") as fout:
        for doctype in DOCTYPES.keys():
            for i in range(args.per_doctype):
                text, spans = render_doc(doctype, rng)
                # Sanity: every span surface is non-empty and matches text.
                for s in spans:
                    surf = text[s["start"]:s["end"]]
                    assert surf, (s, doctype, i)
                    assert s["end"] > s["start"], (s, doctype, i)
                    per_type[s["type"]] = per_type.get(s["type"], 0) + 1
                doc_id = f"fin-de-{doctype}-{i:04d}"
                fout.write(
                    json.dumps(
                        {"id": doc_id, "text": text, "entities": spans},
                        ensure_ascii=False,
                    )
                    + "\n"
                )
                docs += 1
                spans_total += len(spans)
                per_doctype[doctype] = per_doctype.get(doctype, 0) + 1

    summary = ", ".join(f"{k}={v}" for k, v in sorted(per_doctype.items()))
    type_summary = ", ".join(
        f"{k}={v}" for k, v in sorted(per_type.items(), key=lambda x: -x[1])
    )
    print(
        f"wrote {out_path}: {docs} docs ({summary}), "
        f"{spans_total} gold spans; types: {type_summary}",
        file=sys.stderr,
    )
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
