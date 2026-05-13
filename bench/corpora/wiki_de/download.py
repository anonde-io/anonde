#!/usr/bin/env python3
"""Download German Wikipedia medical articles via the public MediaWiki API.

No authentication required. Walks a curated list of medical categories,
fetches up to N articles per category as plain-text extracts, and writes
them to corpus.jsonl in the shared bench schema with `entities: []`
(Wikipedia has no PHI by design).

Purpose: precision probe. Any finding anonde produces on this corpus is
suspicious — most likely a false positive (medical jargon flagged as
PERSON, historical figure name in a disease-eponym article, etc.).
"""

from __future__ import annotations

import argparse
import json
import sys
import time
import urllib.parse
import urllib.request
from pathlib import Path

API = "https://de.wikipedia.org/w/api.php"

# Curated German medical Wikipedia categories. The MediaWiki API
# walks these via list=categorymembers; depth=1 (no recursion into
# sub-categories — that explodes the sample to tens of thousands).
DEFAULT_CATEGORIES = [
    "Kategorie:Krankheit",          # Diseases
    "Kategorie:Symptom",            # Symptoms
    "Kategorie:Diagnose",           # Diagnoses
    "Kategorie:Medikament",         # Drugs
    "Kategorie:Anatomie",           # Anatomy
    "Kategorie:Chirurgischer_Eingriff",  # Surgical procedures
    "Kategorie:Medizinisches_Untersuchungsverfahren",  # Diagnostic procedures
    "Kategorie:Pflege",             # Nursing
]


def _api_get(params: dict) -> dict:
    """One GET against the MediaWiki API. Polite User-Agent + small delay."""
    params = dict(params)
    params.setdefault("format", "json")
    params.setdefault("formatversion", "2")
    url = API + "?" + urllib.parse.urlencode(params)
    req = urllib.request.Request(
        url,
        headers={"User-Agent": "anonde-bench/0.1 (https://github.com/anonde-io/anonde)"},
    )
    with urllib.request.urlopen(req, timeout=30) as resp:
        return json.loads(resp.read().decode("utf-8"))


def _category_members(category: str, limit: int) -> list[str]:
    """Returns up to `limit` page titles in the given category."""
    titles: list[str] = []
    cont: dict = {}
    while len(titles) < limit:
        params = {
            "action": "query",
            "list": "categorymembers",
            "cmtitle": category,
            "cmlimit": min(limit - len(titles), 500),
            "cmnamespace": "0",  # main namespace only — no talk pages, no categories
            "cmtype": "page",
        }
        params.update(cont)
        data = _api_get(params)
        for m in data.get("query", {}).get("categorymembers", []):
            titles.append(m["title"])
            if len(titles) >= limit:
                break
        cont = data.get("continue") or {}
        if not cont:
            break
        time.sleep(0.2)  # be polite
    return titles


def _page_extract(title: str) -> str:
    """Plain-text extract of a single page (no Wikipedia markup)."""
    data = _api_get({
        "action": "query",
        "prop": "extracts",
        "titles": title,
        "explaintext": "1",
        "exsectionformat": "plain",
        "redirects": "1",
    })
    pages = data.get("query", {}).get("pages") or []
    if not pages:
        return ""
    return pages[0].get("extract", "") or ""


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--out", required=True, help="output corpus.jsonl")
    ap.add_argument("--per-category", type=int, default=15,
                    help="max articles to pull per category")
    ap.add_argument("--min-chars", type=int, default=500,
                    help="skip articles shorter than this")
    ap.add_argument(
        "--category",
        action="append",
        default=None,
        help="repeat to override the default category list",
    )
    args = ap.parse_args()

    categories = args.category or DEFAULT_CATEGORIES
    out_path = Path(args.out)
    out_path.parent.mkdir(parents=True, exist_ok=True)

    seen_titles: set[str] = set()
    docs = 0
    n_chars = 0
    with out_path.open("w", encoding="utf-8") as fout:
        for cat in categories:
            print(f"==> {cat}", file=sys.stderr)
            try:
                titles = _category_members(cat, args.per_category * 2)
            except Exception as exc:  # noqa: BLE001
                print(f"  failed to list category: {exc}", file=sys.stderr)
                continue
            picked = 0
            for title in titles:
                if title in seen_titles:
                    continue
                seen_titles.add(title)
                try:
                    text = _page_extract(title)
                except Exception as exc:  # noqa: BLE001
                    print(f"  fetch {title!r}: {exc}", file=sys.stderr)
                    continue
                if len(text) < args.min_chars:
                    continue
                # Strip = Section = markers that survive plain-text extract
                text = "\n".join(
                    line for line in text.splitlines()
                    if not (line.strip().startswith("==") and line.strip().endswith("=="))
                )
                fout.write(json.dumps(
                    {"id": f"wiki-de-{title.replace(' ', '_')}",
                     "text": text,
                     "entities": []},
                    ensure_ascii=False,
                ) + "\n")
                docs += 1
                n_chars += len(text)
                picked += 1
                if picked >= args.per_category:
                    break
                time.sleep(0.2)
            print(f"  picked {picked}", file=sys.stderr)
    print(
        f"wrote {out_path}: {docs} docs, "
        f"{n_chars/1024:.0f} KB total, ~{n_chars//max(docs,1)} chars/doc",
        file=sys.stderr,
    )
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
