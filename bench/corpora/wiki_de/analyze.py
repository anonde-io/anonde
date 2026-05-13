#!/usr/bin/env python3
"""Summarise anonde findings on a precision-probe corpus (no PHI gold).

Any finding here is by construction a false positive (or rarely, a real
leak the corpus author overlooked). Report counts + sample for human
review.
"""

from __future__ import annotations

import argparse
import csv
import json
import statistics
import sys
from collections import Counter, defaultdict
from pathlib import Path


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--corpus", required=True)
    ap.add_argument("--anonde", required=True)
    ap.add_argument("--out", required=True)
    ap.add_argument("--csv", default="")
    ap.add_argument("--samples", type=int, default=30,
                    help="how many sample findings to include in the report")
    ap.add_argument("--title", default="Wikipedia DE precision probe",
                    help="title used in REPORT.md")
    ap.add_argument("--source", default="German medical Wikipedia articles",
                    help="short description of the corpus shown in the header")
    args = ap.parse_args()

    docs = {}
    with open(args.corpus, encoding="utf-8") as f:
        for line in f:
            line = line.strip()
            if not line:
                continue
            d = json.loads(line)
            docs[d["id"]] = d

    findings_by_doc = defaultdict(list)
    durations = []
    with open(args.anonde, encoding="utf-8") as f:
        for line in f:
            line = line.strip()
            if not line:
                continue
            d = json.loads(line)
            findings_by_doc[d["id"]] = d.get("findings", [])
            if "duration_ms" in d:
                durations.append(float(d["duration_ms"]))

    type_counts: Counter[str] = Counter()
    docs_with_findings = 0
    findings_per_doc: list[int] = []
    sample_findings = []
    for doc_id, doc in docs.items():
        f = findings_by_doc.get(doc_id, [])
        findings_per_doc.append(len(f))
        if f:
            docs_with_findings += 1
        for entry in f:
            type_counts[entry["type"]] += 1
            if len(sample_findings) < args.samples:
                start, end = int(entry["start"]), int(entry["end"])
                surface = doc["text"][start:end] if 0 <= start < end <= len(doc["text"]) else ""
                sample_findings.append(
                    (doc_id, entry["type"], surface, float(entry.get("score", 0)))
                )

    total = sum(findings_per_doc)
    avg = total / max(len(docs), 1)
    out_lines = []
    out_lines.append(f"# {args.title}\n")
    out_lines.append(
        f"anonde run against **{len(docs)}** {args.source}. "
        f"Corpus has no PHI by design — any finding here is suspicious "
        f"(almost certainly a false positive).\n"
    )

    out_lines.append("## Headline\n")
    out_lines.append(f"- **{total}** total findings across {len(docs)} docs ({avg:.1f} per doc)")
    out_lines.append(
        f"- **{docs_with_findings}** of {len(docs)} docs ({docs_with_findings/max(len(docs),1)*100:.1f}%) "
        f"had at least one finding"
    )
    if findings_per_doc:
        out_lines.append(
            f"- per-doc findings: median {statistics.median(findings_per_doc):.0f}, "
            f"max {max(findings_per_doc)}"
        )
    if durations:
        ds = sorted(durations)
        out_lines.append(
            f"- latency: median {statistics.median(ds):.2f} ms, "
            f"p99 {ds[max(0, int(0.99*len(ds))-1)]:.2f} ms"
        )
    out_lines.append("")

    out_lines.append("## Findings by type\n")
    out_lines.append("| Type | Count | % of total |")
    out_lines.append("|---|---:|---:|")
    for typ, cnt in type_counts.most_common():
        pct = cnt / max(total, 1) * 100
        out_lines.append(f"| {typ} | {cnt} | {pct:.1f}% |")
    out_lines.append("")

    out_lines.append(f"## Sample of findings (first {args.samples})\n")
    out_lines.append("Review for systematic FP patterns.\n")
    out_lines.append("| Doc | Type | Surface | Score |")
    out_lines.append("|---|---|---|---:|")
    for doc_id, typ, surface, score in sample_findings:
        clean = surface.replace("|", "\\|").replace("\n", " ")
        if len(clean) > 60:
            clean = clean[:57] + "…"
        out_lines.append(f"| {doc_id[:30]} | {typ} | `{clean}` | {score:.2f} |")
    out_lines.append("")

    Path(args.out).write_text("\n".join(out_lines), encoding="utf-8")
    print(f"wrote {args.out}", file=sys.stderr)

    if args.csv:
        with open(args.csv, "w", newline="", encoding="utf-8") as fh:
            w = csv.writer(fh)
            w.writerow(["doc_id", "type", "start", "end", "score", "surface"])
            for doc_id, doc in docs.items():
                for entry in findings_by_doc.get(doc_id, []):
                    s, e = int(entry["start"]), int(entry["end"])
                    surface = doc["text"][s:e] if 0 <= s < e <= len(doc["text"]) else ""
                    w.writerow([doc_id, entry["type"], s, e,
                                f"{float(entry.get('score', 0)):.4f}",
                                surface.replace("\n", " ")[:120]])
        print(f"wrote {args.csv}", file=sys.stderr)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
