#!/usr/bin/env python3
"""Generate synthetic financial text with PII gold by construction.

This is the SINGLE shared generator for all five synth_finance_{en,de,
es,fr,it} corpora. The four sibling corpora are thin Makefile wrappers
that call this script with a different `--language`; there is no
per-language copy of the generator (mirrors the ai4privacy / mapa
shared-loader pattern — see bench/corpora/ai4privacy_en/cmd).

For each doc type (invoice, bank statement, KYC/onboarding, transaction
confirmation):

  1. Pick one header, N body paragraphs, one footer from templates.py
     (localised to the requested --language).
  2. The bank-statement doc type carries a `{ROWS_TX}` marker; M
     transaction rows are rendered from a row template and spliced in,
     each row with fresh slot values.
  3. Walk the assembled template's `{SLOT}` markers in order. PII slots
     call a generator from generators.py which returns Slot(text, type).
     The surface is spliced into the output and (codepoint_start,
     codepoint_end, type) is recorded as a gold span.

Gold types emitted are the CANONICAL label_map types directly (PERSON,
IBAN, ID, ADDRESS, ORGANIZATION, DATE, EMAIL, PHONE, PROFESSION) — every
one is a pass-through entry in the gold: section of label_map.yaml, so
no label-map changes are needed.

Monetary amounts ({AMOUNT}/{AMOUNT_NEG}), payment references ({REF}) and
invoice numbers ({INVNO}) are filled with realistic values but are NOT
gold-tagged — anonde's no-monetary-PII rule, and the brief scopes the
PII slot set to the canonical list above.
"""

from __future__ import annotations

import argparse
import json
import random
import re
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).parent))
from generators import GENERATORS  # noqa: E402
from templates import DOCTYPES, TEMPLATES  # noqa: E402

SLOT_RE = re.compile(r"\{([A-Z_]+)\}")


def _format_amount(rng: random.Random, lang: str, negative: bool = False) -> str:
    """Locale-formatted monetary amount. NOT a gold span."""
    cents = rng.randint(500, 9999999)
    euros, c = divmod(cents, 100)
    if lang == "en":
        s = f"{euros:,}"  # 1,234
        out = f"£{s}.{c:02d}"
    else:
        # de/es/fr/it: thousands '.', decimal ',' suffix EUR.
        s = f"{euros:,}".replace(",", ".")
        out = f"{s},{c:02d} EUR"
    return f"-{out}" if negative else out


def _format_invno(rng: random.Random) -> str:
    """Invoice number — a document identifier, deliberately NOT a gold
    span (the brief scopes PII to person/account identifiers)."""
    return f"INV-{rng.randint(2018, 2026)}-{rng.randint(1000, 99999)}"


_REFS = [
    "monthly retainer", "order 4471", "subscription renewal", "consulting Q3",
    "deposit refund", "service fee", "membership dues", "expense reimbursement",
]


def _fill_slot(
    slot: str, lang: str, rng: random.Random
) -> tuple[str, str | None]:
    """Return (surface, gold_type). gold_type=None marks a non-PII fill."""
    if slot in GENERATORS:
        s = GENERATORS[slot](lang, rng)
        return s.text, s.type
    if slot == "AMOUNT":
        return _format_amount(rng, lang), None
    if slot == "AMOUNT_NEG":
        return _format_amount(rng, lang, negative=True), None
    if slot == "REF":
        return _REFS[rng.randrange(len(_REFS))], None
    if slot == "INVNO":
        return _format_invno(rng), None
    # Unknown slot — render as a literal so it is visible in the corpus.
    return f"[{slot}]", None


def _render_with_offset(
    tpl: str, lang: str, rng: random.Random, base_offset: int
) -> tuple[str, list[dict]]:
    """Walk a flat template (no {ROWS_*} markers); emit (text, spans).

    Spans are absolute codepoint offsets: base_offset + local position.
    """
    parts: list[str] = []
    spans: list[dict] = []
    cur_len = 0
    pos = 0
    for m in SLOT_RE.finditer(tpl):
        chunk = tpl[pos:m.start()]
        parts.append(chunk)
        cur_len += len(chunk)
        slot = m.group(1)
        text, span_type = _fill_slot(slot, lang, rng)
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


def _expand_rows(
    full_tpl: str,
    row_tpl: str | None,
    row_marker: str | None,
    n_rows: int,
    lang: str,
    rng: random.Random,
) -> tuple[str, list[dict]]:
    """Replace `{ROWS_<MARKER>}` with N rendered rows; return whole-doc
    (text, spans). Renders flat if the doctype has no row template."""
    if row_tpl is None or row_marker is None:
        return _render_with_offset(full_tpl, lang, rng, 0)

    marker = "{" + row_marker + "}"
    idx = full_tpl.find(marker)
    if idx < 0:
        return _render_with_offset(full_tpl, lang, rng, 0)

    prefix_tpl = full_tpl[:idx]
    suffix_tpl = full_tpl[idx + len(marker):]

    prefix_text, prefix_spans = _render_with_offset(prefix_tpl, lang, rng, 0)
    rows_parts: list[str] = []
    rows_spans: list[dict] = []
    cur = len(prefix_text)
    for i in range(n_rows):
        line, line_spans = _render_with_offset(row_tpl, lang, rng, cur)
        rows_parts.append(line)
        rows_spans.extend(line_spans)
        cur += len(line)
        if i < n_rows - 1:
            rows_parts.append("\n")
            cur += 1
    rows_text = "".join(rows_parts)
    suffix_text, suffix_spans = _render_with_offset(suffix_tpl, lang, rng, cur)

    return (
        prefix_text + rows_text + suffix_text,
        prefix_spans + rows_spans + suffix_spans,
    )


def render_doc(
    lang: str, doctype: str, rng: random.Random
) -> tuple[str, list[dict]]:
    (headers, body_pool, footers, row_tpl, row_marker, row_range,
     body_range) = TEMPLATES[lang][doctype]

    header_tpl = headers[rng.randrange(len(headers))]
    footer_tpl = footers[rng.randrange(len(footers))]
    body_lo, body_hi = body_range
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
    return _expand_rows(full_tpl, row_tpl, row_marker, n_rows, lang, rng)


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--out", required=True, help="output corpus.jsonl")
    ap.add_argument(
        "--language", required=True, choices=sorted(TEMPLATES.keys()),
        help="locale: en|de|es|fr|it",
    )
    ap.add_argument(
        "--per-doctype", type=int, default=30,
        help="how many docs per doc type (4 doc types total)",
    )
    ap.add_argument(
        "--seed", type=int, default=20260512,
        help="deterministic seed for reproducible runs",
    )
    args = ap.parse_args()

    lang = args.language
    # Fold the language into the seed so the five corpora don't share
    # identical RNG streams (which would make them suspiciously parallel).
    rng = random.Random(args.seed + sum(ord(c) for c in lang))
    out_path = Path(args.out)
    out_path.parent.mkdir(parents=True, exist_ok=True)

    docs = 0
    spans_total = 0
    per_doctype: dict[str, int] = {}
    per_type: dict[str, int] = {}
    with out_path.open("w", encoding="utf-8") as fout:
        for doctype in DOCTYPES:
            for i in range(args.per_doctype):
                text, spans = render_doc(lang, doctype, rng)
                # Sanity: every span surface is non-empty + well-formed.
                for s in spans:
                    surf = text[s["start"]:s["end"]]
                    assert surf, (s, lang, doctype, i)
                    assert s["end"] > s["start"], (s, lang, doctype, i)
                    per_type[s["type"]] = per_type.get(s["type"], 0) + 1
                doc_id = f"fin-{lang}-{doctype}-{i:04d}"
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
        f"wrote {out_path} (lang={lang}): {docs} docs ({summary}), "
        f"{spans_total} gold spans; types: {type_summary}",
        file=sys.stderr,
    )
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
