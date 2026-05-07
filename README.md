# anonde

A Go implementation of the PII detection and anonymization pipeline inspired by [Microsoft Presidio](https://github.com/microsoft/presidio).

Detect and anonymize personally identifiable information (PII) in text with pattern-based recognizers and optional named-entity recognition (NER). Zero CGO, zero Python, no model downloads required by default.

## Features

- **15 pattern recognizers** — email, phone, credit card, SSN, passport, IBAN, IP, MAC, URL, crypto wallets, dates, and more
- **Two NER backends** — Hugot/ONNX (default, in-process transformer) and Ollama (local LLM inference). Plus a patterns-only mode when no NER is wanted.
- **5 anonymization operators** — replace, redact, mask, hash, encrypt (AES-GCM)
- **Parallel dispatch** — all recognizers run concurrently via goroutines
- **Conflict resolution** — overlapping spans are resolved by score then length
- **`DisableNER` flag** — pattern-only mode for maximum throughput (200–400× faster)
- **Capitalised-word pre-filter** — applied to Hugot/Ollama NER to skip lowercase-only text without losing recall

## Installation

```bash
go get github.com/moogacs/anonde
```

Requires Go 1.22+.

## Quick Start

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
    text := `Hi, I'm John. My email is john@example.com and my phone is +1-800-555-0199.
My SSN is 123-45-6789 and credit card 4111111111111111.`

    analyzerEngine := anonde.DefaultAnalyzerEngine()
    anonymizerEngine := anonde.DefaultAnonymizerEngine()

    results, err := analyzerEngine.Analyze(context.Background(), text, analyzer.AnalysisConfig{
        Language:        "en",
        ScoreThreshold:  0.3,
        RemoveConflicts: true,
    })
    if err != nil {
        log.Fatal(err)
    }

    for _, r := range results {
        fmt.Printf("[%d:%d] %-20s score=%.2f  %q\n",
            r.Start, r.End, r.EntityType, r.Score, text[r.Start:r.End])
    }

    cfg := anonymizer.AnonymizerConfig{
        "EMAIL_ADDRESS": &operators.Replace{NewValue: "<EMAIL>"},
        "PHONE_NUMBER":  &operators.Mask{CharsToMask: 4, FromEnd: true},
        "CREDIT_CARD":   &operators.Redact{},
        "US_SSN":        &operators.Hash{HashType: operators.HashSHA256},
        "*":             &operators.Replace{}, // default for all other entities
    }

    out, err := anonymizerEngine.Anonymize(text, results, cfg)
    if err != nil {
        log.Fatal(err)
    }
    fmt.Println(out.Text)
}
```

## Architecture

```
anonde
├── analyzer/
│   ├── AnalyzerEngine          # orchestrates recognizers in parallel
│   ├── RecognizerRegistry      # thread-safe recognizer store
│   ├── AnalysisConfig          # per-call settings
│   └── recognizers/
│       ├── Pattern recognizers (40+) — regex + validation + checksums
│       ├── HugotNERRecognizer  # in-process ONNX transformer (default)
│       └── OllamaNERRecognizer # local Ollama inference (opt-in)
└── anonymizer/
    ├── AnonymizerEngine        # applies operators to detected spans
    ├── AnonymizerConfig        # entity → operator map (supports "*" wildcard)
    └── operators/
        ├── Replace, Redact, Mask, Hash, Encrypt
```

The `AnalyzerEngine.Analyze` call:
1. Filters recognizers by language and requested entity types
2. Skips all NER if `DisableNER` is set
3. Applies a capitalised-word speed heuristic to local `NERRecognizer` only
4. Runs remaining recognizers concurrently
5. Filters results below `ScoreThreshold`
6. Optionally removes overlapping spans (keeping highest-score, then longest)

## Pattern Recognizers

| Entity Type | Description | Score |
|---|---|---|
| `EMAIL_ADDRESS` | Email addresses | 1.0 |
| `PHONE_NUMBER` | International phone numbers | 0.75 |
| `CREDIT_CARD` | Visa, Mastercard, Amex, etc. (Luhn validated) | 0.3–1.0 |
| `IBAN_CODE` | International bank account numbers (MOD-97 validated) | 0.5–1.0 |
| `IP_ADDRESS` | IPv4 and IPv6 addresses | 0.95 |
| `MAC_ADDRESS` | MAC addresses | 0.9 |
| `URL` | Web URLs | 0.6 |
| `CRYPTO` | Bitcoin and Ethereum wallet addresses | 0.9 |
| `DATE_TIME` | Dates and times in many formats | 0.85 |
| `US_SSN` | US Social Security Numbers | 0.85 |
| `US_PASSPORT` | US passport numbers | 0.5 |
| `US_BANK_NUMBER` | US bank account numbers | 0.3 |
| `US_DRIVER_LICENSE` | US driver license numbers | 0.6 |
| `US_ITIN` | US Individual Taxpayer Identification Numbers | 0.85 |
| `MEDICAL_LICENSE` | Medical license numbers | 0.5 |

## NER Options

Named entity recognition detects `PERSON`, `LOCATION`, `ORGANIZATION`, and `NRP` (nationalities/religions/political groups).

### Hugot / ONNX (default — recommended)

Runs a pre-trained transformer NER model in-process via ONNX Runtime. No Python sidecar, no external service. The model is downloaded automatically on first run into `~/.cache/anonde/models` (~270 MB for the default `Isotonic/distilbert_finetuned_ai4privacy_v2`).

```go
// auto-downloads the default model on first call
engine := anonde.DefaultAnalyzerEngineWithHugot("", "", true)

// custom models dir + model id
engine := anonde.DefaultAnalyzerEngineWithHugot(
    "/var/lib/anonde/models",
    "Isotonic/distilbert_finetuned_ai4privacy_v2",
    true,
)
```

Benchmark results vs Microsoft Presidio default (`en_core_web_lg`) on a 500-doc slice of `ai4privacy/pii-masking-200k`:

| Entity | anonde+Hugot F1 | Presidio F1 |
|---|---:|---:|
| PERSON | 0.93 | 0.44 |
| LOCATION | 0.86 | 0.26 |
| EMAIL_ADDRESS | 1.00 | 1.00 |
| IP_ADDRESS | 1.00 | 1.00 |
| US_SSN | 0.71 | 0.71 |
| CREDIT_CARD | 0.76 | 0.06 |
| PHONE_NUMBER | 0.72 | 0.47 |

8 wins, 4 ties, 0 losses across 12 entity types. Reproduce: `bench/parity/`. Full report: `bench/parity/REPORT_FULL.md`.

### Patterns-only (no NER, no model)

Use this when you can't tolerate a model download and the entities you care about are all pattern-detectable (email, phone, IP, MAC, IBAN, credit card, US SSN, crypto, regional IDs). Faster than any NER backend; gives up PERSON / LOCATION / ORGANIZATION detection entirely.

```go
engine := anonde.DefaultAnalyzerEngine() // patterns only — no NER
```

Or via the platform service: `ANALYZER_BACKEND=patterns`.

### Ollama (local LLM inference)

Calls a local [Ollama](https://ollama.com) daemon. GPU-accelerated on Apple Silicon (MPS) and NVIDIA (CUDA). Slower per call than Hugot; useful when you already have an Ollama setup.

```go
// defaults: endpoint="http://localhost:11434", model="phi3:mini"
engine := anonde.DefaultAnalyzerEngineWithOllama("", "")

// custom endpoint and model
engine := anonde.DefaultAnalyzerEngineWithOllama("http://localhost:11434", "llama3.2:1b")
```

### Comparison

| Backend | NER quality | Latency (median) | Dependencies | Data leaves machine |
|---|---|---|---|---|
| Hugot (default) | High (≥ Presidio default) | ~140 ms | model download on first run (~270 MB) | No |
| Patterns-only | n/a (no NER) | ~1 ms | none | No |
| Ollama | Depends on model | ~50–500 ms | Ollama daemon | No |

> The previous `prose`-based NER backend was removed — its quality fell below the parity bar (PERSON F1 0.58 on the benchmark corpus, vs Hugot's 0.93 and the 0.7 absolute floor). If you don't want a model download, prefer patterns-only over a known-low-quality NER fallback.

Microsoft Presidio is **not** a runtime backend. It is retained only as a benchmarking partner under `bench/parity/` — see that directory's `README.md` to reproduce the comparison. The `analyzer/recognizers/ner_presidio_remote.go` recognizer was removed in the Phase B detach; if you need Presidio's runtime API, run it yourself and call it from your application code.

## Deployment & Model Distribution

The "single binary" guarantee is about **no Python sidecar at runtime**, not about embedding all weights into the executable. The Go binary itself is ~30 MB; the default Hugot model is a separate ~270 MB asset, fetched once and cached. This is the same shape as Ollama, llama.cpp, and transformers.

### Three deployment shapes

| Shape | Binary | Model | When to use |
|---|---|---|---|
| **Default (auto-download)** | shipped, small | fetched from HuggingFace Hub on first request into `~/.cache/anonde/models` | library users (`go get`), dev, any environment with network at deploy time |
| **Docker with named volume** | in image | fetched into the `hugot_models` named volume on first request | production self-hosting via `docker-compose.yml` |
| **Air-gapped / pre-staged** | shipped | operator pre-populates the cache or volume before first request | no-egress production, regulated environments, reproducible builds |

### Pre-staging the model (air-gapped)

```go
// In an init container or build step:
import "github.com/knights-analytics/hugot"

opts := hugot.NewDownloadOptions()
_, _ = hugot.DownloadModel(ctx,
    "Isotonic/distilbert_finetuned_ai4privacy_v2",
    "/var/lib/anonde/models",
    opts,
)
```

Then run anonde with `AutoDownload: false` and `ModelsDir: "/var/lib/anonde/models"`. Subsequent requests use the cache without ever calling out to HuggingFace.

### Should we ever `go:embed` the model?

By default, no. 270 MB embedded into a Go binary makes `go build` slow, bloats container images, and couples binary version to model version (independent rotation breaks). Teams that genuinely want one fat artifact can add a build-tag-gated `//go:embed` themselves — it's a ~10-line addition. The benchmark numbers in `bench/parity/REPORT_FULL.md` are reproducible without it.

## Anonymization Operators

Configure per-entity operators via `anonymizer.AnonymizerConfig`. Use `"*"` as the key for a default operator applied to entities with no specific mapping.

### Replace

Replaces the entity with a fixed value or the entity type tag.

```go
&operators.Replace{NewValue: "<EMAIL>"}   // → <EMAIL>
&operators.Replace{}                       // → <EMAIL_ADDRESS>
```

### Redact

Removes the entity entirely.

```go
&operators.Redact{}
```

### Mask

Replaces characters with a masking character.

```go
&operators.Mask{MaskingChar: "*", CharsToMask: 4, FromEnd: true}
// "+1-800-555-0199" → "+1-800-555-****"
```

### Hash

Replaces with a hex digest.

```go
&operators.Hash{HashType: operators.HashSHA256}
&operators.Hash{HashType: operators.HashSHA512}
```

### Encrypt / Decrypt

AES-GCM encryption. Stores the result as a base64-encoded `nonce+ciphertext` string that can be decrypted later.

```go
op := &operators.Encrypt{Key: "your-32-byte-aes-key-here!!!!!!"} // 16, 24, or 32 bytes

// Decrypt
original, err := operators.Decrypt(encryptedValue, key)
```

## Configuration

```go
cfg := analyzer.AnalysisConfig{
    Language:        "en",                              // recognizer language filter
    Entities:        []string{"EMAIL_ADDRESS", "PERSON"}, // empty = all
    ScoreThreshold:  0.5,                               // filter low-confidence results
    RemoveConflicts: true,                              // resolve overlapping spans
    DisableNER:      false,                             // true = pattern-only, maximum throughput
}
```

## Building Custom Engines

```go
import (
    "github.com/moogacs/anonde/analyzer"
    "github.com/moogacs/anonde/analyzer/recognizers"
)

registry := analyzer.NewRecognizerRegistry()
registry.Add(recognizers.NewEmailRecognizer())
registry.Add(recognizers.NewCreditCardRecognizer())
// add your own recognizers implementing analyzer.EntityRecognizer

engine := analyzer.NewAnalyzerEngine(registry)
```

### Implementing a Custom Recognizer

```go
type MyRecognizer struct{}

func (r *MyRecognizer) Name() string                 { return "MyRecognizer" }
func (r *MyRecognizer) SupportedEntities() []string  { return []string{"MY_ENTITY"} }
func (r *MyRecognizer) SupportedLanguages() []string { return []string{"en"} }

func (r *MyRecognizer) Analyze(ctx context.Context, text string, entities []string, lang string) ([]analyzer.RecognizerResult, error) {
    // return []analyzer.RecognizerResult{{Start: 0, End: 5, Score: 0.9, EntityType: "MY_ENTITY"}}
    return nil, nil
}
```

## Benchmarks

Two benchmark surfaces:

```bash
# Throughput micro-benchmarks (in-process, no model)
go test ./benchmark/ -bench=. -benchmem
go test ./benchmark/ -bench=BenchmarkBulkPatternOnly -benchmem

# End-to-end PII parity comparison vs Microsoft Presidio
# (5000-doc subset of ai4privacy/pii-masking-200k)
# See bench/parity/README.md for the full reproduction recipe.
```

Representative micro-bench results on Apple M-series (patterns-only mode):

| Benchmark | Throughput |
|---|---|
| Pattern-only 1 MB | ~150 MB/s |
| Pattern-only batch 1000 docs parallel | ~400 MB/s |

Patterns-only mode (`DisableNER: true`) skips NER entirely and is dramatically faster than any NER-enabled pipeline for structured PII (emails, SSNs, credit cards, IBAN, IP, MAC, etc.). End-to-end parity numbers vs Presidio are in `bench/parity/REPORT_FULL.md`.

## Running the Example

```bash
go run ./examples/main.go
```

## Local Platform Scaffold

This repo now includes a local-first service scaffold for anonymize/de-anonymize flows:

- `cmd/platform` — HTTP API:
  - `POST /v1/ingest`
  - `POST /v1/detokenize`
  - `POST /v1/reveal`
  - `GET /healthz`
- `internal/platform` — service contracts for policy, vault, and store
- `docker-compose.yml` — local deployment with `platform` (default analyzer backend: `hugot`, model auto-downloaded into a named volume) and optional `ollama`, `policy`, and `vault-db`

Endpoint request/response examples are documented in:
`PLATFORM_ENDPOINTS.md`

A standalone guide for ingest/reveal flows is available at:
`INGEST_REVEAL_README.md`

In-memory retention controls (to avoid unbounded growth):

- `MEMORY_VAULT_TTL` (default: `5m`) — token-to-cleartext mappings retention
- `MEMORY_STORE_TTL` (default: `5m`) — anonymized document retention
- Set either of the above to `0` to disable expiry for that store

Request-size limit:

- `MAX_CONTENT_BYTES` (default `10485760` = 10 MiB) — caps every ingest / reveal / detokenize request body. Oversized requests return HTTP `413`. Set to `0` to disable.

### Server-log ingestion

For line-oriented server logs use `content_format: "logs"` (alias `log`):

- Each line is processed independently; line structure is preserved.
- ANSI escape sequences (e.g. `\x1b[31m`) are stripped before analysis.
- Invalid UTF-8 bytes are replaced with `U+FFFD` so non-UTF-8 input never trips the recognizers.
- Each line is auto-classified per line as JSON (recursed through string leaves) or plain text.

For strictly newline-delimited JSON streams use `content_format: "ndjson"` — every non-empty line must parse as JSON or the request is rejected.

To narrow the recognizer pipeline for log volume, IngestRequest accepts:

- `entities`: allow-list of entity types (e.g. `["EMAIL_ADDRESS","IP_ADDRESS"]`)
- `disable_ner`: skip PERSON/LOCATION/ORGANIZATION/NRP for maximum throughput
- `score_threshold`: drop findings below the given confidence
- `language`: recognizer language filter
> Tokens are always minted per-document. Tenant-scoped token reuse (the same email getting the same token across many docs) was scoped out — tracked in `TODO.md`.

See `PLATFORM_ENDPOINTS.md` for full request/response schemas.

Default `docker compose up` brings up `platform` with the Hugot backend. The model is downloaded into the `hugot_models` named volume on first request.

Enable Ollama only when needed:

```bash
docker compose --profile ollama up --build -d
```

Then set `ANALYZER_BACKEND=ollama` for `platform` (and optionally `OLLAMA_ENDPOINT` / `OLLAMA_MODEL`).

### Benchmarking against Microsoft Presidio

Presidio is no longer a runtime backend. To compare anonde against Presidio's detection on a labeled corpus, see `bench/parity/README.md` — it provisions a Python venv with `presidio-analyzer`, runs both engines over `ai4privacy/pii-masking-200k`, and emits a span-exact F1 comparison report.

## License

MIT
