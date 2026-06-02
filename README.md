<p align="center">
  <img src="docs/assets/brand-mark-animated.svg" alt="anonde" width="128" height="128">
</p>

<h1 align="center">anonde</h1>

<p align="center">
  <strong>Make regulated data safe to use with LLMs.</strong><br>
  Anonymize before the model sees it. Reveal only where it's allowed.<br>
  <sub>A developer toolkit for building copilots, RAG, and agents over healthcare, finance, and enterprise data.</sub>
</p>

<p align="center">
  <a href="https://anonde.io">anonde.io</a> ·
  <a href="https://anonde.io">Live demo</a> ·
  <a href="docs/QUICKSTART.md">Quickstart</a> ·
  <a href="bench/REPORT_MATRIX.md">Benchmarks</a>
</p>

<p align="center">
  <a href="https://github.com/anonde-io/anonde/actions/workflows/bench.yml"><img src="https://github.com/anonde-io/anonde/actions/workflows/bench.yml/badge.svg" alt="Bench"></a>
  <a href="LICENSE"><img src="https://img.shields.io/badge/license-Apache_2.0-blue.svg" alt="License: Apache 2.0"></a>
  <img src="https://img.shields.io/badge/Go-1.26-00ADD8.svg" alt="Go 1.26">
  <img src="https://img.shields.io/badge/image-12MB%20(patterns)%20%7C%20~770MB%20(NER)-2496ED.svg" alt="Image size">
</p>

---

## See it in 10 seconds

**Your input** (text, JSON, NDJSON, logs, or PDF; scanned PDFs are OCR'd automatically):

```text
From: sarah.chen@acme.example
Hi, this is Sarah Chen (+1 415 555 0142). Card 4111-1111-1111-1111
was charged twice on 2024-03-15 for $89.99, please refund.
```

**What the LLM sees:**

```text
From: <EMAIL_ADDRESS_1>
Hi, this is <PERSON_1> (<PHONE_NUMBER_1>). Card <CREDIT_CARD_1>
was charged twice on <DATE_TIME_1> for $89.99, please refund.
```

**What your user sees:** the original text, restored inside your trust boundary, gated by `actor` + `purpose` audit metadata. Tokens are stable per `(tenant, doc)` and reversible via an in-memory vault you control.

## Why anonde

- **Drop-in for any LLM workflow.** Same shape in, same shape out. Plug it between your app and OpenAI, Anthropic, Bedrock, Ollama, or your own model. They see only tokens.
- **Wins on leak rate.** anonde-ner has the lowest leak rate on 25 of 29 gold-annotated corpora the bench tracks across English, German, Spanish, French and Italian — covering clinical, legal, finance, structured PII, and adversarial / out-of-distribution text. ([numbers](#benchmarks))
- **Local-first.** Ships as a Go library or a Docker image you run yourself. No cloud calls. NER models are baked into the image, so there is no outbound HuggingFace traffic at request time.
- **Multilingual.** Open-set NER (GLiNER) plus 52 region-aware pattern recognizers covering 12+ jurisdictions: international IDs, US, UK, Germany, Italy, Spain, Australia, India, Poland, Singapore, Finland, Korea.
- **Reversible, audited.** Tokens map back to cleartext only where you allow it. The reveal call requires `actor` + `purpose` and is the only place plaintext comes back.
- **Recall-biased.** Missing a span is a leak; tokenising one too many is cheap. The bench tracks this explicitly via `leak_rate` (lower is better).

## Quick start

Two ways to run anonde — pick the one that matches how you ship.

### Docker (HTTP server, fastest)

One command, no Go toolchain needed. The patterns-only image is ~12 MB
and cold-starts in <1s; the NER image (~770 MB) bakes in GLiNER +
libonnxruntime for PERSON / ORG / LOC detection.

```bash
# Patterns-only (no model download, no CGO)
docker run --rm -p 8081:8080 ghcr.io/anonde-io/anonde:latest

# NER (GLiNER + libonnxruntime baked in)
docker run --rm -p 8081:8080 ghcr.io/anonde-io/anonde-ner:latest
```

Then anonymize a request:

```bash
curl -sS -X POST http://localhost:8081/v1/anonymizations \
  -H "Content-Type: application/json" \
  -d '{"tenant_id":"demo","content":"Hi, this is Sarah Chen (sarah.chen@acme.example)."}'
# → { "id": "anon_8f3c…", "anonymized_content": "...", "tokens": [...] }
```

Image variants, volumes, and docker compose profiles live in
[Run the HTTP server](#run-the-http-server) below.

### Go library

```bash
go get github.com/anonde-io/anonde
```

```go
package main

import (
	"context"
	"fmt"

	"github.com/anonde-io/anonde"
	"github.com/anonde-io/anonde/analyzer"
	"github.com/anonde-io/anonde/anonymizer"
	"github.com/anonde-io/anonde/anonymizer/operators"
)

func main() {
	text := `Hi, I'm Sarah Chen (sarah.chen@acme.example, +1 415 555 0142). Card 4111-1111-1111-1111.`

	engine := anonde.DefaultAnalyzerEngine()
	results, _ := engine.Analyze(context.Background(), text, analyzer.AnalysisConfig{
		Language:        "en",
		ScoreThreshold:  0.3,
		RemoveConflicts: true,
	})

	anon := anonde.DefaultAnonymizerEngine()
	out, _ := anon.Anonymize(text, results, anonymizer.AnonymizerConfig{
		"*": &operators.Replace{}, // → <PERSON_1>, <EMAIL_ADDRESS_1>, <PHONE_NUMBER_1>, <CREDIT_CARD_1>
	})
	fmt.Println(out.Text)
}
```

Default build is pure Go, no CGO. The `-tags hugot` build enables in-process NER (GLiNER, hugot); see [docs/DEPLOYMENT.md](docs/DEPLOYMENT.md).

## Claude Code hook

A local PII guard for [Claude Code](https://code.claude.com): it scans your
prompts and the actions the agent is about to take (`Bash`, `Write`, `Edit`)
and **warns** or **blocks** before PII reaches the model or leaves your machine.
Detection runs locally — patterns in-process by default, or full NER against a
running anonde server. Install one way:

```text
# Plugin (auto-registers the hooks; lowest friction)
/plugin marketplace add anonde-io/anonde
/plugin install anonde@anonde
```

```bash
# Or grab the binary directly
curl -fsSL https://raw.githubusercontent.com/anonde-io/anonde/main/install.sh | sh
go install github.com/anonde-io/anonde/cmd/anonde-hook@latest   # (needs a Go toolchain)
```

Setup, modes (`warn`/`block`), config, and the contract's limits are in the
[Claude Code hook README](plugins/claude-code/).

## Run the HTTP server

With Go:

```bash
ANALYZER_BACKEND=patterns ANONDE_ADDR=:8081 go run ./cmd/anonde/
```

With Docker (patterns-only image, ~12 MB):

```bash
docker build -f Dockerfile.anonde -t anonde:patterns .
docker run --rm -p 8081:8080 anonde:patterns
```

The NER variant (GLiNER + libonnxruntime baked in, ~770 MB) builds the same way from `Dockerfile.anonde-ner`. It ships the FP32 ONNX (`onnx/model.onnx`) by default; the matrix proved INT8 leaks ~6pp more PII overall. Memory-constrained deployments can opt back into INT8 with `GLINER_QUANT=int8` (saves ~240 MB image size at the cost of recall on multilingual legal / clinical text). See [docs/DEPLOYMENT.md](docs/DEPLOYMENT.md) for env vars and image internals.

Or with Docker Compose (mutually exclusive profiles, one runs at a time):

```bash
docker compose --profile patterns  up   # ~12 MB,    text/JSON only
docker compose --profile ner       up   # ~1.13 GB,  GLiNER + PDF + metrics on :9090
docker compose --profile ner-stack up   # ~2.65 GB,  lowest-leak GLiNER stack
```

Hit the running server:

```bash
curl -sS -X POST http://localhost:8081/v1/anonymizations \
  -H "Content-Type: application/json" \
  -d '{"tenant_id":"demo","content":"Hi, this is Sarah Chen (sarah.chen@acme.example)."}'
# → { "id": "anon_8f3c…", "anonymized_content": "...", "tokens": [...] }
```

### Port and listen address

The server binds to `:8080` by default. Override via either env var
(both are checked, in this precedence order):

| Env var        | Form              | Example                       |
|----------------|-------------------|-------------------------------|
| `ANONDE_ADDR`  | full address      | `ANONDE_ADDR=0.0.0.0:9000`    |
| `PORT`         | port only         | `PORT=9000` (Heroku/Cloud Run) |

```bash
# Local Go
ANONDE_ADDR=:9000 go run ./cmd/anonde

# Docker — set inside the container AND match the host port mapping
docker run --rm -e ANONDE_ADDR=:9000 -p 9000:9000 ghcr.io/anonde-io/anonde:latest
```

### Persistence

All three images set `ANONDE_DATA_DIR=/var/lib/anonde` and declare it
as a Docker `VOLUME`. Two files live there: the telemetry install ID,
and the bbolt vault DB when you opt into `STORE_BACKEND=bbolt`. Mount
a named volume for durability across `docker rm`:

```bash
docker run -v anonde-data:/var/lib/anonde -p 8081:8080 anonde:patterns
```

`docker-compose.yml` already wires a per-profile named volume. See
[docs/DEPLOYMENT.md](docs/DEPLOYMENT.md#persistent-data-directory)
for the full env-var precedence and library-mode behavior.

## HTTP API

The same server speaks three transports on one port:

- **REST/JSON** via grpc-gateway: `POST /v1/anonymizations`, `POST /v1/anonymizations/{id}/reveal|detokenize`, `DELETE /v1/anonymizations/{id}?tenant_id={tenant_id}`, `POST /v1/synthesize`, `GET /v1/version`. `id` is optional on create (server mints `anon_<hex>` if omitted); tenant lives in the request body / query for now and moves to a bearer-token header when auth lands. JSON fields are snake_case on the wire (`tenant_id`, `content_format`, `anonymized_content`, …); inputs also accept the camelCase form so generated gRPC clients work without translation.
- **Connect** (Connect/JSON, Connect/Protobuf, gRPC-Web): `POST /anonde.v1.Service/<Method>`.
- **Native gRPC** over HTTP/2 cleartext: same `/anonde.v1.Service/<Method>` path.

Plus two optional surfaces:

- **PDF redaction (raw bytes in, raw bytes out):** `POST /v1/anonymizations/pdf` accepts a raw `application/pdf` body and returns a redacted PDF; `GET /v1/anonymizations/{id}/reveal-pdf` returns the original. Tenant via the `X-Anonde-Tenant` header or `?tenantId=` query. Opt-in via `ANONDE_PDF_ENABLED=1`. Defined in the proto (`AnonymizePDF` / `RevealPDF` RPCs); also callable over gRPC / Connect, with `pdf_content` and `redacted_pdf` as base64-encoded `bytes` fields. See [PDFs, including scans (OCR fallback)](#pdfs-including-scans-ocr-fallback) below.
- **OpenAI-compatible proxy** at `POST /v1/chat/completions`. See [Use anonde as an OpenAI proxy](#use-anonde-as-an-openai-proxy) below.

Source of truth: [`proto/anonde/v1/anonde.proto`](proto/anonde/v1/anonde.proto). Regenerate handlers with `buf generate`. Full round-trip examples (text, JSON, PDF) live in [docs/QUICKSTART.md](docs/QUICKSTART.md).

## PDFs, including scans (OCR fallback)

anonde has two PDF surfaces; pick by use case:

1. **Text PDFs through the normal anonymize endpoint.** Send a
   base64-encoded PDF with `content_format: "pdf"` to
   `POST /v1/anonymizations`. The text layer is extracted via
   `ledongthuc/pdf`, fed into the same analyzer pipeline as text input,
   and you get the standard tokenised text + vault back. Reversible
   via `/v1/anonymizations/{id}/reveal`.
2. **Raw PDF in, redacted PDF out via `POST /v1/anonymizations/pdf`.**
   Send the raw PDF body. The server returns a redacted PDF with
   black boxes drawn over each PII span on the original page rasters.
   Reversible via `GET /v1/anonymizations/{id}/reveal-pdf`, which
   returns the original PDF bytes. Opt-in: start the server with
   `ANONDE_PDF_ENABLED=1` (requires the `anonde-ner` image or a local
   `-tags hugot` build with `pdftoppm` + `tesseract` on `PATH`).

When the PDF text layer is empty or shorter than
`ANONDE_OCR_TEXT_FLOOR` bytes (default 64), i.e. an image-only scan
or a photo-to-PDF, both surfaces transparently
rasterise each page with `pdftoppm` and OCR it with `tesseract`
before running the analyzer. No code change on the caller side.

```bash
# 1) Text PDF via the normal endpoint
B64=$(base64 -i invoice.pdf)
curl -sS -X POST http://localhost:8081/v1/anonymizations \
  -H "Content-Type: application/json" \
  -d "{\"tenant_id\":\"demo\",\"content_format\":\"pdf\",\"content\":\"$B64\"}"

# 2) Raw PDF body → redacted PDF body
curl -sS -X POST http://localhost:8081/v1/anonymizations/pdf \
  -H "Content-Type: application/pdf" \
  -H "X-Anonde-Tenant: demo" \
  --data-binary @scan.pdf \
  -D /tmp/headers.txt -o redacted.pdf
# /tmp/headers.txt carries X-Anonde-Id, X-Anonde-Entities,
# X-Anonde-Entity-Types, X-Anonde-Entity-Count (per-type) so you
# can log counts without a second request.

# Reveal the original bytes for that anonymization id
curl -sS -H "X-Anonde-Tenant: demo" \
  "http://localhost:8081/v1/anonymizations/$ANON_ID/reveal-pdf" \
  -o original.pdf
```

The `anonde-ner` and `anonde-ner-stack` images bundle `poppler-utils`
+ `tesseract-ocr` with `eng+deu+fra+spa+ita+ron` language packs, so
OCR is on by default there. The patterns-only image (`anonde`) does
not bundle them; `ExtractAnalyzable` silently skips the OCR fallback
when the binaries aren't on `PATH`, so the patterns-only image stays
~12 MB.

Whenever `ANONDE_PDF_ENABLED=1` the server eagerly loads a YOLOS
signature detector so the visual redactor covers signatures, stamps,
and logos that no OCR will see. The model is baked into `anonde-ner`
(FP32 by default; flip with the `SIGNATURE_QUANT={fp16,int8}` build
arg). Memory cost is ~500 MB resident; operators who can't afford it
should leave `ANONDE_PDF_ENABLED` unset and route PDFs to a separate
node.

Tune via env: `ANONDE_OCR_ENABLED`, `ANONDE_OCR_LANGS`,
`ANONDE_OCR_DPI`, `ANONDE_OCR_TEXT_FLOOR`, `ANONDE_PDF_ENABLED`,
`ANONDE_SIGNATURE_MODEL_PATH`. See
[docs/DEPLOYMENT.md](docs/DEPLOYMENT.md#pdf--ocr) for the full table.

## Anonymize PDFs

PDFs are a first-class HTTP endpoint, not a separate binary: every knob
the old `anonymize-pdf` CLI exposed (mode, dpi, box-padding, entities,
score-threshold, ocr-langs, …) binds from URL query parameters on
`POST /v1/anonymizations/pdf`. The redactor uses the same
`internal/content` primitives as the rest of the server, so output is
byte-identical regardless of transport.

```bash
# Run an NER server with PDF enabled (one liner)
docker run --rm -p 8081:8080 ghcr.io/anonde-io/anonde-ner:latest

# Visual redaction (default; black boxes on page rasters)
curl -X POST 'http://localhost:8081/v1/anonymizations/pdf' \
  -H 'Content-Type: application/pdf' -H 'X-Anonde-Tenant: demo' \
  --data-binary @in.pdf -o out.pdf

# Text-mode + mask operator (rerendered text PDF with '#' substitutions)
curl -X POST 'http://localhost:8081/v1/anonymizations/pdf?mode=text&operator=mask' \
  -H 'Content-Type: application/pdf' -H 'X-Anonde-Tenant: demo' \
  --data-binary @in.pdf -o out.pdf

# Restrict OCR languages + entity allow-list per request
curl -X POST 'http://localhost:8081/v1/anonymizations/pdf?ocr_langs=eng%2Bdeu&entities=PERSON&entities=LOCATION' \
  -H 'Content-Type: application/pdf' -H 'X-Anonde-Tenant: demo' \
  --data-binary @in.pdf -o out.pdf
```

See [`docs/DEVELOPER_GUIDE.md`](docs/DEVELOPER_GUIDE.md) for the full
field table.

## Use anonde as an OpenAI proxy

The lowest-friction integration: point your existing OpenAI SDK at
anonde instead of `api.openai.com`. anonde anonymizes the prompt,
forwards it to the real provider, de-anonymizes the response, and hands
it back in OpenAI shape. No plugin, no code change beyond the base URL.
Works with the raw OpenAI SDK, LangChain, or anything that speaks the
OpenAI API.

Start the server with the upstream configured:

```bash
ANONDE_OPENAI_BASE_URL=https://api.openai.com/v1 \
ANONDE_OPENAI_API_KEY=sk-...your-real-key... \
ANONDE_ADDR=:8081 go run ./cmd/anonde/
```

Then swap the base URL in your client:

```python
from openai import OpenAI

client = OpenAI(base_url="http://localhost:8081/v1", api_key="unused")

resp = client.chat.completions.create(
    model="openai/gpt-4o",   # provider/model; "openai/" is optional
    messages=[{"role": "user",
               "content": "Email a summary to sarah.chen@acme.example"}],
)
# The provider only ever saw <EMAIL_ADDRESS_…>; the reply you get back
# has the real address restored.
```

The endpoint is a single OpenAI-shaped `POST /v1/chat/completions`, so
the client base URL is byte-identical to a real OpenAI swap. The
upstream provider is selected in-band (the OpenRouter convention) by a
`provider/model` prefix on the `model` field: `openai/gpt-4o` routes to
OpenAI and forwards the bare `gpt-4o` upstream. A model with no prefix
defaults to OpenAI. v0.1 proxies OpenAI only; an `anthropic/…` model is
rejected with a clear error until Anthropic routing lands in v0.2.

| Env var | Default | Purpose |
|---|---|---|
| `ANONDE_OPENAI_BASE_URL` | `https://api.openai.com/v1` | Any OpenAI-compatible endpoint, incl. a local Ollama (`http://localhost:11434/v1`). |
| `ANONDE_OPENAI_API_KEY` | _(empty)_ | Forwarded as `Authorization: Bearer`. Leave empty for keyless upstreams like Ollama. |
| `ANONDE_PROXY_TENANT` | `openai-proxy` | Vault tenant when a request carries no `X-Anonde-Tenant` header. |
| `ANONDE_PROXY_TIMEOUT` | `120s` | Upstream request timeout (shared across all proxied providers). |

**Known limitation (v0.1):** non-streaming only. A `stream: true` request
is rejected with a clear error rather than silently downgraded.
Streaming SSE de-anonymization lands in v0.1.1. Anthropic and Gemini
upstreams (selected by the `anthropic/` / `gemini/` model prefix) are on
the roadmap.

## Write your own recognizer

Extensibility is the part Presidio gets right and we copied. A pattern
recognizer is a regex, a label, a language list, and an optional set of
context words that boost the score when they appear nearby.

**In-repo (joins every engine by default).** Drop the file into
`analyzer/recognizers/` and add one line to `anonde.go`:

```go
// analyzer/recognizers/my_id.go
package recognizers

import "regexp"

var myIDRE = regexp.MustCompile(`\bMID-\d{6,10}\b`)

// NewMyIDRecognizer detects MY_ID entities.
func NewMyIDRecognizer() *PatternRecognizer {
	return NewPatternRecognizerWithContext(
		"MyIDRecognizer",
		[]string{"MY_ID"},
		[]string{"*"},                                // languages; "*" runs on all
		[]namedPattern{{re: myIDRE, score: 1.0}},
		[]string{"member id", "membership", "mid"},   // context boosts
	)
}
```

```go
// anonde.go — register inside patternRecognizers()
func patternRecognizers() []analyzer.EntityRecognizer {
	return []analyzer.EntityRecognizer{
		// Generic / international
		recognizers.NewEmailRecognizer(),
		recognizers.NewPhoneRecognizer(),
		// …
		recognizers.NewMyIDRecognizer(),   // ← add your recognizer here
	}
}
```

**As a library consumer (no fork).** Implement
[`analyzer.EntityRecognizer`](analyzer/recognizer.go) in your own
package and attach it to the engine's registry at startup — useful when
you `go get` anonde and don't want to maintain a fork:

```go
type MyIDRecognizer struct{ re *regexp.Regexp }

func (r *MyIDRecognizer) Name() string                 { return "MyIDRecognizer" }
func (r *MyIDRecognizer) SupportedEntities() []string  { return []string{"MY_ID"} }
func (r *MyIDRecognizer) SupportedLanguages() []string { return []string{"*"} }

func (r *MyIDRecognizer) Analyze(_ context.Context, text string, _ []string, _ string) ([]analyzer.RecognizerResult, error) {
	var out []analyzer.RecognizerResult
	for _, m := range r.re.FindAllStringIndex(text, -1) {
		out = append(out, analyzer.RecognizerResult{
			Start: m[0], End: m[1], Score: 1.0,
			EntityType: "MY_ID", RecognizerName: "MyIDRecognizer",
		})
	}
	return out, nil
}

engine := anonde.DefaultAnalyzerEngine()
engine.Registry.Add(&MyIDRecognizer{re: regexp.MustCompile(`\bMID-\d{6,10}\b`)})
```

Either path joins the parallel-dispatch pipeline and the conflict
resolver handles overlap with existing recognizers automatically; NER
preferences (PERSON / ORG / LOC / AGE / PROFESSION / NRP) and the full
pipeline rules live in [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md).
The full 52-recognizer catalogue + how to add a model-backed (NER)
recognizer is in [docs/RECOGNIZERS.md](docs/RECOGNIZERS.md).

## Built for

- **Healthcare.** Chart summaries, discharge letters, clinical Q&A. Keep PHI off the wire to third-party models.
- **Finance.** KYC review, support triage, statement summarisation. Account numbers, card data, and PII stay inside your boundary.
- **Logs & telemetry.** Application logs, audit trails, SIEM exports, and traces often carry emails, IPs, account IDs, and free-text from users. Run them through anonde before they hit a remote LLM, a log aggregator, or a BI store.
- **Enterprise.** Internal copilots over support tickets, contracts, HR docs. Audit who reveals what, and why.

Want to see the full flow in the browser? [anonde.io](https://anonde.io).

## Benchmarks

Public bench matrix, re-run on every PR. The default NER image
(`ghcr.io/anonde-io/anonde-ner`) loads the **FP32 ONNX** GLiNER model;
the columns below are that engine (`anonde-ner`). One row per language
on the clinical de-identification axis (the cleanest apples-to-apples
slice across all five), plus a structured-PII row and an adversarial /
OOD row. **Lower leak rate is better.**

| Corpus | Slice | Best non-anonde baseline | anonde-ner | Δ |
|---|---|---|---:|---:|
| `synth_clinical_en` | English · clinical | presidio 20.3% | **3.3%** | ↓ 17.0 pp |
| `synth_clinical` | German · clinical | openai-pf 23.5% | **0.6%** | ↓ 22.9 pp |
| `meddocan_es` | Spanish · clinical | gliner-py 23.7% | **21.0%** | ↓ 2.7 pp |
| `synth_clinical_fr` | French · clinical | openai-pf 22.1% | **11.9%** | ↓ 10.2 pp |
| `synth_clinical_it` | Italian · clinical | openai-pf 25.1% | **18.2%** | ↓ 6.9 pp |
| `ai4privacy_en` | English · structured PII | openai-pf 15.5% | **15.1%** | ↓ 0.4 pp |
| `adversarial_de` | German · adversarial / OOD | openai-pf 32.1% | **8.2%** | ↓ 23.9 pp |

anonde-ner has the lowest leak rate on every row above. Across the **full matrix (29 corpora, 5 languages)** it's the lowest-leak engine on **25 of 29** corpora; the per-cell roll-up wins 19, ties 1, loses 4 across the 24 populated `(domain × language)` cells.

**Vs external baselines** on the corpora where they were benched:

- **Microsoft Presidio** on `ai4privacy_en` (English PII): anonde-ner **15.1%** vs Presidio 56.0%.
- **OpenAI Privacy Filter** on `openmed` (German clinical): anonde-ner **13.3%** vs OpenAI PF 35.6%.

Presidio and OpenAI Privacy Filter weren't run on every corpus: Presidio's bench harness uses its English pipeline only, and OpenAI Privacy Filter is ~80 s/doc on CPU, which makes it impractical on the larger corpora. Both engines can technically run on more languages; the bench numbers reflect what's been measured, not capability ceilings. Full grid (strict / partial / type-agnostic F1, all corpora, all engines) lives in [bench/REPORT_MATRIX.md](bench/REPORT_MATRIX.md).

## Docs

- [Quickstart](docs/QUICKSTART.md): local round-trip via HTTP
- **API reference (Swagger)** — browsable spec auto-generated from [`proto/anonde/v1/anonde.proto`](proto/anonde/v1/anonde.proto):
  - Hosted: https://anonde-io.github.io/anonde/ (redeployed on every `main` push that touches the proto or generated spec; needs Pages enabled in repo settings → source `GitHub Actions`)
  - Local: open [`docs/api/index.html`](docs/api/) after `make proto` (serve over HTTP, e.g. `python3 -m http.server`)
  - Source JSON: [`gen/anonde/v1/anonde.swagger.json`](gen/anonde/v1/anonde.swagger.json)
- [Developer guide](docs/DEVELOPER_GUIDE.md): text + PDF + scanned-image flows, per-request PDF knobs, Prometheus metrics
- [Recognizers](docs/RECOGNIZERS.md): 52-recognizer table and writing custom recognizers
- [Architecture](docs/ARCHITECTURE.md): pipeline, directory tree, conflict resolution
- [Operators](docs/OPERATORS.md): Replace, Redact, Mask, Hash, Encrypt, Synthesize
- [Deployment](docs/DEPLOYMENT.md): Docker, env vars, CI
- [Benchmark matrix](bench/REPORT_MATRIX.md): full results

## Telemetry

anonde sends an anonymous heartbeat once every 24 hours so we can see
which deployment shapes (patterns vs NER, OS / arch, backend mix) and
which entity types are in active use, and prioritise the roadmap
against real signal rather than guesswork.

Disable with `ANONDE_TELEMETRY=off` or `ANONDE_OFFLINE=1`.

A random install ID is persisted at `$XDG_DATA_HOME/anonde/install_id`
(fallback `~/.anonde/install_id`) and reused across restarts.

Fields sent:

| Field | Example |
|---|---|
| `install_id` | `8a17c…b3` |
| `version` | `f684298` |
| `build_tag` | `default` or `hugot` |
| `os` / `arch` | `linux` / `amd64` |
| `backend` | `patterns`, `gliner`, … |
| `uptime_seconds` | `86400` |
| `request_count` | `1245` |
| `error_count` | `3` |
| `entity_counts` | `{"PERSON": 412, "EMAIL_ADDRESS": 89}` |
| `p95_latency_ms` | `42.1` |

No input text, output text, token values, vault contents, IP
addresses, hostnames, tenant IDs, document IDs, actors, or purposes
are sent. The wire payload is defined in
[`internal/telemetry/payload.go`](internal/telemetry/payload.go) and
enforced by a unit test.

Override the endpoint with `ANONDE_TELEMETRY_URL`.

## Contributing & community

- **Issues:** bug reports, feature requests, and questions all welcome.
  The repo has [issue templates](.github/ISSUE_TEMPLATE/) for each.
- **Pull requests:** start with [CONTRIBUTING.md](CONTRIBUTING.md) for
  dev setup, recognizer-adding patterns, and bench expectations. No DCO
  or CLA; Apache 2.0 §5 grants the inbound license automatically.
- **Code of conduct:** [Contributor Covenant 2.1](CODE_OF_CONDUCT.md).
  Conduct concerns go to `conduct@anonde.io`.
- **Security:** vulnerabilities go through the private channels in
  [SECURITY.md](SECURITY.md), not public issues.

## License

[Apache 2.0](LICENSE). See [`NOTICE`](NOTICE) for the attribution
notice that downstream redistributors are asked to preserve.
