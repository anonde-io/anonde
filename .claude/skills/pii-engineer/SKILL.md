---
name: pii-engineer
description: |
  Apply when designing, building, benchmarking, or debugging local-first PII detection / anonymization systems. Covers metric tradeoffs (leak rate vs F1), NER backend selection (patterns + open-set NER vs standalone), conflict resolution between heuristic + neural detectors, the silent-fallback class of bugs (and how to catch them), bench harness design (corpus typology + guard rails), CGO + libonnxruntime deployment, recognizer extension patterns, and competitor landscape (Presidio, GLiNER, OpenAI Privacy Filter). Loads concepts, not file paths — pair with a project-specific skill for the latter.
allowed-tools: Read, Bash, Edit, Write, Grep, Glob
---

# PII engineer — concepts and gotchas for local-first PII tooling

> Generic patterns extracted from production work on anonde (anonymize / de-anonymize toolkit, German-clinical-first, Go-native). The facts here are codebase-agnostic — every "X" below has been verified against real bench results and production deploys.

## The bias that defines this kind of system

**Recall > precision.** A PII redactor that misses a PHI span has leaked data (a breach). A redactor that over-flags has wasted some attention (annoying, fixable). These costs aren't symmetric. Design every choice — score thresholds, conflict resolution, default backends — so the failure mode is over-redaction, not under-redaction.

**Leak rate is the load-bearing metric.** Defined as: gold PHI spans with no overlapping prediction. Not F1, not precision-recall, not perplexity. Leak rate directly counts "did we miss a name?" in 1:1 correspondence with real-world breach risk. Strict F1 penalises wider-than-gold spans (e.g. "Herr Müller" vs gold "Müller") which is academically rigorous but operationally meaningless — both successfully redact.

When you publish bench numbers, lead with leak rate. F1 is supporting.

## NER backend selection

The right architecture for high-recall multilingual PII as of mid-2026 is **regex+checksum patterns + open-set NER (GLiNER family)** with a conflict resolver that prefers each for what it's good at:

| Entity class | Whose job | Why |
|---|---|---|
| Structured IDs (IBAN, credit card, SSN, fiscal codes, …) | Patterns + checksums | MOD-97 / Luhn / similar give ~99% precision; random text essentially never satisfies them |
| Phone, email, IP, MAC, URL, crypto wallet, structured dates | Patterns | Shape-detectable; regex precision is high |
| Names (PERSON), companies (ORGANIZATION), places (LOCATION), age, profession | NER (GLiNER) | Context-dependent; no regex captures "Müller" reliably without false positives on common nouns |
| Free-form clinical / legal / financial vocabulary that AREN'T PII | Don't detect | Detection is opt-in per entity-type; don't expand without a recognizer per type |

Standalone NER alone (no patterns) leaks ~50% on most corpora because structured entities (IBAN, phone, dates) fall through. Patterns alone leak more on names (the regex anomaly recognizers are heuristic). The combination is what wins.

### NER landscape (Apr 2026 snapshot)

| Model | License | Languages | Verdict |
|---|---|---|---|
| **GLiNER PII (knowledgator/gliner-pii-*-v1.0)** | Apache 2.0 | English-trained, generalises well to DE/ES/FR/IT/NL/PT due to multilingual base | **Production pick** |
| GLiNER multi-PII (urchade/gliner_multi_pii-v1) | Apache 2.0 | 6 EU languages | Sigmoid scores 4-5× lower → brittle thresholds; rejected after bench |
| Microsoft Presidio (spaCy en_core_web_lg) | MIT | English in production; DE community pack unmaintained | Solid EN baseline (~28% leak on ai4privacy_en); not viable for German |
| OpenAI Privacy Filter | Open-weight (custom architecture) | English | 98% leak on German clinical; not worth the 2.8 GB weight + 80sec/doc CPU cost |
| Hugot/XLM-R PII (onnx-community/multilang-pii-ner-ONNX) | Apache 2.0 | Multilingual | Slower than GLiNER, worse leak rate, useful only for regression detection |
| John Snow Labs Healthcare NLP | Paid commercial | EN | Out of reach for OSS / local workloads |

## Conflict resolution — the non-obvious failure mode

Naive score-only resolution **always picks patterns over NER for unstructured types**. Pattern recognizers produce deterministic constants (0.85, 1.0). NER produces sigmoid floats (0.4–0.85). 0.85 > 0.83 → pattern wins every time, even when NER's span is the more accurate one.

The fix that's known to work:

```go
func shouldReplace(kept, candidate RecognizerResult) bool {
    // Same entity type AND it's a type where NER is more reliable than
    // heuristic patterns — NER wins regardless of score.
    if kept.EntityType == candidate.EntityType && nerPreferredEntities[kept.EntityType] {
        keptNER := nerRecognizerNames[kept.RecognizerName]
        candNER := nerRecognizerNames[candidate.RecognizerName]
        if candNER && !keptNER { return true }
        if keptNER && !candNER { return false }
    }
    return candidate.Score > kept.Score  // score-only fallback
}
```

`nerPreferredEntities`: PERSON, ORGANIZATION, LOCATION, AGE, PROFESSION, NRP. **Structured types stay score-only** — regex+checksum precision beats NER context there.

Document the rule in code. Without the comment, the next person to touch the resolver will revert to "just pick the higher score" and silently regress recall by 5pp on every release.

## The silent-fallback class of bugs

The single most expensive class of bug in this kind of system is **a configured NER backend that silently doesn't run, with the engine returning patterns-only output as if it succeeded**. We've hit this multiple times across:

- Missing `libonnxruntime.so` / wrong path / wrong arch
- ONNX file ambiguity (repo ships 3 variants, downloader refuses to pick)
- CGO disabled at build, NER recognizer linked against CGO-only ORT
- Wrong distroless base (`distroless/static` instead of `distroless/cc`)
- Python sidecar shadowed by file in `bench/runners/gliner.py` colliding with the installed `gliner` package

In every case: engine launched, lazy recognizer init errored, error was swallowed at the engine level (because per-recognizer failures shouldn't crash the batch), output JSONL had patterns-only findings. Indistinguishable from "patterns-only is your prod backend" except by leak rate.

### Canaries (always wire ALL of these)

1. **Engine-level visibility log** — when a recognizer errors, log it once per occurrence. Don't make per-recognizer errors invisible in the name of "robustness". A 1-line addition that makes the silent-fallback class of bugs detectable at all:

   ```go
   if p.err != nil {
       log.Printf("analyzer: recognizer error (swallowed): %v", p.err)
       errs = append(errs, p.err)
       continue
   }
   ```

2. **Non-round-sigmoid-score test** — pattern scores are deterministic constants (0.85, 1.0, multiples of 0.05). NER produces sigmoid floats. After a bench run, check:

   ```python
   ner_attributable = sum(
       1 for d in docs for f in d["findings"]
       if abs(f["score"] * 20 - round(f["score"] * 20)) > 1e-6
   )
   ```

   If `ner_attributable == 0`, NER didn't fire. Doesn't matter what the engine label says.

3. **CI guard rail** — fail the workflow if any bench cell with a model-based engine produces zero non-round-score findings. This is the failure mode CI exists to catch:

   ```yaml
   - name: Guard rail — NER actually fired
     run: |
       python3 - <<'PY'
       # check non-round sigmoid scores in every gliner cell ...
       if not_attributable: sys.exit(1)
       PY
   ```

4. **Per-init log lines** — recognizer `init()` should log "starting → ready" (or "INIT FAILED: ..."). When debugging, the absence of "ready" tells you init returned through an error path.

Without these, debugging a silent fallback takes hours. With them, seconds.

## Bench corpus typology

Three types, each answers a different question:

| Type | Has span-level gold? | What it answers | Example |
|---|---|---|---|
| **Gold-annotated** | ✓ | F1 + leak rate (recall) | GraSCCo PHI, ai4privacy, WikiAnn |
| **Slot-based synthetic** | ✓ (by construction) | Coverage under known PII shapes | Roll-your-own generator with templates + named-entity slots |
| **Precision probe** | ✗ | Does the engine over-flag on normal prose? | PubMed case reports, Wikipedia articles |

For a defensible bench, you want at least one of each. Synthetic alone produces high F1 numbers that don't generalise; real-text without gold can't compute F1 at all; real-text WITH gold (like GraSCCo / WikiAnn) is the gold standard but expensive to assemble.

### Bench matrix design

A matrix of (corpus × engine) cells, each producing a `findings.jsonl` of identical schema. A renderer aggregates into one report with:

- **Leak rate grid** (the headline metric)
- **Type-agnostic F1** (one F1 view, not three — strict/partial/type-only in CSV for the wonks)
- **Latency in mixed units** (`<1s` as ms, `≥1s` as s; 25,408 ms is hostile to read, "25.4 s" is fine)
- **Per-corpus verdict cards** in plain English with pp-deltas
- **Cell-coverage matrix** so missing cells are explicit, not hidden
- **A glossary footer** explaining "leak rate" vs "strict F1" vs "–" — readers don't all know these terms

Cache cells as file targets (Make order-only dependencies on the data fetch step) so partial reruns don't redo work. Without caching the matrix is too slow to iterate on.

## Production deployment shape

For Go-based PII pipelines with in-process ONNX NER:

| Variant | Image | Use when |
|---|---|---|
| Patterns-only | `distroless/static-debian12` + statically-linked Go binary | Maximum throughput, structured PII only |
| Patterns + NER | `distroless/cc-debian12` (includes glibc + libgcc + libstdc++) + libonnxruntime.so.1.X + baked model | Production — needs CGO=1 and a runtime shared lib |

Bake the model into the image (don't lazy-download at first request). Bake `libonnxruntime.so` from Microsoft's signed GitHub release tarball at build time. Set `ORT_SO_PATH=/usr/lib/x86_64-linux-gnu/libonnxruntime.so.1` explicitly so the dlopen doesn't depend on `LD_LIBRARY_PATH` discovery.

**Don't introduce a CGO dep in the patterns-only / default build path.** That breaks `Dockerfile.platform` (the static variant). Use Go build tags to keep the CGO-using recognizers behind an opt-in (e.g. `-tags hugot` or `-tags ner`).

## Recognizer patterns

A recognizer is a tiny interface — `Name() string`, `SupportedEntities() []string`, `SupportedLanguages() []string`, `Analyze(...) ([]Result, error)`. Two flavours:

### Pattern recognizer

Compile a regex once at package init. For structured IDs, follow with a **validator** (Luhn for cards, MOD-97 for IBAN, ISO 7064 for tax IDs, etc.). Random strings essentially never satisfy these — your FP rate on the structured class falls below 1%.

```go
type IBANRecognizer struct{ rx *regexp.Regexp }

func (r *IBANRecognizer) Analyze(_ context.Context, text string, _ []string, _ string) ([]RecognizerResult, error) {
    var out []RecognizerResult
    for _, m := range r.rx.FindAllStringIndex(text, -1) {
        if !validateMOD97(text[m[0]:m[1]]) { continue }
        out = append(out, RecognizerResult{Start: m[0], End: m[1], Score: 1.0, EntityType: "IBAN_CODE"})
    }
    return out, nil
}
```

### NER recognizer

Lazy `sync.Once` init. Catch panics with `defer recover` and surface as an error — upstream ONNX/tokenizer libs panic on edge inputs and you don't want one bad doc killing the batch. Log init success and per-Analyze chunk counts behind a `GLINER_QUIET=1`-style env toggle so the log doesn't spam in steady state.

**Name suffix matters.** If your engine has a `DisableNER` flag, name model-backed recognizers `*NERRecognizer` so the engine can filter them by name suffix. Recognizers emitting unstructured types (PERSON/ORG/LOC/AGE/PROFESSION) that AREN'T NER (like a clinical-anomaly heuristic) should NOT use the suffix — they need to keep running when NER is disabled.

## When to NOT detect

Don't expand entity coverage just because something LOOKS like it could be PII. Each new entity type adds:

- A new recognizer to test
- A new column in `label_map.yaml`
- A new bucket in the bench results
- A new source of false positives in production text

Examples of entities we explicitly DON'T flag in anonde:
- Monetary amounts ("48,000 EUR") — not PII in any standard taxonomy
- Generic time-of-day expressions ("at noon")
- Common-word names ("Hope", "Joy") without a strong context cue

The cost of NOT detecting a low-signal entity is one missed potential flag. The cost of OVER-detecting is shipping a tool that produces unusable amounts of redaction noise. Default to under-detecting in the gray zones.

## What this skill is NOT

- Not a model-training playbook. We use pretrained models. If you're fine-tuning, you need a different skill.
- Not a generic NLP-systems guide. Focused on the specific shape of "local PII pipeline" — high-recall, structured + unstructured detection, redaction-flow integration.
- Not a security-audit playbook for handling redacted output. That's downstream (encryption at rest, audit logs, role-based reveal — none of which is detection).

## Pair with project-specific skills

Project skills (like `anonde`) cite specific file paths, commit hashes, deploy commands, and current bench numbers. This skill stays evergreen because nothing here is timestamp-bound. When in doubt — concept-level lives here, project-level lives in the per-repo skill.
