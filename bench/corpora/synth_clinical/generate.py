#!/usr/bin/env python3
"""Generate synthetic German clinical text with PII gold by construction.

For each sublanguage (ED triage, OP report, radiology, rehab discharge):

  1. Pick one header, K body paragraphs, one footer from templates.py
  2. Walk the assembled template's `{SLOT}` markers in order
  3. Each slot calls a generator from generators.py, which returns a
     (surface, type) tuple. We splice the surface into the output and
     record (codepoint_start, codepoint_end, type) as a gold span.

Result mirrors GraSCCo's corpus.jsonl shape so compare.py and the
existing label_map.yaml work unchanged.
"""

from __future__ import annotations

import argparse
import json
import random
import re
import sys
from pathlib import Path

# Import the bundled vocab + generators.
sys.path.insert(0, str(Path(__file__).parent))
from generators import GENERATORS  # noqa: E402
from templates import SUBLANGUAGES  # noqa: E402

SLOT_RE = re.compile(r"\{([A-Z_]+)\}")

# Non-PHI placeholder fills: filled with realistic values but no gold span.
NON_PHI_FILLS: dict[str, list[str]] = {
    "TRIAGE_LEVEL": ["1 (Rot)", "2 (Orange)", "3 (Gelb)", "4 (Grün)"],
}


def render_template(
    tpl: str, rng: random.Random
) -> tuple[str, list[dict]]:
    """Walk a template, fill slots, return (text, gold_spans)."""
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
            v = f"[{slot}]"
            parts.append(v)
            cur_len += len(v)
        pos = m.end()
    tail = tpl[pos:]
    parts.append(tail)
    return "".join(parts), spans


def render_doc(
    sublang: str, rng: random.Random
) -> tuple[str, list[dict]]:
    headers, body_pool, footers = SUBLANGUAGES[sublang]
    header_tpl = rng.choice(headers)
    n_body = rng.randint(6, min(14, len(body_pool)))
    body_lines = rng.sample(body_pool, n_body)
    footer_tpl = rng.choice(footers)
    full_tpl = header_tpl.rstrip() + "\n\n" \
        + "\n".join(body_lines) + "\n" \
        + footer_tpl.lstrip()
    return render_template(full_tpl, rng)


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--out", required=True, help="output corpus.jsonl")
    ap.add_argument("--per-sublanguage", type=int, default=30,
                    help="how many docs per sublanguage (4 sublanguages total)")
    ap.add_argument("--seed", type=int, default=20260512,
                    help="deterministic seed for reproducible runs")
    args = ap.parse_args()

    rng = random.Random(args.seed)
    out_path = Path(args.out)
    out_path.parent.mkdir(parents=True, exist_ok=True)

    docs = 0
    spans_total = 0
    per_sublang: dict[str, int] = {}
    with out_path.open("w", encoding="utf-8") as fout:
        for sublang in SUBLANGUAGES.keys():
            for i in range(args.per_sublanguage):
                text, spans = render_doc(sublang, rng)
                # Sanity: every recorded span surface == text[start:end].
                for s in spans:
                    surf = text[s["start"]:s["end"]]
                    assert surf, (s, sublang)
                doc_id = f"synth-{sublang}-{i:04d}"
                fout.write(json.dumps(
                    {"id": doc_id, "text": text, "entities": spans},
                    ensure_ascii=False,
                ) + "\n")
                docs += 1
                spans_total += len(spans)
                per_sublang[sublang] = per_sublang.get(sublang, 0) + 1

    summary = ", ".join(f"{k}={v}" for k, v in sorted(per_sublang.items()))
    print(f"wrote {out_path}: {docs} docs ({summary}), {spans_total} gold spans",
          file=sys.stderr)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
