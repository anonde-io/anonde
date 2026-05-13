#!/usr/bin/env python3
"""runner_openai_privacy_filter — emit anonde-shaped findings.jsonl
from the openai/privacy-filter model (transformers token-classification).

Why a sidecar: the published ONNX export uses decoder-style ops
(SkipSimplifiedLayerNormalization, …) the pure-Go onnxruntime backend
(hugot) does not implement, so anonde's in-process NER pipeline can't load
it. Running it via transformers in a Python sidecar lets us score it on
the same gold corpus and feed compare.py the same finding schema.

Output schema (per line):

    {"id": "...", "engine": "openai-pf", "findings": [
        {"start": int, "end": int, "type": "PERSON|...", "score": float}
    ], "duration_ms": float}

Offsets are CODEPOINT indices (Python convention).

Cost note: first run pulls model.safetensors (~2.8 GB) and tokenizer.json
(~28 MB) from HuggingFace. Subsequent runs are local. Expect ~50–200 ms per
doc on CPU; chunking long docs is needed because the model has a fixed
context window — done here by `pipeline(..., stride=..., aggregation=)`.

Usage:

    .venv-bench/bin/python bench/runners/openai_pf.py \\
        --in bench/corpora/openmed/data/corpus.jsonl \\
        --out bench/corpora/openmed/data/anonde_openaipf.jsonl
"""

from __future__ import annotations

import argparse
import json
import sys
import time
import unicodedata
from pathlib import Path

# OpenAI Privacy Filter emits 8 categories (per the model card / blog):
# NAME, ADDRESS, EMAIL, PHONE, URL, DATE, ACCOUNT_NUMBER, SECRET.
# Map them to anonde canonical types used by compare.py + label_map.yaml.
# Labels appear in the model's `id2label` config; the keys here are
# upper-cased to be tolerant of either case.
LABEL_TO_CANONICAL: dict[str, str] = {
    "NAME":           "PERSON",
    "PERSON":         "PERSON",
    "ADDRESS":        "ADDRESS",
    "EMAIL":          "EMAIL_ADDRESS",
    "PHONE":          "PHONE_NUMBER",
    "URL":            "URL",
    "DATE":           "DATE_TIME",
    "ACCOUNT_NUMBER": "ID",
    "SECRET":         "ID",
}


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--in", dest="inp", required=True)
    ap.add_argument("--out", required=True)
    ap.add_argument("--model", default="openai/privacy-filter")
    ap.add_argument("--engine-label", default="openai-pf")
    ap.add_argument("--max-length", type=int, default=512,
                    help="model context window in tokens (for chunking)")
    ap.add_argument("--stride", type=int, default=64,
                    help="overlap tokens between chunks")
    ap.add_argument("--aggregation", default="simple",
                    choices=("none", "simple", "first", "average", "max"),
                    help="HF pipeline aggregation strategy for sub-tokens")
    ap.add_argument("--threshold", type=float, default=0.5,
                    help="drop entities with score below this")
    args = ap.parse_args()

    try:
        from transformers import (  # type: ignore
            AutoModelForTokenClassification,
            AutoTokenizer,
            pipeline,
        )
        import torch  # type: ignore
    except ImportError as e:
        print(f"transformers/torch not installed: {e}\n"
              f"Install: .venv-bench/bin/pip install transformers torch",
              file=sys.stderr)
        return 2

    print(f"loading {args.model} (first run downloads ~2.8 GB safetensors)…",
          file=sys.stderr)
    t0 = time.perf_counter()
    # OpenAI Privacy Filter ships a custom tokenizer class (TokenizersBackend)
    # not registered in transformers' AutoTokenizer mapping. trust_remote_code
    # lets transformers run the repo's tokenizer code to register it.
    tok = AutoTokenizer.from_pretrained(args.model, trust_remote_code=True)
    mdl = AutoModelForTokenClassification.from_pretrained(args.model, trust_remote_code=True)
    mdl.eval()
    # Print id2label so the operator can see the model's actual label
    # vocabulary — useful when the LABEL_TO_CANONICAL map looks too sparse.
    id2label = getattr(mdl.config, "id2label", {}) or {}
    print(f"id2label = {id2label}", file=sys.stderr)
    pipe = pipeline(
        task="token-classification",
        model=mdl, tokenizer=tok,
        aggregation_strategy=args.aggregation,
        device=-1,  # CPU; flip to torch.cuda.current_device() on GPU box
    )
    print(f"model ready in {(time.perf_counter()-t0)*1000:.0f} ms",
          file=sys.stderr)

    inp = Path(args.inp)
    out = Path(args.out)
    out.parent.mkdir(parents=True, exist_ok=True)

    n_docs = 0
    n_findings = 0
    with inp.open("r", encoding="utf-8") as fin, out.open("w", encoding="utf-8") as fout:
        for line in fin:
            line = line.strip()
            if not line:
                continue
            try:
                doc = json.loads(line)
            except json.JSONDecodeError as e:
                print(f"skip malformed line: {e}", file=sys.stderr)
                continue
            doc_id = doc.get("id", "")
            text = unicodedata.normalize("NFC", doc.get("text", ""))
            if not text:
                continue

            t1 = time.perf_counter()
            findings = []
            try:
                # Chunked inference. The HF token-classification pipeline
                # doesn't natively chunk for token-level tasks the way it
                # does for QA, so we manually slide a window with `stride`
                # tokens of overlap, then dedupe by (start, end, type).
                results = run_chunked(
                    pipe, tok, text,
                    max_length=args.max_length, stride=args.stride,
                )
            except Exception as e:
                print(f"predict failed id={doc_id}: {e}", file=sys.stderr)
                results = []
            dur_ms = (time.perf_counter() - t1) * 1000.0

            seen: set[tuple[int, int, str]] = set()
            for ent in results:
                if ent["score"] < args.threshold:
                    continue
                raw = ent.get("entity_group") or ent.get("entity") or ""
                canonical = LABEL_TO_CANONICAL.get(raw.upper())
                if not canonical:
                    continue
                s, e = int(ent["start"]), int(ent["end"])
                key = (s, e, canonical)
                if key in seen:
                    continue
                seen.add(key)
                findings.append({
                    "start": s,
                    "end": e,
                    "type": canonical,
                    "score": float(ent["score"]),
                })

            fout.write(json.dumps({
                "id": doc_id,
                "engine": args.engine_label,
                "findings": findings,
                "duration_ms": dur_ms,
            }, ensure_ascii=False) + "\n")
            n_docs += 1
            n_findings += len(findings)
            print(f"doc={n_docs} id={doc_id} spans={len(findings)} dur={dur_ms:.0f}ms",
                  file=sys.stderr)

    print(f"processed {n_docs} docs, {n_findings} findings -> {out}", file=sys.stderr)
    return 0


def run_chunked(pipe, tok, text: str, *, max_length: int, stride: int):
    """Slide an `max_length`-token window over `text` and aggregate
    pipeline outputs across windows. Offsets returned by the pipeline are
    relative to the full text (transformers ≥4.30 handles this correctly
    when we feed character spans), so dedup by (start, end, group) is safe.
    """
    if len(text) <= max_length * 4:
        return pipe(text)

    enc = tok(text, return_offsets_mapping=True, add_special_tokens=False,
              truncation=False)
    offsets = enc["offset_mapping"]
    n = len(offsets)
    step = max(1, max_length - stride)

    out = []
    i = 0
    while i < n:
        j = min(n, i + max_length)
        if i >= j:
            break
        char_start = offsets[i][0]
        char_end = offsets[j - 1][1]
        chunk = text[char_start:char_end]
        for ent in pipe(chunk):
            ent["start"] = int(ent["start"]) + char_start
            ent["end"] = int(ent["end"]) + char_start
            out.append(ent)
        if j == n:
            break
        i += step
    return out


if __name__ == "__main__":
    sys.exit(main())
