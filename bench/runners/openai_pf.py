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

Dependency note: openai/privacy-filter is a transformers-5.6+ NATIVE
architecture (config.json model_type "openai_privacy_filter"). It is NOT
remote code — there is no modeling_*.py and no `auto_map` in config.json,
so trust_remote_code does nothing. transformers < 5.6 fails to load it
with `KeyError: 'openai_privacy_filter'`. The shared bench venv pins
transformers < 5.2 (gliner==0.2.26's cap), so this runner needs its OWN
venv built from bench/requirements-openai-pf.txt — the bench Makefile's
CELL_openai_pf creates and uses .venv-openai-pf for exactly this reason.

Cost note: first run pulls model.safetensors (~2.8 GB) and tokenizer.json
(~28 MB) from HuggingFace. Subsequent runs are local. Expect ~50–200 ms per
doc on CPU; chunking long docs is needed because the model has a fixed
context window — done here by `pipeline(..., stride=..., aggregation=)`.

Usage:

    python -m venv bench/.venv-openai-pf
    bench/.venv-openai-pf/bin/pip install -r bench/requirements-openai-pf.txt
    bench/.venv-openai-pf/bin/python bench/runners/openai_pf.py \\
        --in bench/corpora/openmed/data/corpus.jsonl \\
        --out bench/corpora/openmed/data/anonde_openai-pf.jsonl \\
        --max-docs 40

`--max-docs N` scores only the first N docs in deterministic id order, so
the matrix can include openai-pf as a column without paying ~80 s/doc over
the whole corpus. render_matrix.py scores each engine over the docs it
actually returned, so a 40-of-512 run is not penalised as a 92% leak.
"""

from __future__ import annotations

import argparse
import json
import sys
import time
import unicodedata
from pathlib import Path

# OpenAI Privacy Filter emits 8 categories. The model's config.json
# id2label uses BIES tagging over lowercase labels:
#   private_person, private_address, private_email, private_phone,
#   private_url, private_date, account_number, secret
# (verified against openai/privacy-filter config.json, 2026-05).
# With aggregation_strategy != "none" the HF pipeline strips the
# B-/I-/E-/S- prefix and returns these bare names in `entity_group`.
# Map them to anonde canonical types used by compare.py + label_map.yaml.
# Keys are matched case-insensitively (see _canonical below); the older
# uppercase NAME/EMAIL/... aliases are kept so an alternate checkpoint or
# fine-tune with differently-cased labels still resolves.
LABEL_TO_CANONICAL: dict[str, str] = {
    "PRIVATE_PERSON":  "PERSON",
    "PRIVATE_ADDRESS": "ADDRESS",
    "PRIVATE_EMAIL":   "EMAIL_ADDRESS",
    "PRIVATE_PHONE":   "PHONE_NUMBER",
    "PRIVATE_URL":     "URL",
    "PRIVATE_DATE":    "DATE_TIME",
    "ACCOUNT_NUMBER":  "ID",
    "SECRET":          "ID",
    # legacy / alternate-checkpoint aliases
    "NAME":            "PERSON",
    "PERSON":          "PERSON",
    "ADDRESS":         "ADDRESS",
    "EMAIL":           "EMAIL_ADDRESS",
    "PHONE":           "PHONE_NUMBER",
    "URL":             "URL",
    "DATE":            "DATE_TIME",
}


def _canonical(raw: str) -> str | None:
    """Resolve a model entity label to an anonde canonical type.

    Tolerant of BIES prefixes that survive when aggregation is "none"
    (e.g. "B-private_person") and of either label casing.
    """
    name = raw.upper().strip()
    if len(name) > 2 and name[1] == "-" and name[0] in ("B", "I", "E", "S"):
        name = name[2:]
    return LABEL_TO_CANONICAL.get(name)


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
    ap.add_argument("--max-docs", type=int, default=0,
                    help="when >0, score only the first N docs in "
                         "deterministic order (sorted by doc id). Reruns are "
                         "stable. openai-pf is ~80 s/doc on CPU, so the matrix "
                         "scores it on a fixed subsample rather than the whole "
                         "corpus — see render_matrix.py partial-doc scoring.")
    args = ap.parse_args()

    try:
        import transformers  # type: ignore
        from transformers import (  # type: ignore
            AutoModelForTokenClassification,
            AutoTokenizer,
            pipeline,
        )
        import torch  # type: ignore
    except ImportError as e:
        print(f"transformers/torch not installed: {e}\n"
              f"openai-pf needs its OWN venv (transformers>=5.6 — the model\n"
              f"is a transformers-5.6 native architecture, incompatible with\n"
              f"the gliner-pinned transformers<5.2 in bench/requirements.txt).\n"
              f"Install: python -m venv .venv-openai-pf && \\\n"
              f"  .venv-openai-pf/bin/pip install -r bench/requirements-openai-pf.txt",
              file=sys.stderr)
        return 2

    # Preflight: openai/privacy-filter declares model_type
    # "openai_privacy_filter" / architecture
    # "OpenAIPrivacyFilterForTokenClassification". That architecture is
    # NATIVE to transformers and only exists in >= 5.6.0. The repo ships no
    # custom modeling code and no `auto_map` in config.json, so
    # trust_remote_code cannot help — older transformers fail with
    # `KeyError: 'openai_privacy_filter'` deep inside from_pretrained.
    # Catch it here with an actionable message instead.
    tv = tuple(int(p) for p in transformers.__version__.split(".")[:2]
               if p.isdigit())
    if args.model.startswith("openai/privacy-filter") and tv and tv < (5, 6):
        print(f"transformers {transformers.__version__} is too old for "
              f"{args.model}.\n"
              f"The 'openai_privacy_filter' architecture needs transformers"
              f">=5.6.0.\n"
              f"This venv is likely the shared bench venv (gliner pins "
              f"transformers<5.2).\n"
              f"Build the isolated openai-pf venv instead:\n"
              f"  python -m venv .venv-openai-pf && \\\n"
              f"  .venv-openai-pf/bin/pip install -r "
              f"bench/requirements-openai-pf.txt\n"
              f"and run this script with .venv-openai-pf/bin/python.",
              file=sys.stderr)
        return 2

    print(f"loading {args.model} (first run downloads ~2.8 GB safetensors)…",
          file=sys.stderr)
    t0 = time.perf_counter()
    # No trust_remote_code: openai/privacy-filter has no auto_map / remote
    # modeling code. Both the model architecture and the tokenizer class
    # (TokenizersBackend) are built into transformers >= 5.6 — the version
    # preflight above guarantees we are on such a build.
    tok = AutoTokenizer.from_pretrained(args.model)
    mdl = AutoModelForTokenClassification.from_pretrained(args.model)
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

    # Load every doc up front. openai-pf is ~80 s/doc, so for the matrix we
    # score only a deterministic subsample (--max-docs N): sort by doc id and
    # take the first N. Sorting (not the file order) makes reruns stable even
    # if the corpus loader reorders lines. The subsample's findings are the
    # ONLY docs written to the output JSONL — render_matrix.py then scores
    # openai-pf over just the docs it actually returned (intersection of gold
    # ids ∩ pred ids), so a partial run does not show as a fake leak.
    docs: list[dict] = []
    with inp.open("r", encoding="utf-8") as fin:
        for line in fin:
            line = line.strip()
            if not line:
                continue
            try:
                docs.append(json.loads(line))
            except json.JSONDecodeError as e:
                print(f"skip malformed line: {e}", file=sys.stderr)
                continue

    if args.max_docs and args.max_docs > 0:
        docs.sort(key=lambda d: str(d.get("id", "")))
        before = len(docs)
        docs = docs[:args.max_docs]
        print(f"--max-docs {args.max_docs}: scoring {len(docs)}/{before} docs "
              f"(deterministic, sorted by id)", file=sys.stderr)

    n_docs = 0
    n_findings = 0
    with out.open("w", encoding="utf-8") as fout:
        for doc in docs:
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
                canonical = _canonical(raw)
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
