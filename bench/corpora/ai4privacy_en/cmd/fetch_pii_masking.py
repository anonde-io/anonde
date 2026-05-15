#!/usr/bin/env python3
"""Fetch ai4privacy/pii-masking-200k from HuggingFace and convert into the
JSONL schema this benchmark expects.

ai4privacy uses BIO-tagged tokens with privacy-mask labels like FIRSTNAME,
LASTNAME, EMAIL, USERNAME, etc. We map those into anonde / Presidio entity
types (PERSON, EMAIL_ADDRESS, etc.) and emit span-based JSONL.

Usage:
    fetch_pii_masking.py --out bench/parity/data/corpus.jsonl [--max 5000] [--split validation]
"""

from __future__ import annotations

import argparse
import json
import sys
from pathlib import Path
from typing import Iterable

# ai4privacy label → our entity type. Anything not in this mapping is dropped
# from gold (treated as a non-PII token) so the comparison is apples-to-apples
# with what anonde/Presidio actually try to detect.
LABEL_MAP = {
    # Personal names
    "FIRSTNAME": "PERSON",
    "LASTNAME": "PERSON",
    "MIDDLENAME": "PERSON",
    "FULLNAME": "PERSON",
    "USERNAME": "PERSON",
    "PREFIX": "PERSON",
    "SUFFIX": "PERSON",
    # Locations
    "CITY": "LOCATION",
    "STATE": "LOCATION",
    "COUNTRY": "LOCATION",
    "STREET": "LOCATION",
    "STREETADDRESS": "LOCATION",
    "BUILDINGNUMBER": "LOCATION",
    "ZIPCODE": "LOCATION",
    "SECONDARYADDRESS": "LOCATION",
    # Organizations
    "COMPANY_NAME": "ORGANIZATION",
    "JOBTITLE": "ORGANIZATION",
    # Communication
    "EMAIL": "EMAIL_ADDRESS",
    "PHONE_NUMBER": "PHONE_NUMBER",
    "PHONENUMBER": "PHONE_NUMBER",
    "URL": "URL",
    "IP": "IP_ADDRESS",
    "IPV4": "IP_ADDRESS",
    "IPV6": "IP_ADDRESS",
    "MAC": "MAC_ADDRESS",
    "MACADDRESS": "MAC_ADDRESS",
    # Financial / IDs
    "CREDITCARDNUMBER": "CREDIT_CARD",
    "CREDITCARD": "CREDIT_CARD",
    "IBAN": "IBAN_CODE",
    "SSN": "US_SSN",
    "BTCADDRESS": "CRYPTO",
    "ETHEREUMADDRESS": "CRYPTO",
}


def fix_label(raw: str) -> str | None:
    if not raw or raw == "O":
        return None
    if raw.startswith(("B-", "I-")):
        raw = raw[2:]
    return LABEL_MAP.get(raw.upper())


def spans_from_bio(tokens: list[str], tags: list[str]) -> list[dict]:
    """Reconstruct entity spans from BIO-tagged tokens. Token boundaries are
    re-derived by joining tokens with single spaces — same convention the
    dataset uses when materializing back to text."""
    out: list[dict] = []
    cursor = 0
    cur_start = -1
    cur_end = -1
    cur_type: str | None = None

    rendered: list[str] = []

    for tok, tag in zip(tokens, tags):
        if rendered:
            rendered.append(" ")
            cursor += 1
        tok_start = cursor
        rendered.append(tok)
        cursor += len(tok)
        tok_end = cursor

        mapped = fix_label(tag)
        is_b = tag.startswith("B-") if tag else False

        if mapped is None:
            if cur_type is not None:
                out.append({"start": cur_start, "end": cur_end, "type": cur_type})
                cur_type = None
            continue

        # Continue an existing span if the type matches and we're inside (I-) or
        # the previous tag also belonged to this entity type.
        if cur_type == mapped and not is_b:
            cur_end = tok_end
            continue

        # Otherwise, close any open span and start a new one.
        if cur_type is not None:
            out.append({"start": cur_start, "end": cur_end, "type": cur_type})
        cur_type = mapped
        cur_start = tok_start
        cur_end = tok_end

    if cur_type is not None:
        out.append({"start": cur_start, "end": cur_end, "type": cur_type})

    return out


def reconstruct_text(tokens: list[str]) -> str:
    return " ".join(tokens)


def iter_examples(split: str, max_n: int | None) -> Iterable[dict]:
    try:
        from datasets import load_dataset, get_dataset_split_names
    except ImportError as exc:
        print(f"datasets not installed: {exc}", file=sys.stderr)
        sys.exit(2)

    # ai4privacy/pii-masking-200k used to ship a `validation` split; as
    # of 2026-05 only `train` is available on the auto-parquet ref. We
    # try the requested split first, then fall back to the first split
    # that does exist — keeping older invocations (`--split validation`)
    # working without forcing the caller to know the upstream layout.
    available = get_dataset_split_names("ai4privacy/pii-masking-200k")
    effective = split
    if split not in available:
        if not available:
            print(f"no splits available for ai4privacy/pii-masking-200k",
                  file=sys.stderr)
            sys.exit(2)
        effective = available[0]
        print(f"split {split!r} not found in {available!r}; "
              f"falling back to {effective!r}", file=sys.stderr)

    ds = load_dataset("ai4privacy/pii-masking-200k", split=effective)
    n = 0
    for i, row in enumerate(ds):
        if max_n is not None and n >= max_n:
            return
        # Two row shapes are common in this dataset family:
        #   - {"source_text": str, "privacy_mask": list[{label, start, end}]}
        #   - {"mbert_text_tokens": [...], "mbert_bio_labels": [...]}
        # We handle both.
        if "source_text" in row and "privacy_mask" in row and row["privacy_mask"]:
            text = row["source_text"]
            entities = []
            for span in row["privacy_mask"]:
                mapped = fix_label(span.get("label", ""))
                if mapped is None:
                    continue
                entities.append(
                    {"start": int(span["start"]), "end": int(span["end"]), "type": mapped}
                )
            if not entities:
                continue
            yield {"id": f"row-{i}", "text": text, "entities": entities}
            n += 1
            continue

        tokens = row.get("mbert_text_tokens") or row.get("tokens")
        tags = row.get("mbert_bio_labels") or row.get("ner_tags")
        if not tokens or not tags or len(tokens) != len(tags):
            continue
        text = reconstruct_text(tokens)
        entities = spans_from_bio(tokens, tags)
        if not entities:
            continue
        yield {"id": f"row-{i}", "text": text, "entities": entities}
        n += 1


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--out", required=True)
    parser.add_argument("--split", default="validation",
                        help="dataset split (validation is small/fast; train is full)")
    parser.add_argument("--max", type=int, default=5000,
                        help="max docs to write (default 5000 for tractable runtime)")
    args = parser.parse_args()

    out = Path(args.out)
    out.parent.mkdir(parents=True, exist_ok=True)

    written = 0
    with out.open("w") as fout:
        for ex in iter_examples(args.split, args.max):
            fout.write(json.dumps(ex, ensure_ascii=False) + "\n")
            written += 1
    print(f"wrote {written} docs to {out}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
