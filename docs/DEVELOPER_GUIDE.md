# Developer guide: run anonde locally

End-to-end loop: anonymize → deanonymize text, PDFs, scanned images, plus
Prometheus metrics. All endpoints listen on a single port (default `:8081`).

## 1. Run the full suite

Two ways: native Go (fastest dev loop) or Docker (closest to prod).

### Native (patterns + NER)

```bash
# patterns-only (fast, no model download)
make run                # ANALYZER_BACKEND=patterns on :8081

# NER (GLiNER, requires -tags hugot + CGO + libonnxruntime)
make run-ner            # ANALYZER_BACKEND=gliner on :8081

# NER + PDF endpoint + Prometheus on 127.0.0.1:9090
make run-ner-pdf        # host needs pdftoppm (poppler) and tesseract on PATH
```

`make run-ner-pdf` is the full developer suite: text/JSON/PDF anonymize,
reveal, and `/metrics`. The CLI counterpart for one-off PDFs without a
server is `make build-pdf-cli`, which drops a binary at
`./bin/anonymize-pdf`.

### Docker (one container, everything wired)

```bash
make docker-build       # patterns image (anonde-smoke:patterns, ~12 MB, text/JSON only)
make docker-run         # patterns container on :8081

make docker-build-ner   # NER image (anonde-smoke:ner, ~770 MB, GLiNER + OCR baked in)
make docker-run-ner     # NER container on :8081, PDF endpoint + metrics on :9090

make smoke              # round-trips ingest, reveal, delete against :8081
```

For one-shot PDFs without running a server, build the dedicated CLI image:

```bash
make docker-build-pdf-cli
docker run --rm -v "$PWD:/data" -v anonde-models:/root/.cache/anonde \
  anonymize-pdf /data/in.pdf /data/out.pdf
```

The lowest-leak tier (`Dockerfile.anonde-ner-stack`, base + LARGE GLiNER,
~2.1 GB, ~2× inference latency) builds via `make docker-build-ner-stack`.

## 2. Text: anonymize / deanonymize

Current API is Stripe-style: `/v1/anonymizations/{id}/{verb}`.

```bash
# Anonymize
curl -s -X POST http://localhost:8081/v1/anonymizations \
  -H 'Content-Type: application/json' \
  -d '{
    "tenant_id": "demo",
    "id":        "doc-1",
    "content":   "Patient Hans Müller, geb. 14.03.1962, Berlin"
  }'
# → {"anonymized_content":"Patient <PERSON_DEMO_000001>, geb. <DATE_TIME_DEMO_000002>, <LOCATION_DEMO_000003>", ...}

# Reveal (substitute tokens back into the anonymized blob)
curl -s -X POST http://localhost:8081/v1/anonymizations/doc-1/reveal \
  -H 'Content-Type: application/json' \
  -d '{
    "tenant_id": "demo",
    "actor":     "you",
    "purpose":   "demo",
    "content":   "Patient <PERSON_DEMO_000001>, geb. <DATE_TIME_DEMO_000002>"
  }'

# Detokenize (token list → cleartext map)
curl -s -X POST http://localhost:8081/v1/anonymizations/doc-1/detokenize \
  -H 'Content-Type: application/json' \
  -d '{"tenant_id":"demo","tokens":["<PERSON_DEMO_000001>"]}'

# Delete (wipes vault + lineage for this id)
curl -s -X DELETE 'http://localhost:8081/v1/anonymizations/doc-1?tenantId=demo'
```

`content_format` accepts `text`, `json`, `ndjson`, `logs`, `pdf`, `auto`. JSON
walks string leaves and preserves shape.

## 3. PDF: anonymize / deanonymize

```bash
# Anonymize a PDF (returns redacted PDF; original is stashed under an auto-minted id)
curl -s -X POST http://localhost:8081/v1/anonymizations/pdf \
  -H 'Content-Type: application/pdf' \
  -H 'X-Anonde-Tenant: demo' \
  --data-binary @in.pdf \
  -D /tmp/headers.txt \
  -o out.pdf
ID=$(grep -i '^X-Anonde-Id:' /tmp/headers.txt | awk '{print $2}' | tr -d '\r')

# Reveal: fetch the original bytes back
curl -s -H 'X-Anonde-Tenant: demo' \
  "http://localhost:8081/v1/anonymizations/$ID/reveal-pdf" -o original.pdf
```

Both endpoints take the tenant via the `X-Anonde-Tenant` header
(preferred, since it survives proxies that strip query strings from
logs) or the `?tenant=<id>` query param. The POST response also echoes
`X-Anonde-Tenant`, `X-Anonde-Entities` (total redacted span count),
`X-Anonde-Entity-Types` (distinct types), and one
`X-Anonde-Entity-Count: TYPE=N` header per detected entity type, so
you can log counts without a second request.

The redactor rasterizes each page (200 DPI), runs OCR + GLiNER, then
draws black boxes over PII word boxes on the page images. Output is a
flattened image-PDF, so text-layer extraction won't recover the redacted
content.

CLI equivalent (one-shot, no server):

```bash
go run -tags hugot ./cmd/anonymize-pdf in.pdf out.pdf
# flags: -mode visual|text, -backend gliner|patterns, -langs eng+deu,
#        -score-threshold 0.3, -signature-model, -dpi 200
```

## 4. Scanned images (PNG / JPG)

There's no raw-image endpoint. Wrap the image in a single-page PDF first,
then use the PDF flow above:

```bash
# ImageMagick
magick scan.png scan.pdf
# or img2pdf (preserves resolution without recompression)
img2pdf scan.png -o scan.pdf

curl -s -X POST http://localhost:8081/v1/anonymizations/pdf \
  -H 'Content-Type: application/pdf' --data-binary @scan.pdf -o scan-redacted.pdf
```

OCR languages are controlled by `ANONDE_OCR_LANGS` (default
`eng+deu+fra+spa+ita+ron` in the Docker images, `eng+deu` locally; install
the matching `tesseract-ocr-<lang>` packages on the host).

## 5. Metrics

Prometheus is on by default (`METRICS_ENABLED=true`) but **only exposed when
you set `METRICS_BIND`**, so it stays off the public port on purpose.
`make run-ner-pdf` and `make docker-run-ner` both bind `:9090`.

```bash
make run-ner-pdf                                       # or: make docker-run-ner
curl -s http://127.0.0.1:9090/metrics | grep ^anonde_
```

Series you'll actually want:

| Metric | Type | What it tells you |
|---|---|---|
| `anonde_requests_total{route,status}` | counter | Per-route call rate + error breakdown |
| `anonde_request_duration_seconds` | histogram | End-to-end latency (p50/p95) |
| `anonde_analyze_duration_seconds{backend}` | histogram | Time spent in the detector only |
| `anonde_entities_detected_total{entity_type,recognizer}` | counter | What's being matched, by recognizer |
| `anonde_conflicts_resolved_total{winner,loser}` | counter | NER-vs-pattern conflict wins |
| `anonde_bytes_processed_total{route}` | counter | Throughput in bytes |
| `anonde_vault_ops_total{op,result}` | counter | Vault hits/misses, deletes |
| `anonde_policy_denials_total{reason}` | counter | Authz rejections (today: always 0) |
| `anonde_text_length_bytes` / `anonde_entity_score` | histograms | Input-size + score distributions |
| `anonde_vault_entries` / `anonde_store_entries` (+ `_bytes`) | gauges | In-memory footprint, scraped live |

Quick smoke chain (needs metrics, so use the NER container):

```bash
make docker-run-ner
curl -s localhost:8081/v1/health
curl -s -X POST localhost:8081/v1/anonymizations \
  -H 'Content-Type: application/json' \
  -d '{"tenant_id":"t","id":"d1","content":"Hans Müller, Berlin"}' | jq
curl -s localhost:9090/metrics | grep -E 'anonde_(requests|entities)_total' | head
```
