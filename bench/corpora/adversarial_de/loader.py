#!/usr/bin/env python3
"""loader — derive an adversarial / out-of-distribution probe corpus from
synth_clinical by applying six classes of realistic input perturbations.

Every other German gold corpus in the bench (`synth_clinical`,
`finance_de`, `legal_de`, `openmed`) ships *clean* well-formed text:
correctly-spelled names, ASCII-clean dates, single-space tokenisation,
canonical umlauts. Real production traffic is not this clean — users
type Mueller for Müller, paste log lines with ANSI escape sequences, mix
English clauses into German notes, and so on. This corpus measures
robustness to those perturbations by *re-annotating* synth_clinical
through six deterministic transformations.

For each input doc N perturbations are applied (one perturbed doc per
kind). The output id encodes the perturbation kind so a downstream
renderer can group by kind:

    {"id":"adv-de-typo-0042",
     "perturbation":"typo_inside_pii",
     "text":"...",
     "entities":[{"start":int,"end":int,"type":"..."}]}

Critical invariant: after every perturbation the entity offsets are
recomputed so that `text[start:end]` is the perturbed surface form of
the original span. The script asserts this on every emitted entity and
exits with code 2 on first failure.
"""

from __future__ import annotations

import argparse
import json
import random
import sys
from pathlib import Path
from typing import Callable

PERTURBATIONS = [
    "typo_inside_pii",
    "transliteration_de_en",
    "case_scramble",
    "ansi_insertion",
    "code_switching",
    "whitespace_normalization",
]

# Short English sentences injected by code_switching. Picked to feel
# plausible in a clinical / business note ("the rest of the staff was
# notified", etc.). Each ends with a period + space so it slots cleanly
# between sentences.
EN_INJECTIONS = [
    "The patient was stable.",
    "No further intervention required.",
    "Follow-up scheduled next week.",
    "All vital signs within normal range.",
    "See attached chart for details.",
]

ANSI_RED = "\x1b[31m"
ANSI_RESET = "\x1b[0m"

# Umlaut transliteration table. ß is special (1→2 chars).
TRANSLIT = {
    "ä": "ae",
    "ö": "oe",
    "ü": "ue",
    "Ä": "Ae",
    "Ö": "Oe",
    "Ü": "Ue",
    "ß": "ss",
}


# ---------------------------------------------------------------------------
# offset bookkeeping helpers
# ---------------------------------------------------------------------------

def _shift_entities(entities: list[dict], inserts: list[tuple[int, int]]) -> list[dict]:
    """Given a list of (position, delta) insertions sorted by position,
    shift entity offsets accordingly. An insertion at position p with
    delta d shifts every offset >= p by d. For insertions *inside* a
    span (p strictly between start and end), the end shifts but the
    start does not.
    """
    out: list[dict] = []
    for ent in entities:
        s, e = ent["start"], ent["end"]
        ds, de = 0, 0
        for pos, delta in inserts:
            if pos <= s:
                ds += delta
                de += delta
            elif pos < e:
                de += delta
            # else insertion after the span; both unchanged
        out.append({**ent, "start": s + ds, "end": e + de})
    return out


# ---------------------------------------------------------------------------
# perturbations
# ---------------------------------------------------------------------------

def perturb_typo_inside_pii(text: str, entities: list[dict], rng: random.Random) -> tuple[str, list[dict]]:
    """For every PII span >=3 chars, swap two adjacent characters at the
    midpoint. Offsets are unchanged because swap is in-place.
    """
    chars = list(text)
    new_entities: list[dict] = []
    for ent in entities:
        s, e = ent["start"], ent["end"]
        span = chars[s:e]
        if len(span) >= 3:
            # Deterministic midpoint swap. Skip if it would collide with
            # a whitespace char (swapping inside "Karin Müller" at the
            # space is detectable but not really a typo).
            mid = len(span) // 2
            # Search outward from mid for a swap that doesn't involve
            # whitespace on either side.
            picked = None
            for off in range(len(span) - 1):
                cand = mid + (off // 2) * (1 if off % 2 == 0 else -1)
                if 0 <= cand < len(span) - 1:
                    a, b = span[cand], span[cand + 1]
                    if not a.isspace() and not b.isspace():
                        picked = cand
                        break
            if picked is not None:
                i = s + picked
                chars[i], chars[i + 1] = chars[i + 1], chars[i]
        new_entities.append({**ent})
    return "".join(chars), new_entities


def perturb_transliteration_de_en(text: str, entities: list[dict], rng: random.Random) -> tuple[str, list[dict]]:
    """Replace ä/ö/ü/ß (and capitalised forms) with ae/oe/ue/ss across
    the whole text. ß→ss adds one char per occurrence; umlauts also
    expand 1→2.
    """
    # Build a list of (codepoint_index, delta) insertions plus the new
    # string in one left-to-right pass.
    out_chars: list[str] = []
    inserts: list[tuple[int, int]] = []
    for i, ch in enumerate(text):
        rep = TRANSLIT.get(ch)
        if rep is None:
            out_chars.append(ch)
        else:
            out_chars.append(rep)
            delta = len(rep) - 1  # always +1 for our table
            # Insert "after position i" semantically — the +1 char shows
            # up between codepoint i and i+1 of the original. The span
            # containing i absorbs it; later spans shift by +1.
            inserts.append((i + 1, delta))
    new_text = "".join(out_chars)
    new_entities = _shift_entities(entities, inserts)
    return new_text, new_entities


def perturb_case_scramble(text: str, entities: list[dict], rng: random.Random) -> tuple[str, list[dict]]:
    """Flip case of every other alphabetic character. Offsets are
    unchanged (case flip is 1:1).
    """
    out = []
    flip = False
    for ch in text:
        if ch.isalpha():
            if flip:
                out.append(ch.swapcase())
            else:
                out.append(ch)
            flip = not flip
        else:
            out.append(ch)
    return "".join(out), [dict(e) for e in entities]


def perturb_ansi_insertion(text: str, entities: list[dict], rng: random.Random) -> tuple[str, list[dict]]:
    """Insert ANSI red+reset around every PII span. The inner span (the
    cleartext PII) is what entities point at — so start shifts by
    len(ANSI_RED), end shifts by len(ANSI_RED), and the closing
    ANSI_RESET goes *after* the original end. Spans are processed from
    rightmost to leftmost so earlier offsets remain valid while we splice.
    """
    # Sort entities by start desc; we rebuild a parallel new_entities
    # list with shifted offsets after splicing.
    ordered = sorted(enumerate(entities), key=lambda iv: iv[1]["start"], reverse=True)
    new_text = text
    # For each (original_index, ent) we will record (new_start, new_end).
    deltas: dict[int, tuple[int, int]] = {}
    # Track cumulative shifts to compute final positions for entities
    # we have already processed (further to the right, so they shift
    # by every insertion that happens to their left as we walk left).
    # We compute final positions in a second pass via _shift_entities.
    inserts: list[tuple[int, int]] = []
    # Apply right-to-left so that splicing uses still-valid offsets.
    for _orig_idx, ent in ordered:
        s, e = ent["start"], ent["end"]
        new_text = new_text[:e] + ANSI_RESET + new_text[e:]
        new_text = new_text[:s] + ANSI_RED + new_text[s:]
        inserts.append((s, len(ANSI_RED)))
        inserts.append((e, len(ANSI_RESET)))
    # Sort insertions left-to-right for _shift_entities.
    inserts.sort(key=lambda t: t[0])
    new_entities = _shift_entities(entities, inserts)
    return new_text, new_entities


def perturb_code_switching(text: str, entities: list[dict], rng: random.Random) -> tuple[str, list[dict]]:
    """Inject 3-5 English sentences between German sentences at
    deterministic positions. Sentence boundaries are paragraph breaks
    (`\\n\\n`) or single newlines following ASCII letters — both are
    reliable in the synth_clinical sublanguages because every section
    header ends with a newline. We deliberately avoid the ". "
    heuristic: clinical text contains plenty of "St." / "Dr." / "med."
    abbreviations and accidentally splitting on those drops English
    sentences *inside* PII spans (e.g. mid-hospital-name).

    Boundaries that fall inside an existing entity span are dropped so
    the injected English never lands mid-PII.
    """
    # Mark codepoints that are inside a PII span; these are off-limits
    # as insertion targets.
    in_span = [False] * (len(text) + 1)
    for ent in entities:
        for k in range(ent["start"] + 1, ent["end"]):
            # +1 / no end so that inserting *exactly* at start or end
            # of a span is still allowed (it doesn't bisect the span).
            in_span[k] = True

    # Find sentence boundaries. Insertion position is the index *of the
    # first character of the next sentence*, i.e. immediately after the
    # delimiter.
    boundaries: list[int] = []
    i = 0
    while i < len(text) - 1:
        ch = text[i]
        nxt = text[i + 1]
        # Paragraph break.
        if ch == "\n" and nxt == "\n":
            pos = i + 2
            if pos < len(text) and not in_span[pos]:
                boundaries.append(pos)
            i += 2
            continue
        # Single newline after a non-space character — treat as a line
        # break / section header end. Skip if next char is whitespace
        # (already a paragraph break) or if it lands inside a span.
        if ch == "\n" and i > 0 and not text[i - 1].isspace():
            pos = i + 1
            if pos < len(text) and not in_span[pos]:
                boundaries.append(pos)
            i += 1
            continue
        i += 1
    if not boundaries:
        # Nothing safe to inject into — return unchanged.
        return text, [dict(e) for e in entities]
    # Pick deterministically: number of injections in [3, 5], spaced
    # evenly across the boundary list.
    n_inject = min(len(boundaries), 3 + (rng.randrange(0, 3)))
    step = max(1, len(boundaries) // n_inject)
    chosen = sorted({boundaries[min(k * step, len(boundaries) - 1)] for k in range(n_inject)})
    # Build inserts. Each chosen position gets one EN sentence + " ".
    inserts: list[tuple[int, int]] = []
    new_text = text
    en_picks = [EN_INJECTIONS[rng.randrange(0, len(EN_INJECTIONS))] for _ in chosen]
    # Process rightmost-first so positions stay valid during splicing.
    for pos, sent in sorted(zip(chosen, en_picks), key=lambda t: t[0], reverse=True):
        payload = sent + " "
        new_text = new_text[:pos] + payload + new_text[pos:]
        inserts.append((pos, len(payload)))
    inserts.sort(key=lambda t: t[0])
    new_entities = _shift_entities(entities, inserts)
    return new_text, new_entities


def perturb_whitespace_normalization(text: str, entities: list[dict], rng: random.Random) -> tuple[str, list[dict]]:
    """Replace one single space inside each PII span with a NBSP
    (U+00A0). NBSP is a single codepoint so offsets do not shift —
    only the character changes. If a span has no internal space, leave
    it untouched.
    """
    chars = list(text)
    new_entities: list[dict] = []
    for ent in entities:
        s, e = ent["start"], ent["end"]
        # Find all internal space positions. Use a deterministic pick:
        # rng has been seeded per-doc, so this is reproducible.
        space_positions = [k for k in range(s, e) if chars[k] == " "]
        if space_positions:
            pick = space_positions[rng.randrange(0, len(space_positions))]
            chars[pick] = " "
        new_entities.append({**ent})
    return "".join(chars), new_entities


PERTURB_FNS: dict[str, Callable] = {
    "typo_inside_pii": perturb_typo_inside_pii,
    "transliteration_de_en": perturb_transliteration_de_en,
    "case_scramble": perturb_case_scramble,
    "ansi_insertion": perturb_ansi_insertion,
    "code_switching": perturb_code_switching,
    "whitespace_normalization": perturb_whitespace_normalization,
}


# ---------------------------------------------------------------------------
# driver
# ---------------------------------------------------------------------------

def _validate(doc: dict) -> None:
    """Raise AssertionError if any entity in doc has an empty or
    out-of-range slice. Used as a final correctness gate.
    """
    text = doc["text"]
    for ent in doc.get("entities", []):
        s, e = ent["start"], ent["end"]
        assert 0 <= s < e <= len(text), (
            f"out-of-range span in {doc['id']}: {ent} (len={len(text)})"
        )
        assert text[s:e], f"empty span in {doc['id']}: {ent}"


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--in", dest="in_path", required=True,
                    help="path to synth_clinical's corpus.jsonl")
    ap.add_argument("--out", required=True)
    ap.add_argument("--n", type=int, default=50,
                    help="number of input docs to sample from --in")
    ap.add_argument("--seed", type=int, default=20260515)
    args = ap.parse_args()

    in_path = Path(args.in_path)
    if not in_path.exists():
        print(f"input not found: {in_path}", file=sys.stderr)
        return 2

    rng = random.Random(args.seed)

    # Load all docs, then deterministically pick N.
    all_docs: list[dict] = []
    with in_path.open("r", encoding="utf-8") as fh:
        for line in fh:
            line = line.strip()
            if not line:
                continue
            all_docs.append(json.loads(line))
    if not all_docs:
        print(f"empty input: {in_path}", file=sys.stderr)
        return 2

    # Sample N deterministically. If the source has fewer than N docs,
    # use what's available.
    n = min(args.n, len(all_docs))
    indices = list(range(len(all_docs)))
    rng.shuffle(indices)
    picked = sorted(indices[:n])
    selected = [all_docs[i] for i in picked]

    out_path = Path(args.out)
    out_path.parent.mkdir(parents=True, exist_ok=True)

    n_emitted = 0
    n_spans = 0
    with out_path.open("w", encoding="utf-8") as fh:
        for doc_idx, doc in enumerate(selected):
            for pert in PERTURBATIONS:
                # Seed each (doc, perturbation) deterministically so
                # output is stable run-to-run regardless of iteration
                # order changes.
                sub_seed = (args.seed * 1_000_003 + doc_idx * 131 +
                            hash(pert) & 0xFFFFFFFF) & 0x7FFFFFFF
                sub_rng = random.Random(sub_seed)

                fn = PERTURB_FNS[pert]
                new_text, new_entities = fn(doc["text"], doc["entities"], sub_rng)
                # Drop zero-width entities defensively (shouldn't happen
                # but a defensive guard catches generator bugs early).
                new_entities = [e for e in new_entities if e["end"] > e["start"]]

                short = pert.split("_", 1)[0]
                out_doc = {
                    "id": f"adv-de-{short}-{doc_idx:04d}",
                    "perturbation": pert,
                    "text": new_text,
                    "entities": new_entities,
                }
                try:
                    _validate(out_doc)
                except AssertionError as err:
                    print(f"offset validation failed: {err}", file=sys.stderr)
                    return 2

                fh.write(json.dumps(out_doc, ensure_ascii=False) + "\n")
                n_emitted += 1
                n_spans += len(new_entities)

    print(
        f"wrote {out_path}: {n_emitted} docs across {len(PERTURBATIONS)} "
        f"perturbations ({n} input docs), {n_spans} entity spans",
        file=sys.stderr,
    )
    return 0


if __name__ == "__main__":
    sys.exit(main())
