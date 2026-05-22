#!/usr/bin/env python3
"""Runs Presidio Analyzer over a corpus JSONL and emits matching JSONL.

Schema: see bench/corpora/ai4privacy_en/README.md.

Multilingual: --language picks the spaCy NLP model.

  en -> en_core_web_lg (or en_core_web_trf with --engine transformer)
  de -> de_core_news_lg
  es -> es_core_news_lg
  fr -> fr_core_news_lg
  it -> it_core_news_lg

The matching spaCy model must be installed locally:

    pip install presidio-analyzer spacy
    python -m spacy download en_core_web_lg
    python -m spacy download de_core_news_lg
    python -m spacy download es_core_news_lg
    python -m spacy download fr_core_news_lg
    python -m spacy download it_core_news_lg

Run:
    python bench/runners/presidio.py \\
      --in  data/corpus.jsonl \\
      --out data/presidio.jsonl \\
      --language de
"""

from __future__ import annotations

import argparse
import json
import sys
import time
from pathlib import Path


# Default spaCy model per language. Presidio's NlpEngineProvider needs
# a `model_name` that resolves on disk; the *_core_news_lg / *_core_web_lg
# packages are the standard mid-size production-grade models for each.
DEFAULT_MODEL_BY_LANG: dict[str, str] = {
    "en": "en_core_web_lg",
    "de": "de_core_news_lg",
    "es": "es_core_news_lg",
    "fr": "fr_core_news_lg",
    "it": "it_core_news_lg",
}


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--in", dest="in_path", required=True)
    parser.add_argument("--out", dest="out_path", required=True)
    parser.add_argument("--threshold", type=float, default=0.5)
    parser.add_argument(
        "--language",
        choices=tuple(DEFAULT_MODEL_BY_LANG.keys()),
        default="en",
        help="Corpus language; selects spaCy model + Presidio analyzer locale.",
    )
    parser.add_argument(
        "--engine",
        choices=("default", "transformer"),
        default="default",
        help="EN-only knob: default = *_core_web_lg, transformer = en_core_web_trf. "
             "Ignored for non-EN languages.",
    )
    parser.add_argument(
        "--model",
        default="",
        help="Override the spaCy model id (e.g. de_core_news_md). "
             "Empty = use the language default.",
    )
    args = parser.parse_args()

    try:
        from presidio_analyzer import AnalyzerEngine
        from presidio_analyzer.nlp_engine import NlpEngineProvider
    except ImportError as exc:
        print(f"presidio-analyzer not installed: {exc}", file=sys.stderr)
        return 2

    model_name = args.model.strip()
    if not model_name:
        if args.language == "en" and args.engine == "transformer":
            model_name = "en_core_web_trf"
        else:
            model_name = DEFAULT_MODEL_BY_LANG[args.language]

    # Sanity-probe the model before launching the analyzer — Presidio's
    # init error path is noisy. A clean exit-2 here makes the cell skip
    # gracefully in the matrix renderer.
    try:
        import spacy  # noqa: F401
        import importlib
        importlib.import_module(model_name)
    except ImportError:
        print(
            f"spaCy model {model_name!r} not installed. Run:\n"
            f"    python -m spacy download {model_name}",
            file=sys.stderr,
        )
        return 2

    nlp_config = {
        "nlp_engine_name": "spacy",
        "models": [{"lang_code": args.language, "model_name": model_name}],
    }
    provider = NlpEngineProvider(nlp_configuration=nlp_config)
    nlp_engine = provider.create_engine()
    analyzer = AnalyzerEngine(
        nlp_engine=nlp_engine, supported_languages=[args.language],
    )

    in_path = Path(args.in_path)
    out_path = Path(args.out_path)
    out_path.parent.mkdir(parents=True, exist_ok=True)
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
                language=args.language,
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
            # Engine label encodes language + variant so downstream
            # report can distinguish presidio-en from presidio-de.
            variant = args.engine if args.language == "en" else "default"
            engine_label = f"presidio-{args.language}-{variant}"
            fout.write(
                json.dumps(
                    {
                        "id": doc["id"],
                        "engine": engine_label,
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
