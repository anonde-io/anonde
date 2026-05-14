<p align="center">
  <img src="docs/assets/anonde-icon.jpg" alt="anonde" width="128" height="128">
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
ANALYZER_BACKEND=patterns PLATFORM_ADDR=:8081 go run ./cmd/anonde/
```

With Docker (patterns-only image, ~12 MB):

```bash
docker build -f Dockerfile.anonde -t anonde:patterns .
docker run --rm -p 8081:8080 anonde:patterns
```

The NER variant (GLiNER + libonnxruntime baked in, ~470 MB) builds the same way from `Dockerfile.anonde-ner`. See [docs/DEPLOYMENT.md](docs/DEPLOYMENT.md) for env vars, image internals, and Fly.io configs.

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

Source of truth: [`proto/anonde/v1/anonde.proto`](proto/anonde/v1/anonde.proto). Regenerate handlers with `buf generate`. Full round-trip examples (text, JSON, PDF) live in [docs/QUICKSTART.md](docs/QUICKSTART.md).

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

## Status

- **Live**: <https://anonde-platform.fly.dev> · [Demo UI](https://anonde.io/demo/)
- **CI**: `.github/workflows/bench.yml` runs the bench matrix on every relevant PR, with a guard rail that fails if the NER backend silently degrades to patterns-only.

## Docs

- [Quickstart](docs/QUICKSTART.md): local round-trip via HTTP
- [Recognizers](docs/RECOGNIZERS.md): 52-recognizer table and writing custom recognizers
- [Architecture](docs/ARCHITECTURE.md): pipeline, directory tree, conflict resolution
- [Operators](docs/OPERATORS.md): Replace, Redact, Mask, Hash, Encrypt, Synthesize
- [Deployment](docs/DEPLOYMENT.md): Docker, Fly.io, env vars, CI
- [Benchmark matrix](bench/REPORT_MATRIX.md): full results

## License

See [LICENSE](LICENSE).
