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
  <a href="https://anonde.io/demo/">Live demo</a> ·
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

**Your input** (text, JSON, NDJSON, logs, or PDF):

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
- **Wins on leak rate.** anonde-gliner has the lowest leak rate on 5 of 6 gold-annotated corpora the bench tracks, including German clinical, German finance, German legal, and English PII. ([numbers](#benchmarks))
- **Local-first.** Ships as a Go library or a Docker image you run yourself. No cloud calls. NER models are baked into the image, so there is no outbound HuggingFace traffic at request time.
- **Multilingual.** Open-set NER (GLiNER) plus 52 region-aware pattern recognizers covering 12+ jurisdictions: international IDs, US, UK, Germany, Italy, Spain, Australia, India, Poland, Singapore, Finland, Korea.
- **Reversible, audited.** Tokens map back to cleartext only where you allow it. The reveal call requires `actor` + `purpose` and is the only place plaintext comes back.
- **Recall-biased.** Missing a span is a leak; tokenising one too many is cheap. The bench tracks this explicitly via `leak_rate` (lower is better).

## Quick start: Go library

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

The NER variant (GLiNER + libonnxruntime baked in, ~770 MB) builds the same way from `Dockerfile.anonde-ner`. It ships the FP32 ONNX (`onnx/model.onnx`) by default — the matrix proved INT8 leaks ~6pp more PII overall. Memory-constrained deployments can opt back into INT8 with `GLINER_QUANT=int8` (saves ~240 MB image size at the cost of recall on multilingual legal / clinical text). See [docs/DEPLOYMENT.md](docs/DEPLOYMENT.md) for env vars and image internals.

Hit the running server:

```bash
curl -sS -X POST http://localhost:8081/v1/anonymizations \
  -H "Content-Type: application/json" \
  -d '{"tenant_id":"demo","content":"Hi, this is Sarah Chen (sarah.chen@acme.example)."}'
# → { "id": "anon_8f3c…", "anonymized_content": "...", "tokens": [...] }
```

## HTTP API

The same server speaks three transports on one port:

- **REST/JSON** via grpc-gateway: `POST /v1/anonymizations`, `POST /v1/anonymizations/{id}/reveal|detokenize`, `DELETE /v1/anonymizations/{id}?tenant_id={tenant_id}`, `POST /v1/synthesize`, `GET /v1/version`. `id` is optional on create (server mints `anon_<hex>` if omitted); tenant lives in the request body / query for now and moves to a bearer-token header when auth lands. JSON fields are snake_case on the wire (`tenant_id`, `content_format`, `anonymized_content`, …); inputs also accept the camelCase form so generated gRPC clients work without translation.
- **Connect** (Connect/JSON, Connect/Protobuf, gRPC-Web): `POST /anonde.v1.Service/<Method>`.
- **Native gRPC** over HTTP/2 cleartext: same `/anonde.v1.Service/<Method>` path.

Plus an **OpenAI-compatible proxy** at `POST /v1/chat/completions` — see [Use anonde as an OpenAI proxy](#use-anonde-as-an-openai-proxy) below.

Source of truth: [`proto/anonde/v1/anonde.proto`](proto/anonde/v1/anonde.proto). Regenerate handlers with `buf generate`. Full round-trip examples (text, JSON, PDF) live in [docs/QUICKSTART.md](docs/QUICKSTART.md).

## Use anonde as an OpenAI proxy

The lowest-friction integration: point your existing OpenAI SDK at
anonde instead of `api.openai.com`. anonde anonymizes the prompt,
forwards it to the real provider, de-anonymizes the response, and hands
it back in OpenAI shape. No plugin, no code change beyond the base URL —
works with the raw OpenAI SDK, LangChain, or anything that speaks the
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
    model="openai/gpt-4o",   # provider/model — "openai/" is optional
    messages=[{"role": "user",
               "content": "Email a summary to sarah.chen@acme.example"}],
)
# The provider only ever saw <EMAIL_ADDRESS_…>; the reply you get back
# has the real address restored.
```

The endpoint is a single OpenAI-shaped `POST /v1/chat/completions`, so
the client base URL is byte-identical to a real OpenAI swap. The
upstream provider is selected in-band — the OpenRouter convention — by a
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
is rejected with a clear error rather than silently downgraded —
streaming SSE de-anonymization lands in v0.1.1. Anthropic and Gemini
upstreams (selected by the `anthropic/` / `gemini/` model prefix) are on
the roadmap.

## Write your own recognizer

Extensibility is the part Presidio gets right and we copied. A pattern
recognizer is a regex, a label, a language list, and an optional set of
context words that boost the score when they appear nearby.

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

Register it in `anonde.go::patternRecognizers()` and it joins the
parallel-dispatch pipeline. The conflict resolver handles overlap with
existing recognizers automatically; NER preferences (PERSON / ORG / LOC
/ AGE / PROFESSION / NRP) and the full pipeline rules live in
[docs/ARCHITECTURE.md](docs/ARCHITECTURE.md). The full 52-recognizer
catalogue + how to add a model-backed (NER) recognizer is in
[docs/RECOGNIZERS.md](docs/RECOGNIZERS.md).

## Built for

- **Healthcare.** Chart summaries, discharge letters, clinical Q&A. Keep PHI off the wire to third-party models.
- **Finance.** KYC review, support triage, statement summarisation. Account numbers, card data, and PII stay inside your boundary.
- **Logs & telemetry.** Application logs, audit trails, SIEM exports, and traces often carry emails, IPs, account IDs, and free-text from users. Run them through anonde before they hit a remote LLM, a log aggregator, or a BI store.
- **Enterprise.** Internal copilots over support tickets, contracts, HR docs. Audit who reveals what, and why.

Want to see the full flow in the browser? [anonde.io/demo](https://anonde.io/demo/).

## Benchmarks

Public bench matrix, re-run on every PR. Each row is a gold-annotated corpus; **lower leak rate is better**.

| Corpus | Best non-anonde baseline | anonde-gliner | Δ |
|---|---|---:|---:|
| `openmed` (German clinical) | gliner-py 50.0% | **17.2%** | ↓ 32.8 pp |
| `synth_clinical` (German) | gliner-py 25.4% | **11.1%** | ↓ 14.3 pp |
| `ai4privacy_en` (English PII) | gliner-py 25.8% · Presidio 28.4% | **25.0%** | ↓ 0.8 pp |
| `finance_de` (German finance) | gliner-py 26.2% | **9.8%** | ↓ 16.4 pp |
| `legal_de` (German legal) | gliner-py 25.4% | **6.9%** | ↓ 18.5 pp |
| `wikiann_de` (German Wikipedia) | gliner-py **12.8%** | 15.3% | ↑ 2.5 pp |

anonde-gliner has the lowest leak rate on 5 of 6 corpora. On `wikiann_de` it's 2.5 pp behind the Python `gliner-py` sidecar (same model, different runtime; included as a parity check).

**Vs external baselines** on the corpora where they were benched:

- **Microsoft Presidio** on `ai4privacy_en` (English PII): anonde-gliner **25.0%** vs Presidio 28.4%.
- **OpenAI Privacy Filter** on `openmed` (German clinical): anonde-gliner **17.2%** vs OpenAI PF 98.4%.

Presidio and OpenAI Privacy Filter weren't run on every corpus: Presidio's bench harness uses its English pipeline only, and OpenAI Privacy Filter is ~80 s/doc on CPU, which makes it impractical on the larger corpora. Both engines can technically run on more languages; the bench numbers reflect what's been measured, not capability ceilings. Full grid (strict / partial / type-agnostic F1, all corpora, all engines) lives in [bench/REPORT_MATRIX.md](bench/REPORT_MATRIX.md).

## Docs

- [Quickstart](docs/QUICKSTART.md): local round-trip via HTTP
- [Recognizers](docs/RECOGNIZERS.md): 52-recognizer table and writing custom recognizers
- [Architecture](docs/ARCHITECTURE.md): pipeline, directory tree, conflict resolution
- [Operators](docs/OPERATORS.md): Replace, Redact, Mask, Hash, Encrypt, Synthesize
- [Deployment](docs/DEPLOYMENT.md): Docker, env vars, CI
- [Benchmark matrix](bench/REPORT_MATRIX.md): full results

## Contributing & community

- **Issues** — bug reports, feature requests, and questions all welcome.
  The repo has [issue templates](.github/ISSUE_TEMPLATE/) for each.
- **Pull requests** — start with [CONTRIBUTING.md](CONTRIBUTING.md) for
  dev setup, recognizer-adding patterns, and bench expectations. No DCO
  or CLA — Apache 2.0 §5 grants the inbound license automatically.
- **Code of conduct** — [Contributor Covenant 2.1](CODE_OF_CONDUCT.md).
  Conduct concerns go to `conduct@anonde.io`.
- **Security** — vulnerabilities go through the private channels in
  [SECURITY.md](SECURITY.md), not public issues.

## License

[Apache 2.0](LICENSE). See [`NOTICE`](NOTICE) for the attribution
notice that downstream redistributors are asked to preserve.
