#!/usr/bin/env python3
"""Fetch the MAPA legal/administrative anonymisation corpus from
HuggingFace and convert it into the span-based JSONL schema this
benchmark expects.

MAPA (Multilingual Anonymisation for Public Administration) is an
EU-funded, anonymisation-grade NER corpus of legal / administrative
text (EUR-Lex judgments + national public-administration documents).
It is one of the datasets in the LEXTREME legal-NLP benchmark. The HF
dataset id is `joelniklaus/mapa` (CC-BY-4.0, single `default` config,
`train`/`validation`/`test` splits, ~42 k sentences across 24 EU
languages tagged by a per-row ISO-code `language` column).

This is the SHARED loader for every mapa_* corpus. The five wired
corpus dirs (mapa_en/de/es/fr/it) all call this one script with
`--language <code>` to keep their slice; an unsupported `--language`
exits 2 cleanly (skipped by the matrix renderer, never an empty
corpus).

--- COARSE vs FINE: why we build gold from the COARSE layer ----------

MAPA ships a two-level annotation: a `coarse_grained` BIO column and a
`fine_grained` BIO column. The brief assumed the fine layer is a clean
leaf decomposition of the coarse layer (given name / family name /
title under PERSON, etc.). It is NOT — probing the live dataset showed:

  * There is NO `GIVEN NAME` label anywhere in MAPA. Given names are
    `B-/I-PERSON` at the coarse level but plain `O` at the fine level.
    ~12 % of coarse PERSON tokens (and ~25 % of ALL coarse-entity
    tokens) are fine==O — entirely uncovered by the fine layer.
  * The fine layer is structurally CROSS-CUTTING, not nested: a
    `FAMILY NAME` span turns up inside a coarse `DATE` or
    `ORGANISATION`; `CITY`/`COUNTRY` turn up inside a coarse `PERSON`.
    The fine layer tags sub-mentions, not leaves of the coarse span.

Building gold from fine-grained spans would therefore (a) drop every
given name — a recall hole no PII bench can accept — and (b) emit
nonsensical spans (a `FAMILY NAME` sitting inside a `DATE`). The
coarse layer is MAPA's actual anonymisation layer and the only
complete one, so this loader builds gold from `coarse_grained`. The
fine layer is left informational.

The emitted coarse types are: PERSON, ORGANISATION, ADDRESS, DATE,
AMOUNT. They are mapped to anonde/Presidio-canonical type strings here
so they line up 1:1 with the `mapa` `gold:` section of
bench/scoring/label_map.yaml. AMOUNT (monetary value) is intentionally
NOT emitted — anonde's rule is "monetary amounts are not PII"; dropping
it loader-side keeps it out of both recall and leak-rate scoring.

Usage:
    fetch_mapa.py --out <corpus>/data/corpus.jsonl \\
        [--language en|de|es|fr|it] [--max 5000] [--split test]
"""

from __future__ import annotations

import argparse
import json
import sys
from pathlib import Path
from typing import Iterable

DATASET_ID = "joelniklaus/mapa"

# MAPA coarse-grained entity type → anonde/Presidio-canonical type.
#
# These are the five coarse classes MAPA uses (verified against the
# live en/de/es/fr/it slices). AMOUNT is deliberately absent: monetary
# amounts are not PII in anonde's vocabulary, so AMOUNT spans are
# dropped at the loader rather than routed through gold. Any coarse tag
# not in this map is skipped (treated as a non-PII token).
COARSE_MAP = {
    "PERSON": "PERSON",
    "ORGANISATION": "ORGANIZATION",   # MAPA spells it the EU/BrE way
    "ADDRESS": "ADDRESS",
    "DATE": "DATE_TIME",
    # "AMOUNT": dropped — monetary value, not PII.
}

# MAPA covers 24 EU languages; the bench wires the five target-language
# corpus dirs below. A loader invoked with a --language outside this
# set exits 2 (cleanly skipped by the matrix renderer) rather than
# silently writing an empty corpus. MAPA's per-row `language` column
# already stores ISO codes, so no name translation is needed (unlike
# the ai4privacy loader).
SUPPORTED_LANGUAGES = ("en", "de", "es", "fr", "it")


def bio_base(tag: str) -> tuple[str | None, str | None]:
    """Split a BIO tag into (B|I, base-type). 'O' → (None, None)."""
    if not tag or tag == "O" or "-" not in tag:
        return (None, None)
    bi, rest = tag.split("-", 1)
    return (bi, rest)


def spans_from_bio(tokens: list[str], tags: list[str]) -> tuple[str, list[dict]]:
    """Rebuild the sentence text + char-offset entity spans from a
    token list and a coarse BIO tag list.

    Text is rebuilt by joining tokens with single spaces — the same
    convention used by wikiann_de / germeval_14 / the ai4privacy BIO
    path, and what anonde's regex recognizers see. Offsets are
    codepoint indices into that joined string.

    A span starts at B-X and runs over contiguous I-X tags of the same
    base type. An I-X with no matching open span, or following a
    different base type, opens a fresh span (MAPA's coarse BIO is
    well-formed in practice, but we tolerate stray I- tags rather than
    drop a PII token)."""
    text_parts: list[str] = []
    tok_starts: list[int] = []
    cursor = 0
    for ti, tok in enumerate(tokens):
        if ti > 0:
            text_parts.append(" ")
            cursor += 1
        tok_starts.append(cursor)
        text_parts.append(tok)
        cursor += len(tok)
    text = "".join(text_parts)
    tok_ends = [s + len(tokens[i]) for i, s in enumerate(tok_starts)]

    entities: list[dict] = []
    i = 0
    n = len(tags)
    while i < n:
        bi, base = bio_base(tags[i])
        if base is None:
            i += 1
            continue
        start_tok = i
        j = i + 1
        while j < n:
            nbi, nbase = bio_base(tags[j])
            if nbi == "I" and nbase == base:
                j += 1
                continue
            break
        mapped = COARSE_MAP.get(base)
        if mapped is not None:
            entities.append({
                "start": tok_starts[start_tok],
                "end": tok_ends[j - 1],
                "type": mapped,
            })
        i = j

    return text, entities


def iter_examples(split: str, max_n: int | None,
                  language: str) -> Iterable[dict]:
    try:
        from datasets import load_dataset, get_dataset_split_names
    except ImportError as exc:
        print(f"datasets not installed: {exc}", file=sys.stderr)
        sys.exit(2)

    # MAPA ships train/validation/test. We try the requested split
    # first, then fall back to the first split that exists — keeping
    # older invocations working without forcing the caller to know the
    # upstream layout. `test` is the default (canonical eval split).
    try:
        available = get_dataset_split_names(DATASET_ID)
    except Exception as exc:  # noqa: BLE001 — network / gated access
        print(f"could not resolve splits for {DATASET_ID}: {exc}",
              file=sys.stderr)
        sys.exit(2)
    effective = split
    if split not in available:
        if not available:
            print(f"no splits available for {DATASET_ID}", file=sys.stderr)
            sys.exit(2)
        effective = available[0]
        print(f"split {split!r} not found in {available!r}; "
              f"falling back to {effective!r}", file=sys.stderr)

    ds = load_dataset(DATASET_ID, split=effective, streaming=True)
    want_lang = language.strip().lower()
    n = 0
    for i, row in enumerate(ds):
        if max_n is not None and n >= max_n:
            return
        # MAPA interleaves all 24 languages in each split, tagged by an
        # ISO-code `language` column. Keep only this corpus's slice.
        row_lang = str(row.get("language", "")).strip().lower()
        if row_lang != want_lang:
            continue
        tokens = row.get("tokens") or []
        tags = row.get("coarse_grained") or []
        if not tokens or len(tokens) != len(tags):
            continue
        text, entities = spans_from_bio(tokens, tags)
        # Emit every in-language sentence, including those with no PII
        # span — they are valid negatives that keep the precision /
        # leak-rate denominator honest. (Other span corpora here do the
        # same.)
        fname = str(row.get("file_name", f"row-{i}"))
        snum = row.get("sentence_number", i)
        yield {"id": f"mapa-{want_lang}-{fname}-{snum}",
               "text": text, "entities": entities}
        n += 1


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--out", required=True)
    parser.add_argument("--split", default="test",
                        help="dataset split (test is the canonical eval "
                             "split; train is the full set)")
    parser.add_argument("--max", type=int, default=5000,
                        help="max docs to write (default 5000)")
    parser.add_argument("--language", default="en",
                        help="keep only this language slice "
                             "(en/de/es/fr/it). MAPA covers 24 EU "
                             "languages; the bench wires these five.")
    args = parser.parse_args()

    want_lang = args.language.strip().lower()
    if want_lang not in SUPPORTED_LANGUAGES:
        print(f"language {want_lang!r} not wired for MAPA "
              f"(supported: {', '.join(SUPPORTED_LANGUAGES)})",
              file=sys.stderr)
        return 2

    out = Path(args.out)
    out.parent.mkdir(parents=True, exist_ok=True)

    written = 0
    n_spans = 0
    with out.open("w", encoding="utf-8") as fout:
        for ex in iter_examples(args.split, args.max, want_lang):
            fout.write(json.dumps(ex, ensure_ascii=False) + "\n")
            written += 1
            n_spans += len(ex["entities"])
    print(f"wrote {written} docs ({n_spans} gold spans) to {out}",
          file=sys.stderr)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
