# anonde

**Local-first PII detection and reversible anonymization toolkit.** A Go-native alternative to [Microsoft Presidio](https://github.com/microsoft/presidio), with German clinical text as a first-class target. Patterns + open-set NER + optional LLM reconciler, all in-process. No cloud calls.

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

## Status

- **Production deploy**: `https://anonde-platform.fly.dev` — Fly.io single-machine, NER-backend = GLiNER, libonnxruntime baked into the image, German + English + 5 more languages.
- **CI**: `.github/workflows/bench.yml` runs the bench matrix on every PR/push to bench-relevant code, with a guard rail that fails if GLiNER silently degrades to patterns-only.
- **Bench**: `bench/REPORT_MATRIX.md` is the authoritative comparison (5 engines × 3 gold-annotated corpora). The headline finding lives in [`.claude/memory/bench_findings_2026_05_13.md`](.claude/memory/bench_findings_2026_05_13.md).

## Features

- **52 pattern recognizers** organised by region (international + EN/US + UK + IT + ES + AU + IN + PL + SG + FI + KR + DE). Most validate (Luhn / MOD-97 / Codice Fiscale check digits / etc.) so precision is high.
- **Three NER backends**:
  - **GLiNER** (in-process, production default) — open-set NER trained for PII; ~280 MB ONNX, ~200 ms/doc on Fly amd64 hardware. Wins leak rate vs Presidio, OpenAI Privacy Filter, and patterns-only across all benched corpora.
  - **hugot/XLM-R** (in-process, legacy) — pre-GLiNER backend, kept for regression detection. Slower and less recall than GLiNER on German clinical text.
  - **Ollama** (local LLM) — opt-in for users who already run an Ollama daemon. Useful as an LLM-reconciler stage layered on top of patterns or GLiNER.
  - **Patterns-only** mode is also fully supported (no model download, no CGO).
- **6 anonymization operators** — replace, redact, mask, hash, encrypt (AES-GCM), synthesize (Luhn-valid / MOD-97-valid / structurally-sound fake data).
- **Conflict resolution that prefers NER for unstructured types** (PERSON, ORGANIZATION, LOCATION, AGE, PROFESSION) and patterns for structured (IBAN, phone, date, …). See `analyzer/result.go::shouldReplace`.
- **Per-recognizer error visibility** — silent failures in the analyzer pipeline are now logged via `analyzer: recognizer error (swallowed)` so a broken NER backend can't masquerade as patterns-only.
- **In-memory vault** for the platform service — token ↔ cleartext with configurable TTL, no DB required for ephemeral workloads.

## Installation

```bash
go get github.com/moogacs/anonde
```

Requires Go 1.26. Default build is pure-Go (no CGO). The `-tags hugot` build is required for the in-process NER backends (Hugot, GLiNER) and pulls in CGO + libonnxruntime as a runtime dep.

## Quick start — patterns-only

Zero deps, zero model downloads:

```go
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/moogacs/anonde"
	"github.com/moogacs/anonde/analyzer"
	"github.com/moogacs/anonde/anonymizer"
	"github.com/moogacs/anonde/anonymizer/operators"
)

func main() {
	text := `Patient Herr Müller, geboren 14.03.1962, Hauptstr. 8, 10115 Berlin, Tel 030-12345678.`

	engine := anonde.DefaultAnalyzerEngine()
	results, err := engine.Analyze(context.Background(), text, analyzer.AnalysisConfig{
		Language:        "de",
		ScoreThreshold:  0.3,
		RemoveConflicts: true,
	})
	if err != nil {
		log.Fatal(err)
	}

	anon := anonde.DefaultAnonymizerEngine()
	out, _ := anon.Anonymize(text, results, anonymizer.AnonymizerConfig{
		"*": &operators.Replace{}, // → <PERSON_1>, <DATE_TIME_1>, …
	})
	fmt.Println(out.Text)
}
```

## Quick start — GLiNER (production NER)

Requires `-tags hugot` + a runtime libonnxruntime:

```go
engine := anonde.DefaultAnalyzerEngineWithGLiNERConfig(recognizers.GLiNERConfig{
	ModelName:    "knowledgator/gliner-pii-base-v1.0",
	OnnxFilePath: "onnx/model_quint8.onnx",
	AutoDownload: true,            // pulls ~200 MB on first call
	Threshold:    0.40,
})
```

The first call lazy-downloads the model into `~/.cache/anonde/models/`. At runtime the recognizer dlopens `libonnxruntime.so` (Linux) / `.dylib` (macOS) — see Deployment below.

## Pattern recognizers (52)

Score is the pattern's confidence band; recognizers that validate via checksums score higher than pure-regex ones.

| Region | Recognizers |
|---|---|
| Generic / international | `EmailRecognizer` · `PhoneRecognizer` · `CreditCardRecognizer` (Luhn) · `IBANRecognizer` (MOD-97) · `IPAddressRecognizer` · `MACAddressRecognizer` · `URLRecognizer` · `CryptoRecognizer` (Bitcoin/Ethereum) · `DateTimeRecognizer` |
| English / US | `USSocialSecurityRecognizer` · `USPassportRecognizer` · `USBankRecognizer` · `USDriverLicenseRecognizer` · `USITINRecognizer` · `MedicalLicenseRecognizer` · `ENAnomalyRecognizer` (clinical-context PERSON) · `ENOrganizationRecognizer` |
| United Kingdom | `UKNHSRecognizer` · `UKNINORecognizer` |
| Italy | `ITFiscalCodeRecognizer` · `ITDriverLicenseRecognizer` · `ITVATCodeRecognizer` · `ITPassportRecognizer` · `ITIdentityCardRecognizer` |
| Spain | `ESNIFRecognizer` · `ESNIERecognizer` |
| Australia | `AUABNRecognizer` · `AUACNRecognizer` · `AUTFNRecognizer` · `AUMedicareRecognizer` |
| India | `INPANRecognizer` · `INAadhaarRecognizer` · `INVehicleRegistrationRecognizer` · `INVoterRecognizer` · `INPassportRecognizer` |
| Poland | `PLPESELRecognizer` |
| Singapore | `SGNRICRecognizer` · `SGUENRecognizer` |
| Finland | `FIPersonalIdentityCodeRecognizer` |
| Korea | `KRRRNRecognizer` |
| Germany (first-class) | `DEDateTimeRecognizer` · `DEDateContextRecognizer` · `DEPhoneRecognizer` · `DEPostalCodeRecognizer` · `DEStreetRecognizer` · `DESteuerIDRecognizer` · `DEAgeRecognizer` · `DEPlaceRecognizer` · `DEClinicalIDRecognizer` · `DEOrganizationRecognizer` · `DEProfessionRecognizer` · `DEAnomalyRecognizer` (closed-vocab clinical anomaly → PERSON candidate) |

Most recognizers are language-gated (`SupportedLanguages()`), so a request with `language: "de"` skips US-specific recognizers and vice versa. Universal pattern recognizers (email, IBAN, etc.) run on every language.

## NER backends — current matrix

From `bench/REPORT_MATRIX.md` (2026-05-13, 3 gold-annotated corpora: `openmed` = GraSCCo PHI synthetic German clinical, `synth_clinical` = anonde-generated DE, `ai4privacy_en` = 5000 EN PII docs). **Lower leak rate = better.**

| Engine | openmed leak | synth_clinical leak | ai4privacy_en leak | Median latency |
|---|---:|---:|---:|---:|
| anonde-patterns (no NER) | 22.85% | 11.76% | 55.36% | ≤1 ms |
| **anonde-gliner** ⭐ production | **17.15%** | **11.15%** | **24.95%** | 59–927 ms |
| gliner-py (Python sidecar) | 50.00% | 25.36% | 25.82% | 77–593 ms |
| Microsoft Presidio | — (EN-only) | — | 28.37% | 6 ms |
| OpenAI Privacy Filter | 98.38% | — | — | 3174 ms |

**Read**:
- anonde-gliner beats every other engine on leak rate on every corpus.
- On English, the closest competitor is Presidio at 28.37% (anonde-gliner: 24.95%).
- On German clinical, OpenAI Privacy Filter is unusable (98% leak — almost blind).
- `gliner-py` is the same model run via the official Python `gliner` library; it confirms our Go-native wrap produces correct inference (it's a parity check, not a competitor).

The strict-F1 column intentionally not shown in this overview — it penalises wider-than-gold spans, which doesn't matter for redaction. See `bench/REPORT_MATRIX.md` for the full strict / partial / type-agnostic F1 grid.

### Running the bench locally

```bash
# fast subset — what CI runs (~10–15 min cold cache, ~2 min warm)
make -C bench corpus-openmed
make -C bench corpus-synth_clinical

# full matrix across all gold corpora + English
make -C bench matrix         # → bench/REPORT_MATRIX.md + bench/results_matrix.csv

# DE-only / EN-only subsets
make -C bench matrix-de
make -C bench matrix-en

# add the slow engines (off by default — openai-pf is ~80 sec/doc on CPU)
make bench/corpora/openmed/data/anonde_openai-pf.jsonl
```

Each corpus has its own `Makefile` and `README.md` under `bench/corpora/<NAME>/` documenting data provenance, gold-annotation source, and any DUA/registration requirements.

## Deployment

Two Fly.io variants of the same `cmd/platform` HTTP API:

| File | What it ships | Image size | When to use |
|---|---|---:|---|
| `Dockerfile.platform` | Pure Go binary, no NER, no CGO | ~12 MB | patterns-only deployments; max throughput |
| `Dockerfile.platform-ner` | Same binary + libonnxruntime + baked GLiNER model | ~470 MB | production: detects PERSON/ORG/etc. via GLiNER |

`fly.toml` deploys the patterns-only image; `fly.ner.toml` deploys the NER image to the same app. Both target `anonde-platform.fly.dev`.

The NER image:
- Uses `gcr.io/distroless/cc-debian12` (needs glibc for libonnxruntime).
- Downloads `libonnxruntime.so.1.26.0` from Microsoft's release tarball at build time and copies it to `/usr/lib/x86_64-linux-gnu/libonnxruntime.so.1`.
- Bakes the GLiNER ONNX + tokenizer into `/models/` so first-request startup needs no outbound network.
- Warms the recognizer at process start via `WARMUP_ON_START=1` (set in `fly.ner.toml`) so the first user request doesn't pay the model-init cost.

### Required env vars (NER variant)

```bash
ANALYZER_BACKEND=gliner
GLINER_MODELS_DIR=/models
GLINER_MODEL=knowledgator/gliner-pii-base-v1.0
GLINER_ONNX_FILE=onnx/model_quint8.onnx
GLINER_THRESHOLD=0.40
ORT_SO_PATH=/usr/lib/x86_64-linux-gnu/libonnxruntime.so.1
```

All defaults are wired in `Dockerfile.platform-ner`; you don't need to set them yourself unless you're swapping the model.

## HTTP API

```bash
# Anonymize + mint reversible tokens
curl -sS -X POST https://anonde-platform.fly.dev/v1/ingest \
  -H "Content-Type: application/json" \
  -d '{"tenant_id":"t1","doc_id":"d1","content":"Patient Herr Müller, …","language":"de"}'

# Exchange tokens back for cleartext (vault lookup)
curl -sS -X POST https://anonde-platform.fly.dev/v1/detokenize \
  -H "Content-Type: application/json" \
  -d '{"tenant_id":"t1","doc_id":"d1","tokens":["<PERSON_T1_000001>"], "actor":"clinician-42","purpose":"chart-review"}'

# One-shot reveal (substitutes tokens in a body of text in one call)
curl -sS -X POST https://anonde-platform.fly.dev/v1/reveal -d '{ … }'

# Health
curl -sS https://anonde-platform.fly.dev/healthz
```

Full schemas in `PLATFORM_ENDPOINTS.md`. Memory-vault TTLs and request-size limits are env-configurable:

- `MEMORY_VAULT_TTL` (default `5m`) — token ↔ cleartext retention.
- `MEMORY_STORE_TTL` (default `5m`) — anonymized-document retention.
- `MAX_CONTENT_BYTES` (default 10 MiB) — request body cap.

## Architecture

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
├── cmd/platform/              # HTTP service
├── internal/platform/         # service + in-memory vault/store/policy
└── bench/                     # single bench harness (was vs/, benchmark/, bench/parity/)
    ├── Makefile               # top-level `make matrix`, `make matrix-de`, `make matrix-en`, …
    ├── corpora/<NAME>/        # per-corpus Makefile + loader + data + gold
    ├── runners/               # one Go runner, three Python sidecars (gliner, openai_pf, presidio)
    ├── probes/                # diagnostic loaders for hugot, gliner
    └── scoring/               # compare.py, render_matrix.py, label_map.yaml
```

### Conflict resolution (the non-obvious part)

`RemoveConflicts` keeps the highest-scoring span when two overlap — **except** for entity types where NER is more reliable than heuristic patterns (PERSON, ORGANIZATION, LOCATION, AGE, PROFESSION, NRP). For those, an NER finding beats a pattern finding regardless of score. Pattern recognizers like `DEAnomalyRecognizer` produce fixed scores (0.85); GLiNER produces sigmoid floats (0.4–0.85); without this rule patterns always won and the NER's contextual judgement was wasted.

For structured types (IBAN, PHONE_NUMBER, DATE_TIME, EMAIL_ADDRESS, …) the score-only rule still applies — regex+checksum precision matters more than NER context there.

See `analyzer/result.go::shouldReplace` for the implementation.

## Anonymization operators

Configure per-entity via `anonymizer.AnonymizerConfig`. Use `"*"` for a catch-all default.

### Replace

```go
&operators.Replace{NewValue: "<EMAIL>"}   // → <EMAIL>
&operators.Replace{}                       // → <EMAIL_ADDRESS> (entity type as tag)
```

### Redact / Mask / Hash / Encrypt

```go
&operators.Redact{}
&operators.Mask{MaskingChar: "*", CharsToMask: 4, FromEnd: true}   // +1-800-555-**** 
&operators.Hash{HashType: operators.HashSHA256}
&operators.Encrypt{Key: "32-byte-aes-key-……………………………"}            // AES-GCM, base64 nonce+ct
operators.Decrypt(value, key)                                       // reversible
```

### Synthesize — structurally-valid fake data

Replaces PII with realistic fakes that pass the same checksums as the original (Luhn for cards, MOD-97 for IBAN, valid SSN area codes, same IP class, etc.). The result looks real but contains no actual personal information — useful for staging environments, test fixtures, demo videos.

```go
&operators.Synthesize{}                              // random per call
&operators.Synthesize{Consistent: true}              // globally deterministic: same input → same fake
&operators.Synthesize{Consistent: true, DocumentScoped: true}  // per-document aliasing; call .Reset()
```

See the prior README's table for per-entity-type generation rules — that section is unchanged.

## CI

`.github/workflows/bench.yml` runs on every push to `main` and every PR whose changes touch `analyzer/**`, `bench/**`, `cmd/platform/**`, or the build chain. The workflow:

1. Builds the default (no-CGO) target.
2. Builds the `-tags hugot` target with CGO.
3. Runs the Go unit-test suite.
4. Runs `make corpus-openmed && make corpus-synth_clinical` — patterns + GLiNER + GLiNER-py sidecar across two German corpora.
5. Renders `bench/REPORT_MATRIX.md` and uploads it (+ `results_matrix.csv` + per-cell findings JSONLs) as workflow artifacts.
6. **Guard rail**: fails the job if either GLiNER cell produced 0 NER-attributable findings (caught a real silent-fallback bug the first time it landed).

Headline is rendered into the GitHub Actions job summary, so PR reviewers see numbers without downloading artifacts.

Local dev installs the bench Python deps via:

```bash
pip install -r bench/requirements.txt
```

The Presidio cell needs `presidio-analyzer + spacy + en_core_web_lg` separately (~700 MB; not in `requirements.txt` to keep CI fast):

```bash
pip install presidio-analyzer spacy
python -m spacy download en_core_web_lg
```

## Building custom engines

```go
registry := analyzer.NewRecognizerRegistry()
registry.Add(recognizers.NewEmailRecognizer())
registry.Add(recognizers.NewCreditCardRecognizer())
// add your own — interface in analyzer/recognizer.go

engine := analyzer.NewAnalyzerEngine(registry)
```

### Custom recognizer

```go
type MyRecognizer struct{}

func (r *MyRecognizer) Name() string                 { return "MyRecognizer" }
func (r *MyRecognizer) SupportedEntities() []string  { return []string{"MY_ENTITY"} }
func (r *MyRecognizer) SupportedLanguages() []string { return []string{"en", "*"} }

func (r *MyRecognizer) Analyze(ctx context.Context, text string, entities []string, lang string) ([]analyzer.RecognizerResult, error) {
	return []analyzer.RecognizerResult{
		{Start: 0, End: 5, Score: 0.9, EntityType: "MY_ENTITY", RecognizerName: r.Name()},
	}, nil
}
```

`SupportedLanguages()` returning `"*"` lets the recognizer match any language. NER-named recognizers (name suffix `NERRecognizer`) get auto-skipped under `DisableNER` and under the no-capitals heuristic.
