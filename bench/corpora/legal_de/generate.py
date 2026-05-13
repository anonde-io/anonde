#!/usr/bin/env python3
"""Generate synthetic German legal text with PII gold by construction.

For each document type (Klageschrift, Beschluss, Vergleich, Vollmacht,
Anwaltsschreiben):

  1. Pick one header, K body paragraphs, one footer from templates.py
  2. Walk the assembled template's `{SLOT}` markers in order
  3. Each slot calls a generator from generators.py, which returns a
     Slot(text, type) tuple. We splice the surface into the output and
     record (codepoint_start, codepoint_end, type) as a gold span.

Result mirrors the synth_clinical corpus.jsonl shape, so compare.py and
the existing label_map.yaml work unchanged. Streitwert / settlement
amount is rendered as a non-PHI placeholder fill — visible in text,
no gold span — per the spec.
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
from templates import DOCTYPES  # noqa: E402

SLOT_RE = re.compile(r"\{([A-Z_]+)\}")

# Non-PHI placeholder fills: realistic surface, but no gold span emitted.
# AMOUNT (Streitwert / Forderung / Vergleichsbetrag) is intentionally
# NOT a PII category — see spec.
NON_PHI_FILLS: dict[str, list[str]] = {
    "AMOUNT": [
        "12.500", "8.750", "23.480,55", "150.000", "4.999,99", "75.300",
        "1.250", "320.000", "60.500,00", "9.840", "187.620,42", "2.300,00",
        "45.000", "510.000", "27.450,80",
    ],
}


def render_template(
    tpl: str, rng: random.Random
) -> tuple[str, list[dict]]:
    """Walk a template, fill slots, return (text, gold_spans).

    Offsets are codepoint-based to match how compare.py reads the
    `start` / `end` fields (Python string slicing is codepoint-aware).
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
        if slot in GENERATORS:
            s = GENERATORS[slot](rng)
            parts.append(s.text)
            spans.append({"start": cur_len, "end": cur_len + len(s.text),
                          "type": s.type})
            cur_len += len(s.text)
        elif slot in NON_PHI_FILLS:
            v = rng.choice(NON_PHI_FILLS[slot])
            parts.append(v)
            cur_len += len(v)
        else:
            # Unknown slot — emit a visible placeholder so it surfaces
            # in spot-checks rather than silently corrupting text.
            v = f"[{slot}]"
            parts.append(v)
            cur_len += len(v)
        pos = m.end()
    tail = tpl[pos:]
    parts.append(tail)
    return "".join(parts), spans


def render_doc(
    doctype: str, rng: random.Random
) -> tuple[str, list[dict]]:
    headers, body_pool, footers = DOCTYPES[doctype]
    header_tpl = rng.choice(headers)
    # Headers are PHI-dense already; bodies add ~2..5 paragraphs of
    # mixed-density narrative. Cap by pool size to avoid sampling
    # duplicates. The 2..5 range keeps total gold spans per doc in the
    # spec's 6..14 band.
    n_body = rng.randint(2, min(5, len(body_pool)))
    body_lines = rng.sample(body_pool, n_body)
    footer_tpl = rng.choice(footers)
    full_tpl = header_tpl.rstrip() + "\n\n" \
        + "\n".join(body_lines) + "\n" \
        + footer_tpl.lstrip()
    return render_template(full_tpl, rng)


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--out", required=True, help="output corpus.jsonl")
    ap.add_argument("--per-doctype", type=int, default=30,
                    help="how many docs per doctype (5 doctypes total)")
    ap.add_argument("--seed", type=int, default=20260513,
                    help="deterministic seed for reproducible runs")
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
                # Sanity: every recorded span surface == text[start:end]
                # and is non-empty (compare.py and our README claim
                # this).
                for s in spans:
                    surf = text[s["start"]:s["end"]]
                    assert surf, (s, doctype)
                    assert s["end"] > s["start"], (s, doctype)
                    per_type[s["type"]] = per_type.get(s["type"], 0) + 1
                doc_id = f"leg-de-{doctype}-{i:04d}"
                fout.write(json.dumps(
                    {"id": doc_id, "text": text, "entities": spans},
                    ensure_ascii=False,
                ) + "\n")
                docs += 1
                spans_total += len(spans)
                per_doctype[doctype] = per_doctype.get(doctype, 0) + 1

    by_dt = ", ".join(f"{k}={v}" for k, v in sorted(per_doctype.items()))
    top_types = sorted(per_type.items(), key=lambda kv: -kv[1])[:6]
    by_t = ", ".join(f"{k}={v}" for k, v in top_types)
    print(f"wrote {out_path}: {docs} docs ({by_dt}), "
          f"{spans_total} gold spans (top: {by_t})",
          file=sys.stderr)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
