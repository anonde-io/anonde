#!/usr/bin/env python3
"""Download German clinical case reports from PubMed Central Open Access.

NCBI E-utilities is fully public (no auth, no key needed for low volume).
We search PMC for case reports tagged as German and pull their full-text
XML, then convert to plain text. Output matches the shared bench schema:

    {"id": "pmc-<PMCID>", "text": "...", "entities": []}

This is a *precision probe*: PubMed-published case reports are de-identified
before publication, so any finding anonde produces is suspicious. Tells us
whether the recognizer over-fires on clinical sublanguages other than
GraSCCo's discharge-letter format (e.g. case-report narrative prose).
"""

from __future__ import annotations

import argparse
import json
import re
import sys
import time
import urllib.parse
import urllib.request
import xml.etree.ElementTree as ET
from pathlib import Path

E_SEARCH = "https://eutils.ncbi.nlm.nih.gov/entrez/eutils/esearch.fcgi"
E_FETCH = "https://eutils.ncbi.nlm.nih.gov/entrez/eutils/efetch.fcgi"

# The query: case reports in PMC's open-access subset, in German.
# `lang[Filter] AND ger[Language]` keeps us inside German-language papers.
# `case reports[PT]` is PubMed's publication-type tag for case reports.
# `open access[Filter]` restricts to OA full-text we can actually fetch.
DEFAULT_QUERY = (
    "(case reports[PT]) AND ger[Language] AND open access[Filter]"
)

UA = "anonde-bench/0.1 (https://github.com/anonde-io/anonde; mailto:contact@anonde.io)"


def _http_get(url: str, timeout: int = 30) -> bytes:
    req = urllib.request.Request(url, headers={"User-Agent": UA})
    with urllib.request.urlopen(req, timeout=timeout) as resp:
        return resp.read()


def _esearch(query: str, retmax: int) -> list[str]:
    """Return up to retmax PMC IDs matching the query."""
    qs = urllib.parse.urlencode({
        "db": "pmc",
        "term": query,
        "retmax": retmax,
        "retmode": "json",
    })
    data = json.loads(_http_get(f"{E_SEARCH}?{qs}"))
    return list(data.get("esearchresult", {}).get("idlist", []) or [])


def _efetch_xml(pmcids: list[str]) -> bytes:
    """Fetch PMC full-text XML for a batch of ids."""
    qs = urllib.parse.urlencode({
        "db": "pmc",
        "id": ",".join(pmcids),
        "retmode": "xml",
    })
    return _http_get(f"{E_FETCH}?{qs}", timeout=60)


_WS_RE = re.compile(r"\s+")


def _xml_to_text(article: ET.Element) -> tuple[str, str]:
    """Extract (pmcid, plain-text body) from one <article> element."""
    pmcid = ""
    for aid in article.iterfind(".//article-id"):
        if aid.attrib.get("pub-id-type") in {"pmc", "pmcid"}:
            pmcid = (aid.text or "").strip().lstrip("PMC")
            break

    # We want the body — title + abstract + body paragraphs.
    parts: list[str] = []
    title = article.findtext(".//article-meta/title-group/article-title", default="")
    if title:
        parts.append(title.strip())

    for sec in article.iterfind(".//abstract"):
        for p in sec.iter("p"):
            txt = "".join(p.itertext()).strip()
            if txt:
                parts.append(txt)

    body = article.find(".//body")
    if body is not None:
        for elem in body.iter():
            if elem.tag in {"p", "title"}:
                txt = "".join(elem.itertext()).strip()
                if txt:
                    parts.append(txt)

    text = "\n\n".join(parts)
    text = _WS_RE.sub(lambda m: "\n\n" if "\n\n" in m.group() else " ", text)
    return pmcid, text.strip()


def _is_german(text: str) -> bool:
    """Cheap heuristic — German articles in PMC OA are often abstract-only
    German with full-body English. We want body-German text. A handful of
    very-common German function words tells us the body is actually in
    German, not just the abstract.
    """
    if len(text) < 200:
        return False
    sample = text[:2000].lower()
    hits = sum(
        1 for tok in (" und ", " der ", " die ", " das ", " ist ",
                      " nicht ", " wir ", " im ", " mit ", " für ", " den ")
        if tok in sample
    )
    return hits >= 5


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--out", required=True, help="output corpus.jsonl")
    ap.add_argument("--max", type=int, default=150,
                    help="max PMC IDs to fetch (will dedupe + filter to German)")
    ap.add_argument("--min-chars", type=int, default=500,
                    help="skip articles shorter than this after extraction")
    ap.add_argument("--query", default=DEFAULT_QUERY,
                    help="override E-utilities search query")
    ap.add_argument("--batch", type=int, default=20,
                    help="ids per efetch call (NCBI tolerates ~20 cleanly)")
    args = ap.parse_args()

    out_path = Path(args.out)
    out_path.parent.mkdir(parents=True, exist_ok=True)

    print(f"==> esearch: {args.query}", file=sys.stderr)
    ids = _esearch(args.query, args.max)
    print(f"   {len(ids)} candidate ids", file=sys.stderr)
    if not ids:
        print("no results — try a different --query", file=sys.stderr)
        return 1

    docs = 0
    n_chars = 0
    n_skipped_lang = 0
    n_skipped_short = 0
    with out_path.open("w", encoding="utf-8") as fout:
        for i in range(0, len(ids), args.batch):
            batch = ids[i:i + args.batch]
            print(f"==> efetch batch {i // args.batch + 1} ({len(batch)} ids)",
                  file=sys.stderr)
            try:
                xml_bytes = _efetch_xml(batch)
            except Exception as exc:  # noqa: BLE001
                print(f"   batch failed: {exc}", file=sys.stderr)
                time.sleep(1.0)
                continue
            try:
                root = ET.fromstring(xml_bytes)
            except ET.ParseError as exc:
                print(f"   parse error: {exc}", file=sys.stderr)
                continue
            for article in root.iter("article"):
                pmcid, text = _xml_to_text(article)
                if len(text) < args.min_chars:
                    n_skipped_short += 1
                    continue
                if not _is_german(text):
                    n_skipped_lang += 1
                    continue
                doc_id = f"pmc-de-{pmcid or f'idx{docs}'}"
                fout.write(json.dumps(
                    {"id": doc_id, "text": text, "entities": []},
                    ensure_ascii=False,
                ) + "\n")
                docs += 1
                n_chars += len(text)
            time.sleep(0.4)  # be polite to NCBI

    print(
        f"wrote {out_path}: {docs} docs, {n_chars/1024:.0f} KB total, "
        f"~{n_chars // max(docs, 1)} chars/doc "
        f"(skipped: {n_skipped_lang} non-German body, {n_skipped_short} too short)",
        file=sys.stderr,
    )
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
