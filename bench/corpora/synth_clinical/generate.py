#!/usr/bin/env python3
"""Generate synthetic clinical text with PII gold by construction.

This is the SINGLE shared generator for all four synth_clinical_{de,en,
fr,it} corpora. The three sibling corpora (synth_clinical_{en,fr,it})
are thin Makefile wrappers that call this script with a different
`--language`; there is no per-language copy of the generator (mirrors
the synth_finance / ai4privacy / mapa shared-loader pattern).

There is deliberately NO synth_clinical_es — Spanish clinical PHI is
covered by the real-gold MEDDOCAN corpus, bench/corpora/meddocan_es.

For each of four clinical sublanguages (ED triage, OP report, radiology,
rehab discharge):

  1. Pick one header, K body paragraphs, one footer from templates.py
     (localised to the requested --language).
  2. Walk the assembled template's `{SLOT}` markers in order.
  3. Each slot calls a generator from generators.py, which returns a
     Slot(surface, type). We splice the surface into the output and
     record (codepoint_start, codepoint_end, type) as a gold span.

Result mirrors GraSCCo's corpus.jsonl shape so compare.py and the
existing label_map.yaml work unchanged.

REGRESSION CONTRACT — `synth_clinical` (the de corpus) is the per-push
regression anchor in .github/workflows/bench.yml. With `--language de`
(the default) this generator MUST reproduce the historical corpus
byte-for-byte at a fixed seed:

  * the German RNG stream uses the bare `--seed` (no language fold), so
    the rng draw sequence is identical to the pre-refactor generator;
  * en/fr/it fold the language into the seed (so the four corpora don't
    share an identical RNG stream) — de is the explicit exception.
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
from templates import SUBLANGUAGES, TEMPLATES  # noqa: E402

SLOT_RE = re.compile(r"\{([A-Z_]+)\}")

# Non-PHI placeholder fills: filled with realistic values but no gold span.
# Localised so the {TRIAGE_LEVEL} slot reads naturally per language.
NON_PHI_FILLS: dict[str, dict[str, list[str]]] = {
    "TRIAGE_LEVEL": {
        "de": ["1 (Rot)", "2 (Orange)", "3 (Gelb)", "4 (Grün)"],
        "en": ["1 (Red)", "2 (Orange)", "3 (Yellow)", "4 (Green)"],
        "fr": ["1 (Rouge)", "2 (Orange)", "3 (Jaune)", "4 (Vert)"],
        "it": ["1 (Rosso)", "2 (Arancione)", "3 (Giallo)", "4 (Verde)"],
    },
}


def render_template(
    tpl: str, lang: str, rng: random.Random
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
            s = GENERATORS[slot](lang, rng)
            parts.append(s.text)
            spans.append({"start": cur_len, "end": cur_len + len(s.text),
                          "type": s.type})
            cur_len += len(s.text)
        elif slot in NON_PHI_FILLS:
            v = rng.choice(NON_PHI_FILLS[slot][lang])
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
    lang: str, sublang: str, rng: random.Random
) -> tuple[str, list[dict]]:
    headers, body_pool, footers = TEMPLATES[lang][sublang]
    header_tpl = rng.choice(headers)
    n_body = rng.randint(6, min(14, len(body_pool)))
    body_lines = rng.sample(body_pool, n_body)
    footer_tpl = rng.choice(footers)
    full_tpl = header_tpl.rstrip() + "\n\n" \
        + "\n".join(body_lines) + "\n" \
        + footer_tpl.lstrip()
    return render_template(full_tpl, lang, rng)


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--out", required=True, help="output corpus.jsonl")
    ap.add_argument(
        "--language", default="de", choices=sorted(TEMPLATES.keys()),
        help="locale: de|en|fr|it (no es — see MEDDOCAN)",
    )
    ap.add_argument("--per-sublanguage", type=int, default=30,
                    help="how many docs per sublanguage (4 sublanguages total)")
    ap.add_argument("--seed", type=int, default=20260512,
                    help="deterministic seed for reproducible runs")
    args = ap.parse_args()

    lang = args.language
    # REGRESSION CONTRACT: de uses the bare seed so the German corpus is
    # byte-identical to the pre-refactor single-language generator. The
    # other languages fold the language into the seed so the four
    # corpora don't share an identical RNG stream.
    if lang == "de":
        rng = random.Random(args.seed)
    else:
        rng = random.Random(args.seed + sum(ord(c) for c in lang))
    out_path = Path(args.out)
    out_path.parent.mkdir(parents=True, exist_ok=True)

    docs = 0
    spans_total = 0
    per_sublang: dict[str, int] = {}
    with out_path.open("w", encoding="utf-8") as fout:
        for sublang in SUBLANGUAGES:
            for i in range(args.per_sublanguage):
                text, spans = render_doc(lang, sublang, rng)
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
    print(f"wrote {out_path} (lang={lang}): {docs} docs ({summary}), "
          f"{spans_total} gold spans",
          file=sys.stderr)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
