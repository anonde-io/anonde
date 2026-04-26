# anonde

A Go implementation of the PII detection and anonymization pipeline inspired by [Microsoft Presidio](https://github.com/microsoft/presidio).

Detect and anonymize personally identifiable information (PII) in text with pattern-based recognizers and optional named-entity recognition (NER). Zero CGO, zero Python, no model downloads required by default.

## Features

- **15 pattern recognizers** — email, phone, credit card, SSN, passport, IBAN, IP, MAC, URL, crypto wallets, dates, and more
- **Three NER backends** — prose (local, zero deps), Ollama (local inference), and Python Presidio (remote sidecar)
- **5 anonymization operators** — replace, redact, mask, hash, encrypt (AES-GCM)
- **Parallel dispatch** — all recognizers run concurrently via goroutines
- **Conflict resolution** — overlapping spans are resolved by score then length
- **`DisableNER` flag** — pattern-only mode for maximum throughput (200–400× faster)
- **Capitalised-word pre-filter** — applied to local prose/Ollama NER for speed; remote Presidio backend always evaluates text

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
│       ├── Pattern recognizers (15) — regex + validation
│       ├── NERRecognizer       # prose-based, zero deps
│       ├── OllamaNERRecognizer # local Ollama inference
│       └── PresidioRemoteNERRecognizer # Python Presidio sidecar
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

### prose (default — zero dependencies)

Uses the [prose](https://github.com/jdkato/prose) NLP library. No API keys, no model downloads, works offline.

```go
engine := anonde.DefaultAnalyzerEngine()
```

### Ollama (local inference — data never leaves the machine)

Calls a local [Ollama](https://ollama.com) daemon. GPU-accelerated on Apple Silicon (MPS) and NVIDIA (CUDA). No API keys, no internet required after the model is pulled.

```go
// defaults: endpoint="http://localhost:11434", model="phi3:mini"
engine := anonde.DefaultAnalyzerEngineWithOllama("", "")

// custom endpoint and model
engine := anonde.DefaultAnalyzerEngineWithOllama("http://localhost:11434", "llama3.2:1b")
```

**Setup Ollama:**
```bash
brew install ollama
ollama serve
ollama pull phi3:mini   # ~2.3 GB, good accuracy/speed balance
# or
ollama pull llama3.2:1b # ~1.3 GB, fastest
```

### Python Presidio sidecar (highest quality, remote service)

Calls a Python Presidio Analyzer service over HTTP (`POST /analyze`) and maps the response into `PERSON`, `LOCATION`, `ORGANIZATION`, and `NRP`.

```go
// default endpoint: http://localhost:3000
engine := anonde.DefaultAnalyzerEngineWithPresidioRemote("")

// custom endpoint
engine := anonde.DefaultAnalyzerEngineWithPresidioRemote("http://localhost:3000")
```

### Comparison

| Backend | Accuracy | Latency | Dependencies | Data leaves machine |
|---|---|---|---|---|
| prose | Good | ~1 ms | none | No |
| Ollama | High | ~50–200 ms | Ollama daemon | No |
| Python Presidio (remote) | Highest | ~20–150 ms + network | Presidio service | No (if self-hosted) |

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

Run benchmarks:
```bash
# prose NER (default)
go test ./benchmark/ -bench=. -benchmem

# pattern-only (DisableNER: true) — maximum throughput
go test ./benchmark/ -bench=BenchmarkBulkPatternOnly -benchmem
```

Representative results on Apple M-series (prose NER backend):

| Benchmark | Throughput |
|---|---|
| Analyze short (1 entity) | ~5,000 ops/s |
| Analyze corpus (5 mixed docs) | ~800 ops/s |
| Pattern-only 1 MB | ~150 MB/s |
| Pattern-only batch 1000 docs parallel | ~400 MB/s |

Pattern-only mode (`DisableNER: true`) is 200–400× faster than Python Presidio for structured PII (emails, SSNs, credit cards).

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
- `docker-compose.yml` — local deployment with `platform` (default analyzer backend: `prose`, no model downloads) and optional `presidio-analyzer`, `ollama`, `policy`, and `vault-db`

Run with Docker (no Ollama required):

```bash
docker compose up --build -d
```

Test ingest (anonymize):

```bash
curl -sS http://localhost:8080/v1/ingest \
  -H 'content-type: application/json' \
  -d '{
    "tenant_id":"acme",
    "doc_id":"doc-1",
    "content":"John Doe SSN 123-45-6789 and email john@example.com"
  }'
```

The response includes `anonymized_content` and `tokens`, for example:
`<US_SSN_ACME_000001>` and `<EMAIL_ADDRESS_ACME_000002>`.

Test detokenize (de-anonymize) with returned tokens:

```bash
curl -sS http://localhost:8080/v1/detokenize \
  -H 'content-type: application/json' \
  -d '{
    "tenant_id":"acme",
    "doc_id":"doc-1",
    "actor":"local-tester",
    "purpose":"manual verification",
    "tokens":["<US_SSN_ACME_000001>","<EMAIL_ADDRESS_ACME_000002>"]
  }'
```

Test reveal (de-anonymize in place) by passing anonymized content directly:

```bash
curl -sS http://localhost:8080/v1/reveal \
  -H 'content-type: application/json' \
  -d '{
    "tenant_id":"acme",
    "doc_id":"doc-1",
    "actor":"local-tester",
    "purpose":"manual verification",
    "content":"Hi I am <PERSON_ACME_000001> SSN <US_SSN_ACME_000002> and email <EMAIL_ADDRESS_ACME_000003>"
  }'
```

The response includes `deanonymized_content` with tokens replaced inline, plus the `resolved` token map.

Enable Ollama only when needed:

```bash
docker compose --profile ollama up --build -d
```

Then set `ANALYZER_BACKEND=ollama` for `platform` (and optionally `OLLAMA_ENDPOINT` / `OLLAMA_MODEL`).

Enable Python Presidio only when needed:

```bash
docker compose --profile presidio up --build -d
```

Then set `ANALYZER_BACKEND=presidio` for `platform` (and optionally `PRESIDIO_ENDPOINT`; default inside Docker network is `http://presidio-analyzer:3000`).

## License

MIT
