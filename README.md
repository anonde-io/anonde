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
  <img src="https://img.shields.io/badge/image-41MB%20(patterns)%20%7C%202.66GB%20(NER)-2496ED.svg" alt="Image size">
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
- **Wins on leak rate.** anonde-ner has the lowest leak rate on all 29 gold-annotated corpora the bench tracks across English, German, Spanish, French and Italian — covering clinical, legal, finance, structured PII, and adversarial / out-of-distribution text. ([full results](bench/REPORT_MATRIX.md))
- **Local-first.** Ships as a Go library or a Docker image you run yourself. No cloud calls. NER models are baked into the image, so there is no outbound HuggingFace traffic at request time.
- **Multilingual.** Open-set NER (GLiNER) plus 70 region-aware pattern recognizers covering international IDs and a dozen-plus national jurisdictions.
- **Reversible, audited.** Tokens map back to cleartext only where you allow it. The reveal call requires `actor` + `purpose` and is the only place plaintext comes back.
- **Recall-biased.** Missing a span is a leak; tokenising one too many is cheap. The bench tracks this explicitly via `leak_rate` (lower is better).

## Quick start

Two ways to run anonde — pick the one that matches how you ship.

### Docker (HTTP server, fastest)

One command, no Go toolchain needed. The patterns-only image is ~41 MB
and cold-starts in <0.3s; the NER image (2.66 GB) bakes in GLiNER base +
libonnxruntime for PERSON / ORG / LOC detection (~2 min pull, ready ~1s,
first inference ~3s while the ONNX session loads, then ~1.5s warm).

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

Image variants, port/listen-address overrides, persistent volumes, and
docker compose profiles are all in [docs/DEPLOYMENT.md](docs/DEPLOYMENT.md).

#### GLiNER label sets (`GLINER_LABEL_SET`)

The NER image runs open-set GLiNER, so the list of entity labels is
supplied at inference time — anonde ships four curated label sets and
selects one with the `GLINER_LABEL_SET` env var. `chat` is the default;
the others are opt-in.

| Set | Tuned for | Highlights |
| --- | --- | --- |
| `chat` *(default)* | Casual / conversational traffic | Names, org, email/phone/URL, postal geography, structured financial + government IDs. **Drops** `age`, `profession`, `job title`, `date` / `date of birth`, and the clinical / German-insurance labels — they over-redact ordinary chat ("18 years of experience" → AGE, "tech" → PROFESSION). |
| `clinical` | Clinical / HIPAA de-identification | The full default set: everything in chat **plus** `age`, `profession`, `date` / `date of birth`, patient/doctor/hospital labels, and the German insurance / tax / case-file IDs (`Versicherungsnummer`, `Steuer-Identifikationsnummer`, `Aktenzeichen`, …). |
| `finance` | Bank statements, KYC, payments, tax forms | Identity + contact core **plus** bank account / routing numbers, IBAN, SWIFT/BIC, credit-card number + CVV, tax IDs (SSN / ITIN / EIN / Steuer-ID), and account / transaction identifiers. |
| `legal` | Pleadings, contracts, court filings, matter files | Identity + geography core, **keeps** `date` / `date of birth` (legal docs are date-sensitive, unlike chat), **plus** case / docket / matter / contract / bar numbers, court name, and party roles (attorney, plaintiff, defendant, judge). |

```bash
# Default (chat) — no env needed
docker run --rm -p 8081:8080 ghcr.io/anonde-io/anonde-ner:latest

# Clinical / HIPAA coverage (adds AGE / DATE + clinical labels)
docker run --rm -p 8081:8080 -e GLINER_LABEL_SET=clinical ghcr.io/anonde-io/anonde-ner:latest

# Finance (bank / IBAN / SWIFT / card+CVV / tax IDs)
docker run --rm -p 8081:8080 -e GLINER_LABEL_SET=finance ghcr.io/anonde-io/anonde-ner:latest

# Legal (case / docket / bar numbers + dates kept)
docker run --rm -p 8081:8080 -e GLINER_LABEL_SET=legal ghcr.io/anonde-io/anonde-ner:latest
```

All four sets map onto the same canonical entity types the pattern
recognizers emit (`PERSON`, `ORGANIZATION`, `IBAN_CODE`, `US_BANK_NUMBER`,
`ID`, …), so anonymizer operators and reveal/detokenize behave identically
regardless of which set is active. An unrecognised value falls back to
`chat`. Go-library callers set `GLiNERConfig.Labels` / `LabelToEntity`
directly (e.g. `recognizers.FinancePIILabels`).

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

Default build is pure Go, no CGO. The `-tags ner` build enables in-process GLiNER NER; see [docs/DEPLOYMENT.md](docs/DEPLOYMENT.md).

## HTTP API

The same server speaks three transports on one port:

- **REST/JSON** via grpc-gateway: `POST /v1/anonymizations`, `POST /v1/anonymizations/{id}/reveal|detokenize`, `DELETE /v1/anonymizations/{id}?tenant_id={tenant_id}`, `POST /v1/synthesize`, `GET /v1/version`. `id` is optional on create (server mints `anon_<hex>` if omitted); tenant lives in the request body / query for now and moves to a bearer-token header when auth lands. JSON fields are snake_case on the wire (`tenant_id`, `content_format`, `anonymized_content`, …); inputs also accept the camelCase form so generated gRPC clients work without translation.
- **Connect** (Connect/JSON, Connect/Protobuf, gRPC-Web): `POST /anonde.v1.Service/<Method>`.
- **Native gRPC** over HTTP/2 cleartext: same `/anonde.v1.Service/<Method>` path.

Two optional surfaces ride alongside: **PDF redaction** (`POST /v1/anonymizations/pdf`, see [PDFs & scans](#pdfs--scans)) and an **OpenAI-compatible proxy** (`POST /v1/chat/completions`, see [OpenAI proxy](#use-anonde-as-an-openai-proxy)).

Source of truth: [`proto/anonde/v1/anonde.proto`](proto/anonde/v1/anonde.proto). Regenerate handlers with `buf generate`. Full round-trip examples (text, JSON, PDF) live in [docs/QUICKSTART.md](docs/QUICKSTART.md).

## PDFs & scans

PDFs are a first-class HTTP endpoint, not a separate binary. Two surfaces:

1. **Text PDFs through the normal endpoint** — send a base64 PDF with
   `content_format: "pdf"` to `POST /v1/anonymizations`; the text layer is
   extracted and run through the same analyzer pipeline as text input.
2. **Raw PDF in, redacted PDF out** via `POST /v1/anonymizations/pdf` —
   returns a PDF with black boxes over each PII span; reversible via
   `GET /v1/anonymizations/{id}/reveal-pdf`. Opt-in with `ANONDE_PDF_ENABLED=1`.

When the text layer is empty or too short (an image-only scan or
photo-to-PDF), both surfaces transparently rasterise each page and OCR it
before running the analyzer — no caller change. The `anonde-ner` /
`anonde-ner-stack` images bundle `poppler-utils` + `tesseract-ocr`
(`eng+deu+fra+spa+ita+ron`), so OCR and the YOLOS signature redactor are on
by default there; the patterns-only image stays ~41 MB and skips them.

Per-request knobs (mode, operator, entities, score-threshold, ocr-langs, …)
bind from URL query params. Full flows, the field table, and OCR env vars are
in [docs/DEVELOPER_GUIDE.md](docs/DEVELOPER_GUIDE.md) and
[docs/DEPLOYMENT.md](docs/DEPLOYMENT.md#pdf--ocr).

## Use anonde as an OpenAI proxy

The lowest-friction integration: point your existing OpenAI SDK at anonde
instead of `api.openai.com`. anonde anonymizes the prompt, forwards it to the
real provider, de-anonymizes the response, and hands it back in OpenAI shape.

```python
from openai import OpenAI

# Server started with ANONDE_OPENAI_BASE_URL + ANONDE_OPENAI_API_KEY set.
client = OpenAI(base_url="http://localhost:8081/v1", api_key="unused")
resp = client.chat.completions.create(
    model="openai/gpt-4o",
    messages=[{"role": "user", "content": "Email a summary to sarah.chen@acme.example"}],
)
# The provider only ever saw <EMAIL_ADDRESS_…>; the reply has the real address restored.
```

Works with the raw OpenAI SDK, LangChain, or anything that speaks the OpenAI
API. Provider is selected in-band by a `provider/model` prefix. Env vars,
multi-provider routing, and the v0.1 streaming limitation are in
[docs/OPENAI_PROXY.md](docs/OPENAI_PROXY.md).

## Write your own recognizer

Extensibility is the part Presidio gets right and we copied. A pattern
recognizer is a regex, a label, a language list, and optional context words
that boost the score when they appear nearby. Add one two ways:

- **In-repo** — drop a file into `analyzer/recognizers/` and register it in
  `anonde.go`; it joins every engine by default.
- **As a library consumer (no fork)** — implement
  [`analyzer.EntityRecognizer`](analyzer/recognizer.go) and
  `engine.Registry.Add(...)` at startup.

Either path joins the parallel-dispatch pipeline and the conflict resolver
handles overlap automatically. Worked examples (context boosts, both paths),
the 70-recognizer catalogue, and how to add a model-backed NER recognizer are
in [docs/RECOGNIZERS.md](docs/RECOGNIZERS.md). NER preferences and the full
pipeline rules are in [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md).

## Built for

- **Healthcare.** Chart summaries, discharge letters, clinical Q&A. Keep PHI off the wire to third-party models.
- **Finance.** KYC review, support triage, statement summarisation. Account numbers, card data, and PII stay inside your boundary.
- **Logs & telemetry.** Application logs, audit trails, SIEM exports, and traces often carry emails, IPs, account IDs, and free-text from users. Run them through anonde before they hit a remote LLM, a log aggregator, or a BI store.
- **Enterprise.** Internal copilots over support tickets, contracts, HR docs. Audit who reveals what, and why.

Want to see the full flow in the browser? [anonde.io](https://anonde.io).

## Docs

- [Quickstart](docs/QUICKSTART.md): local round-trip via HTTP
- **API reference (Swagger)** — browsable spec auto-generated from [`proto/anonde/v1/anonde.proto`](proto/anonde/v1/anonde.proto):
  - Hosted: https://anonde-io.github.io/anonde/ (redeployed on every `main` push that touches the proto or generated spec; needs Pages enabled in repo settings → source `GitHub Actions`)
  - Local: open [`docs/api/index.html`](docs/api/) after `make proto` (serve over HTTP, e.g. `python3 -m http.server`)
  - Source JSON: [`gen/anonde/v1/anonde.swagger.json`](gen/anonde/v1/anonde.swagger.json)
- [Developer guide](docs/DEVELOPER_GUIDE.md): text + PDF + scanned-image flows, per-request PDF knobs, Prometheus metrics
- [Recognizers](docs/RECOGNIZERS.md): 70-recognizer table and writing custom recognizers
- [Architecture](docs/ARCHITECTURE.md): pipeline, directory tree, conflict resolution
- [Operators](docs/OPERATORS.md): Replace, Redact, Mask, Hash, Encrypt, Synthesize
- [OpenAI proxy](docs/OPENAI_PROXY.md): point an OpenAI SDK at anonde
- [Deployment](docs/DEPLOYMENT.md): Docker, env vars, ports, persistence, PDF/OCR, CI
- [Telemetry](docs/TELEMETRY.md): what the anonymous heartbeat sends, and how to disable it
- [Benchmark matrix](bench/REPORT_MATRIX.md): full results

## Telemetry

anonde sends an anonymous heartbeat once every 24 hours (deployment shape,
backend mix, entity-type counts) so we can prioritise the roadmap against
real signal. **No input/output text, token values, vault contents, IPs,
hostnames, tenant/doc IDs, actors, or purposes are ever sent.** Disable with
`ANONDE_TELEMETRY=off` or `ANONDE_OFFLINE=1`. The full field list and the wire
payload are in [docs/TELEMETRY.md](docs/TELEMETRY.md).

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
