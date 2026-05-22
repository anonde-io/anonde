#!/usr/bin/env python3
"""Fetch ai4privacy/pii-masking-300k from HuggingFace and convert into the
JSONL schema this benchmark expects.

ai4privacy ships privacy-mask spans (and parallel BIO-tagged tokens) with
labels like GIVENNAME1, LASTNAME1, EMAIL, USERNAME, etc. We map those into
anonde / Presidio entity types (PERSON, EMAIL_ADDRESS, etc.) and emit
span-based JSONL.

This is the SHARED loader for every ai4privacy_* corpus. The dataset is
multilingual; the 300k release covers six languages — en/fr/de/it/es/nl —
interleaved across `train` + `validation` splits, tagged by a per-row
`language` column. NOTE: that column holds *full English names*
("English", "German", "Spanish", ...), not ISO codes — the loader maps
the `--language` ISO code to the upstream name before filtering.

The five wired corpus dirs (ai4privacy_en/de/fr/it/es) all call this one
script and pass `--language <code>` to keep their slice; `--language any`
keeps every row. See bench/corpora/ai4privacy_en/README.md.

Usage:
    fetch_pii_masking.py --out <corpus>/data/corpus.jsonl \\
        [--language en|de|fr|it|es|nl|any] [--max 5000] [--split validation]
"""

from __future__ import annotations

import argparse
import json
import sys
from pathlib import Path
from typing import Iterable

# ai4privacy label → our entity type. Anything not in this mapping is
# dropped from gold (treated as a non-PII token) so the comparison is
# apples-to-apples with what anonde/Presidio actually try to detect.
#
# This vocabulary tracks the ai4privacy/pii-masking-300k label set (28
# privacy-mask classes). It differs substantially from the older 200k
# release: 300k splits personal names into GIVENNAME1/2 + LASTNAME1/2/3,
# and adds discrete ID classes (IDCARD, PASSPORT, DRIVERLICENSE,
# SOCIALNUMBER, PASS, CARDISSUER). Legacy 200k keys are kept as aliases
# so an older corpus snapshot still parses.
#
# The emitted type strings are anonde/Presidio-canonical so they line up
# 1:1 with the `gold:` section of bench/scoring/label_map.yaml.
LABEL_MAP = {
    # --- ai4privacy 300k privacy-mask classes -------------------------
    # Personal names (300k splits given/last into numbered components).
    "GIVENNAME1": "PERSON",
    "GIVENNAME2": "PERSON",
    "LASTNAME1": "PERSON",
    "LASTNAME2": "PERSON",
    "LASTNAME3": "PERSON",
    "TITLE": "PERSON",          # honorific bound to a name (Sir, Dr.)
    "USERNAME": "PERSON",
    # Locations (street + building + zip fold to LOCATION; see
    # --fold-parity-labels on the engine side).
    "CITY": "LOCATION",
    "STATE": "LOCATION",
    "COUNTRY": "LOCATION",
    "STREET": "LOCATION",
    "BUILDING": "LOCATION",
    "POSTCODE": "LOCATION",
    "SECADDRESS": "LOCATION",
    "GEOCOORD": "LOCATION",
    # Communication.
    "EMAIL": "EMAIL_ADDRESS",
    "TEL": "PHONE_NUMBER",
    "IP": "IP_ADDRESS",
    # Dates / times (BOD = birth-of-date).
    "DATE": "DATE_TIME",
    "BOD": "DATE_TIME",
    "TIME": "DATE_TIME",
    # IDs / financial — all collapse to the canonical ID bucket.
    "IDCARD": "ID",
    "PASSPORT": "ID",
    "DRIVERLICENSE": "ID",
    "SOCIALNUMBER": "ID",
    "PASS": "ID",               # password / passcode string
    "CARDISSUER": "ID",
    # SEX is intentionally unmapped: 'M' / 'F' is not a direct identifier
    # in anonde's vocabulary and there is no canonical type for it, so it
    # is dropped from gold rather than routed through OTHER.

    # --- legacy ai4privacy 200k aliases (kept for older snapshots) ----
    "FIRSTNAME": "PERSON",
    "LASTNAME": "PERSON",
    "MIDDLENAME": "PERSON",
    "FULLNAME": "PERSON",
    "PREFIX": "PERSON",
    "SUFFIX": "PERSON",
    "STREETADDRESS": "LOCATION",
    "BUILDINGNUMBER": "LOCATION",
    "ZIPCODE": "LOCATION",
    "SECONDARYADDRESS": "LOCATION",
    "COMPANY_NAME": "ORGANIZATION",
    "JOBTITLE": "ORGANIZATION",
    "PHONE_NUMBER": "PHONE_NUMBER",
    "PHONENUMBER": "PHONE_NUMBER",
    "URL": "URL",
    "IPV4": "IP_ADDRESS",
    "IPV6": "IP_ADDRESS",
    "MAC": "MAC_ADDRESS",
    "MACADDRESS": "MAC_ADDRESS",
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


# ai4privacy/pii-masking-300k ships these six languages. The dataset's
# per-row `language` column stores the *full English name* ("English",
# "German", ...), so we keep an ISO-code → upstream-name map and filter
# on the name. A loader invoked with a --language outside this set exits
# 2 (cleanly skipped by the matrix renderer) rather than silently
# writing an empty corpus.
#
# Spanish (es) and Dutch (nl) are new in 300k vs the old 200k release;
# the bench wires en/de/fr/it/es as corpus dirs (no nl corpus yet).
LANGUAGE_NAMES = {
    "en": "English",
    "fr": "French",
    "de": "German",
    "it": "Italian",
    "es": "Spanish",
    "nl": "Dutch",
}
SUPPORTED_LANGUAGES = tuple(LANGUAGE_NAMES)


def iter_examples(split: str, max_n: int | None,
                  language: str = "any") -> Iterable[dict]:
    try:
        from datasets import load_dataset, get_dataset_split_names
    except ImportError as exc:
        print(f"datasets not installed: {exc}", file=sys.stderr)
        sys.exit(2)

    # ai4privacy/pii-masking-300k ships `train` + `validation` splits. We
    # try the requested split first, then fall back to the first split
    # that does exist — keeping older invocations working without
    # forcing the caller to know the upstream layout.
    available = get_dataset_split_names("ai4privacy/pii-masking-300k")
    effective = split
    if split not in available:
        if not available:
            print(f"no splits available for ai4privacy/pii-masking-300k",
                  file=sys.stderr)
            sys.exit(2)
        effective = available[0]
        print(f"split {split!r} not found in {available!r}; "
              f"falling back to {effective!r}", file=sys.stderr)

    ds = load_dataset("ai4privacy/pii-masking-300k", split=effective)
    want_lang = (language or "any").strip().lower()
    # The upstream `language` column stores full English names; translate
    # the requested ISO code to the name we filter on.
    want_name = LANGUAGE_NAMES.get(want_lang, "").lower()
    n = 0
    for i, row in enumerate(ds):
        if max_n is not None and n >= max_n:
            return
        # Keep only this corpus's language slice. The dataset interleaves
        # en/fr/de/it/es/nl rows in each split; `--language any` disables
        # the filter. Rows with no language column fall through when no
        # specific language is requested.
        if want_lang != "any":
            row_lang = str(row.get("language", "")).strip().lower()
            if row_lang != want_name:
                continue
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
    parser.add_argument("--language", default="en",
                        help="keep only this language slice "
                             "(en/fr/de/it/es/nl) or 'any' for the whole "
                             "split. ai4privacy/pii-masking-300k covers "
                             "six languages.")
    args = parser.parse_args()

    want_lang = args.language.strip().lower()
    if want_lang != "any" and want_lang not in SUPPORTED_LANGUAGES:
        print(f"language {want_lang!r} not in ai4privacy/pii-masking-300k "
              f"(available: {', '.join(SUPPORTED_LANGUAGES)})", file=sys.stderr)
        return 2

    out = Path(args.out)
    out.parent.mkdir(parents=True, exist_ok=True)

    written = 0
    with out.open("w") as fout:
        for ex in iter_examples(args.split, args.max, want_lang):
            fout.write(json.dumps(ex, ensure_ascii=False) + "\n")
            written += 1
    print(f"wrote {written} docs to {out}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
