#!/usr/bin/env python3
"""Runs Presidio Analyzer over a corpus JSONL and emits matching JSONL.

Schema: see bench/corpora/ai4privacy_en/README.md.

Usage:
    pip install presidio-analyzer spacy
    python -m spacy download en_core_web_lg
    python bench/runners/presidio.py --in data/corpus.jsonl --out data/presidio.jsonl
"""

from __future__ import annotations

import argparse
import json
import sys
import time
from pathlib import Path


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--in", dest="in_path", required=True)
    parser.add_argument("--out", dest="out_path", required=True)
    parser.add_argument("--threshold", type=float, default=0.5)
    parser.add_argument(
        "--engine",
        choices=("default", "transformer"),
        default="default",
        help="Presidio NLP engine: default = en_core_web_lg, transformer = en_core_web_trf",
    )
    args = parser.parse_args()

    try:
        from presidio_analyzer import AnalyzerEngine
        from presidio_analyzer.nlp_engine import NlpEngineProvider
    except ImportError as exc:
        print(f"presidio-analyzer not installed: {exc}", file=sys.stderr)
        return 2

    nlp_config = {
        "nlp_engine_name": "spacy",
        "models": [
            {
                "lang_code": "en",
                "model_name": "en_core_web_trf"
                if args.engine == "transformer"
                else "en_core_web_lg",
            }
        ],
    }
    provider = NlpEngineProvider(nlp_configuration=nlp_config)
    nlp_engine = provider.create_engine()
    analyzer = AnalyzerEngine(nlp_engine=nlp_engine, supported_languages=["en"])

    in_path = Path(args.in_path)
    out_path = Path(args.out_path)
    with in_path.open() as fin, out_path.open("w") as fout:
        for line in fin:
            line = line.strip()
            if not line:
                continue
            try:
                doc = json.loads(line)
            except json.JSONDecodeError as exc:
                print(f"skip malformed line: {exc}", file=sys.stderr)
                continue

            text = doc["text"]
            start = time.perf_counter()
            results = analyzer.analyze(
                text=text,
                language="en",
                score_threshold=args.threshold,
            )
            duration_ms = (time.perf_counter() - start) * 1000.0

            findings = [
                {
                    "start": int(r.start),
                    "end": int(r.end),
                    "type": r.entity_type,
                    "score": float(r.score),
                }
                for r in results
            ]
            fout.write(
                json.dumps(
                    {
                        "id": doc["id"],
                        "engine": f"presidio-{args.engine}",
                        "findings": findings,
                        "duration_ms": duration_ms,
                    },
                    ensure_ascii=False,
                )
                + "\n"
            )
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
