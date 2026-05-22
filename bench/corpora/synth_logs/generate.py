#!/usr/bin/env python3
"""Generate synthetic enterprise log text with PII gold by construction.

For each log type (auth, error, access, audit):

  1. Emit a header line, then N body log lines sampled WITH replacement
     from the type's line pool (real logs repeat line shapes).
  2. Walk each line's `{SLOT}` markers in order. PII / SECRET slots call
     a generator from generators.py returning Slot(text, type). The
     surface is spliced into the output and (codepoint_start,
     codepoint_end, type) is recorded as a gold span. Non-PII scaffold
     slots ({TS}, {LEVEL}, {STATUS}, ...) are filled but NOT tagged.

Classification: synth_logs is wired into the matrix as an ENGLISH
corpus. The log scaffolding is English; only the embedded PII values
(person names, addresses) are sampled across EN/DE/ES/FR/IT locales —
which is realistic for a global SaaS. There is no single dominant PII
language, so English (the scaffolding language) is the honest default;
the alternative (a sixth "mixed" partition) would need new matrix
plumbing for no benchmarking gain.

Gold types:

  * PII slots emit canonical label_map types directly: EMAIL, PERSON,
    URL, PHONE, and ID (IP / MAC / username / account-id all fold to ID
    in label_map.yaml). Scored by default.
  * SECRET slots (API keys, JWTs, bearer/session tokens, OAuth secrets)
    emit the gold type `SECRET`. anonde ships no secret recognizer yet;
    `SECRET` maps to `~` in label_map.yaml so the spans are DROPPED from
    scoring, giving a fair PII-only leak number today. The spans remain
    in the gold JSONL — a future phase can score them by flipping the
    one `SECRET:` line in label_map.yaml. See the corpus README.
"""

from __future__ import annotations

import argparse
import json
import random
import re
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).parent))
from generators import GENERATORS, SECRET_GENERATORS  # noqa: E402
from templates import LOGTYPE_ORDER, LOGTYPES  # noqa: E402

SLOT_RE = re.compile(r"\{([A-Z_]+)\}")

_LEVELS = ["INFO", "WARN", "ERROR", "DEBUG"]
_METHODS = ["GET", "POST", "PUT", "PATCH", "DELETE"]
_STATUSES = ["200", "201", "204", "301", "400", "401", "403", "404",
             "429", "500", "502", "503"]


def _fill_scaffold(slot: str, rng: random.Random) -> str | None:
    """Return surface for a non-PII scaffold slot, or None if `slot` is
    not a scaffold slot."""
    if slot == "TS":
        y, mo, d = 2026, rng.randint(1, 5), rng.randint(1, 28)
        h, mi, s = rng.randint(0, 23), rng.randint(0, 59), rng.randint(0, 59)
        return f"{y}-{mo:02d}-{d:02d}T{h:02d}:{mi:02d}:{s:02d}Z"
    if slot == "LEVEL":
        return _LEVELS[rng.randrange(len(_LEVELS))]
    if slot == "STATUS":
        return _STATUSES[rng.randrange(len(_STATUSES))]
    if slot == "METHOD":
        return _METHODS[rng.randrange(len(_METHODS))]
    if slot == "LATENCY":
        return str(rng.randint(2, 4800))
    if slot == "ERRCODE":
        return f"E{rng.randint(1000, 9999)}"
    return None


def _render_line(
    tpl: str, rng: random.Random, base_offset: int
) -> tuple[str, list[dict]]:
    """Render one log line; return (text, spans) with absolute codepoint
    offsets (base_offset + local position)."""
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
            spans.append({
                "start": base_offset + cur_len,
                "end": base_offset + cur_len + len(s.text),
                "type": s.type,
            })
            cur_len += len(s.text)
        else:
            scaffold = _fill_scaffold(slot, rng)
            if scaffold is None:
                scaffold = f"[{slot}]"
            parts.append(scaffold)
            cur_len += len(scaffold)
        pos = m.end()
    parts.append(tpl[pos:])
    return "".join(parts), spans


def render_doc(
    logtype: str, rng: random.Random
) -> tuple[str, list[dict]]:
    header_tpl, line_pool, (lo, hi) = LOGTYPES[logtype]
    n_lines = rng.randint(lo, hi)

    text_parts: list[str] = []
    spans: list[dict] = []
    cur = 0

    header_text, header_spans = _render_line(header_tpl, rng, cur)
    text_parts.append(header_text)
    spans.extend(header_spans)
    cur += len(header_text)

    for i in range(n_lines):
        # Sample WITH replacement — real logs repeat line shapes.
        line_tpl = line_pool[rng.randrange(len(line_pool))]
        line_text, line_spans = _render_line(line_tpl, rng, cur)
        text_parts.append(line_text)
        spans.extend(line_spans)
        cur += len(line_text)
        text_parts.append("\n")
        cur += 1

    return "".join(text_parts), spans


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--out", required=True, help="output corpus.jsonl")
    ap.add_argument(
        "--per-logtype", type=int, default=30,
        help="how many docs per log type (4 log types total)",
    )
    ap.add_argument(
        "--seed", type=int, default=20260512,
        help="deterministic seed for reproducible runs",
    )
    args = ap.parse_args()

    rng = random.Random(args.seed)
    out_path = Path(args.out)
    out_path.parent.mkdir(parents=True, exist_ok=True)

    secret_names = set(SECRET_GENERATORS.keys())
    docs = 0
    spans_total = 0
    secret_spans = 0
    per_logtype: dict[str, int] = {}
    per_type: dict[str, int] = {}
    with out_path.open("w", encoding="utf-8") as fout:
        for logtype in LOGTYPE_ORDER:
            for i in range(args.per_logtype):
                text, spans = render_doc(logtype, rng)
                # Sanity: every span surface is non-empty + well-formed.
                for s in spans:
                    surf = text[s["start"]:s["end"]]
                    assert surf, (s, logtype, i)
                    assert s["end"] > s["start"], (s, logtype, i)
                    per_type[s["type"]] = per_type.get(s["type"], 0) + 1
                    if s["type"] == "SECRET":
                        secret_spans += 1
                doc_id = f"logs-{logtype}-{i:04d}"
                fout.write(
                    json.dumps(
                        {"id": doc_id, "text": text, "entities": spans},
                        ensure_ascii=False,
                    )
                    + "\n"
                )
                docs += 1
                spans_total += len(spans)
                per_logtype[logtype] = per_logtype.get(logtype, 0) + 1

    _ = secret_names  # documented for clarity; SECRET tagging is by gen type
    summary = ", ".join(f"{k}={v}" for k, v in sorted(per_logtype.items()))
    type_summary = ", ".join(
        f"{k}={v}" for k, v in sorted(per_type.items(), key=lambda x: -x[1])
    )
    print(
        f"wrote {out_path}: {docs} docs ({summary}), {spans_total} gold "
        f"spans ({secret_spans} SECRET — dropped from scoring via "
        f"label_map); types: {type_summary}",
        file=sys.stderr,
    )
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
