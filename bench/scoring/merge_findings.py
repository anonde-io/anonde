#!/usr/bin/env python3
"""merge_findings — union two or more findings JSONLs into one stream.

Why this exists: the Python sidecar runners (GLiNER, OpenAI Privacy Filter)
emit NER-only findings. The Go bench's gliner variants emit findings from
**both** patterns AND NER because anonde's analyzer runs the full registry.
That makes a direct compare unfair — the sidecar engines look much worse
because they lack the patterns leg.

This merger fixes that by union-ing a Python NER sidecar's findings with
the patterns-only Go bench output, dedup on (start, end, type) keeping the
higher score. The result approximates what anonde would produce if the
sidecar's NER were plugged in as the analyzer's NER recognizer.

Caveat: anonde's analyzer also does conflict resolution
(RemoveConflicts=true), which we don't replicate here — overlapping spans
of different types both survive. That makes the merged engine's recall an
optimistic upper bound, not the production result. Useful for "does
swapping the NER move the ceiling?", not for "what would prod look like."

Usage:

    bench/scoring/merge_findings.py \\
        --in bench/corpora/openmed/data/anonde_patterns.jsonl \\
        --in bench/corpora/openmed/data/anonde_glinerpii.jsonl \\
        --out bench/corpora/openmed/data/anonde_patterns+glinerpii.jsonl \\
        --engine patterns+glinerpii
"""

from __future__ import annotations

import argparse
import json
import sys
from collections import defaultdict
from pathlib import Path


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--in", dest="inps", action="append", required=True,
                    help="repeat: --in path/to/findings.jsonl")
    ap.add_argument("--out", required=True)
    ap.add_argument("--engine", default="merged",
                    help="engine label written to each output line")
    args = ap.parse_args()

    if len(args.inps) < 2:
        print("merge_findings needs at least two --in files", file=sys.stderr)
        return 2

    # Load all inputs into {doc_id: list[finding]} dicts, then merge.
    per_doc: dict[str, list[dict]] = defaultdict(list)
    per_doc_dur: dict[str, float] = defaultdict(float)
    for path in args.inps:
        with Path(path).open("r", encoding="utf-8") as fh:
            for line in fh:
                line = line.strip()
                if not line:
                    continue
                obj = json.loads(line)
                doc_id = obj.get("id", "")
                per_doc[doc_id].extend(obj.get("findings") or [])
                per_doc_dur[doc_id] += float(obj.get("duration_ms") or 0.0)

    # Dedup: keep highest-score finding per (start, end, type).
    out_path = Path(args.out)
    out_path.parent.mkdir(parents=True, exist_ok=True)
    with out_path.open("w", encoding="utf-8") as fh:
        for doc_id, findings in per_doc.items():
            keyed: dict[tuple[int, int, str], dict] = {}
            for f in findings:
                key = (int(f["start"]), int(f["end"]), str(f["type"]))
                if key not in keyed or float(f.get("score", 0)) > float(keyed[key].get("score", 0)):
                    keyed[key] = f
            fh.write(json.dumps({
                "id": doc_id,
                "engine": args.engine,
                "findings": list(keyed.values()),
                "duration_ms": per_doc_dur[doc_id],
            }, ensure_ascii=False) + "\n")

    print(f"wrote {out_path} from {len(args.inps)} inputs, "
          f"{len(per_doc)} docs", file=sys.stderr)
    return 0


if __name__ == "__main__":
    sys.exit(main())
