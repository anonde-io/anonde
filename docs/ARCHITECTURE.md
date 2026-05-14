# Architecture

## Pipeline

```
┌─────────┐   ┌──────────────┐   ┌──────────────┐   ┌──────────────┐
│  text   ├──▶│ 40+ regex /  ├──▶│  GLiNER NER  ├──▶│  anonymizer  │──▶ tokenised text + vault
│         │   │  checksum    │   │  (PII labels │   │  (6 ops)     │
│         │   │  recognizers │   │  + DE clin.) │   │              │
└─────────┘   └──────────────┘   └──────────────┘   └──────────────┘
                     │                  │                   │
                     ▼                  ▼                   ▼
              ENGLISH/EU IDs       PERSON/ORG/LOC      Replace, Redact,
              IBAN, phone,         AGE, PROFESSION,    Mask, Hash,
              email, SSN,          (multilingual)      Encrypt, Synthesize
              passport, …
```

The detection bias is **recall > precision**: anonde would rather over-tokenise (safe) than miss a PHI span (a leak). The bench tracks this explicitly via the `leak_rate` metric (lower = better).

## Directory layout

```
anonde/
├── analyzer/                  # recognizer registry + parallel dispatch
│   ├── analyzer.go            # AnalyzerEngine.Analyze: filter → dispatch → conflict resolve
│   ├── result.go              # RecognizerResult + RemoveConflicts (NER-preference rule)
│   ├── reconciler/            # optional LLM disambiguation stage (Ollama)
│   ├── auditor/               # post-anonymization LLM audit (Ollama)
│   └── recognizers/           # 52 pattern + 3 NER recognizers
│       ├── *Recognizer.go     # per-region pattern recognizers
│       ├── ner_hugot.go       # `-tags hugot`: in-process ONNX TokenClassification
│       ├── ner_gliner.go      # `-tags hugot`: GLiNER (open-set NER) via yalue/onnxruntime_go
│       └── ner_ollama.go      # Ollama HTTP client
├── anonymizer/                # apply operators to detected spans
│   ├── anonymizer.go          # mergeAdjacentSameType + dispatch to operators
│   └── operators/             # Replace, Redact, Mask, Hash, Encrypt, Synthesize
├── cmd/anonde/              # HTTP service
├── internal/platform/         # service + in-memory vault/store/policy
└── bench/                     # single bench harness
    ├── Makefile               # top-level `make matrix`, `make matrix-de`, `make matrix-en`, …
    ├── corpora/<NAME>/        # per-corpus Makefile + loader + data + gold
    ├── runners/               # one Go runner, three Python sidecars (gliner, openai_pf, presidio)
    ├── probes/                # diagnostic loaders for hugot, gliner
    └── scoring/               # compare.py, render_matrix.py, label_map.yaml
```

## Conflict resolution (the non-obvious part)

`RemoveConflicts` keeps the highest-scoring span when two overlap — **except** for entity types where NER is more reliable than heuristic patterns (PERSON, ORGANIZATION, LOCATION, AGE, PROFESSION, NRP). For those, an NER finding beats a pattern finding regardless of score.

Why: pattern recognizers like `DEAnomalyRecognizer` produce fixed scores (0.85); GLiNER produces sigmoid floats (0.4–0.85). Without this rule, patterns always won and the NER's contextual judgement was wasted.

For structured types (IBAN, PHONE_NUMBER, DATE_TIME, EMAIL_ADDRESS, …) the score-only rule still applies — regex+checksum precision matters more than NER context there.

See [`analyzer/result.go`](../analyzer/result.go) — `shouldReplace`.

## Per-recognizer error visibility

Silent failures in the analyzer pipeline are logged via `analyzer: recognizer error (swallowed)` so a broken NER backend can't masquerade as patterns-only. CI also asserts that NER cells produce a non-zero number of NER-attributable findings (see [DEPLOYMENT.md](DEPLOYMENT.md#ci)).

## In-memory vault

The platform service ships an in-memory vault (token ↔ cleartext) with configurable TTL — no DB required for ephemeral workloads. TTLs and the request-size cap are env-tunable; see [DEPLOYMENT.md](DEPLOYMENT.md#env-vars).
