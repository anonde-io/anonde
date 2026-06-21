# anonde server: local quickstart

Run the HTTP API locally and round-trip a document through
**anonymize → reveal**. Works with plain text and JSON. The server
auto-detects language (German vs English), so the caller never has to
specify it.

API is Stripe-style: `/v1/anonymizations` for create, then
`/v1/anonymizations/{id}/{verb}` for follow-ups. One port serves REST
(grpc-gateway), Connect, and native gRPC.

For PDFs / scanned images and Prometheus metrics, see
[`DEVELOPER_GUIDE.md`](DEVELOPER_GUIDE.md).

## 1. Start the server

Fastest path — Docker, one command, no Go toolchain. The patterns-only
image is ~41 MB and cold-starts in <0.3s:

```bash
docker run --rm -p 8081:8080 ghcr.io/anonde-io/anonde:latest
```

Every curl below targets `:8081`. For the GLiNER NER pass, swap in
`ghcr.io/anonde-io/anonde-ner:latest` (2.66 GB, ~2 min pull); see the
[Docker image variants](#docker-image-variants) table.

Prefer running from source? Use the Makefile instead (needs the Go
toolchain):

```bash
make run            # ANALYZER_BACKEND=patterns ANONDE_ADDR=:8081 go run ./cmd/anonde/
```

- `ANALYZER_BACKEND=patterns`: pattern recognizers only, no NER model
  download. Fastest start. Swap to `make run-ner` for the GLiNER pass
  (requires `-tags ner` + CGO), or `make run-ner-pdf` for the full
  developer suite (NER + PDF endpoint + Prometheus on `:9090`).
- `ANONDE_ADDR=:8081`: HTTP listen address. Defaults to `:8080`.

Health check:

```bash
curl -s http://localhost:8081/v1/health
# {"status":"SERVING"}
```

## 2. Anonymize text

```bash
curl -s -X POST http://localhost:8081/v1/anonymizations \
  -H "Content-Type: application/json" \
  -d '{
    "tenant_id":      "demo",
    "id":             "letter-001",
    "content_format": "text",
    "content":        "Patient: Anna Schmidt, geb. 15.03.1985\nKlinik: Universitätsklinikum Heidelberg\nE-Mail: anna.schmidt@klinik.de"
  }'
```

Response (abbreviated):

```json
{
  "tenant_id": "demo",
  "id": "letter-001",
  "anonymized_content":
    "Patient: <PERSON_DEMO_000001>, geb. <DATE_TIME_DEMO_000002>\nKlinik: <ORGANIZATION_DEMO_000003>\nE-Mail: <EMAIL_ADDRESS_DEMO_000004>",
  "detected_entity_size": 4,
  "findings": [ /* per-span: start, end, score, entity_type, recognizer_name */ ],
  "tokens":   [ /* {token, entity_type, start, end} */ ]
}
```

The cleartext is stored in an in-memory vault keyed by token. Save the
`anonymized_content` and ship it to your downstream system; your tenant
keeps the originals.

Notes:
- No `language` field needed; it's auto-detected from the content. Add
  `"options": {"language": "de"}` (or `"en"`) to override.
- `tenant_id` is required. `id` is optional: omit it and the server
  mints `anon_<hex>` and returns it in the response.
- Tokens are namespaced by `(tenant_id, id)`.

## 3. Anonymize JSON

```bash
curl -s -X POST http://localhost:8081/v1/anonymizations \
  -H "Content-Type: application/json" \
  -d '{
    "tenant_id":      "demo",
    "id":             "record-002",
    "content_format": "json",
    "content":        "{\"patient\":\"Hans Müller\",\"email\":\"hans@klinik.de\",\"notes\":\"geboren am 22.04.1970 in München\"}"
  }'
```

JSON ingest walks every string leaf, finds entities per-leaf, and
substitutes tokens in place. The shape of the JSON is preserved
verbatim; only string values change.

Response `anonymized_content`:

```json
{"patient":"<PERSON_DEMO_000005>","email":"<EMAIL_ADDRESS_DEMO_000006>","notes":"geboren am <DATE_TIME_DEMO_000007> in <LOCATION_DEMO_000008>"}
```

## 4. Detokenize (lookup tokens → cleartext)

You hold a few tokens from the response and want the originals.
Detokenize requires `actor` + `purpose` for audit. The default policy in
`cmd/anonde/main.go` (`StaticPolicy`) allows everything; in production
you'd swap in your own `PolicyAuthorizer`.

```bash
curl -s -X POST http://localhost:8081/v1/anonymizations/letter-001/detokenize \
  -H "Content-Type: application/json" \
  -d '{
    "tenant_id": "demo",
    "actor":     "billing-team",
    "purpose":   "invoice-generation",
    "tokens":    ["<PERSON_DEMO_000001>", "<EMAIL_ADDRESS_DEMO_000004>"]
  }'
```

Response:

```json
{
  "tenant_id": "demo",
  "id": "letter-001",
  "resolved": {
    "<PERSON_DEMO_000001>": "Anna Schmidt",
    "<EMAIL_ADDRESS_DEMO_000004>": "anna.schmidt@klinik.de"
  }
}
```

Tokens not linked to the id you reference are rejected, even if they
exist for another id under the same tenant.

## 5. Reveal (substitute tokens back into content)

You have the anonymized output and want the original document. Reveal
auto-walks the content, replaces tokens with cleartext, and respects
the original format.

### Text

```bash
curl -s -X POST http://localhost:8081/v1/anonymizations/letter-001/reveal \
  -H "Content-Type: application/json" \
  -d '{
    "tenant_id":      "demo",
    "actor":          "doctor",
    "purpose":        "clinical-review",
    "content_format": "text",
    "content":        "Patient: <PERSON_DEMO_000001>, geb. <DATE_TIME_DEMO_000002>\nKlinik: <ORGANIZATION_DEMO_000003>\nE-Mail: <EMAIL_ADDRESS_DEMO_000004>"
  }'
```

Returns the original German text with every token replaced under
`deanonymized_content`.

### JSON

```bash
curl -s -X POST http://localhost:8081/v1/anonymizations/record-002/reveal \
  -H "Content-Type: application/json" \
  -d '{
    "tenant_id":      "demo",
    "actor":          "doctor",
    "purpose":        "clinical-review",
    "content_format": "json",
    "content":        "{\"patient\":\"<PERSON_DEMO_000005>\",\"email\":\"<EMAIL_ADDRESS_DEMO_000006>\",\"notes\":\"geboren am <DATE_TIME_DEMO_000007> in <LOCATION_DEMO_000008>\"}"
  }'
```

JSON reveal walks the same way ingest did: string leaves only. Object
keys, numbers, booleans, structure all stay untouched.

## 6. Delete

Idempotent. Wipes vault entries + audit lineage for `(tenant_id, id)`.

```bash
curl -s -X DELETE 'http://localhost:8081/v1/anonymizations/letter-001?tenantId=demo'
# {"tokens_deleted":4,"deleted":true}
```

## End-to-end round-trip

Text round-trip is byte-exact:

```bash
ORIGINAL='Patient: Hans Müller, Klinik: Universitätsklinikum Heidelberg.'

ANON=$(curl -s -X POST http://localhost:8081/v1/anonymizations \
  -H "Content-Type: application/json" \
  -d "{\"tenant_id\":\"demo\",\"id\":\"rt-text\",\"content_format\":\"text\",\"content\":$(jq -Rs . <<<"$ORIGINAL")}" \
  | jq -r '.anonymized_content')

echo "anon:     $ANON"

RESTORED=$(curl -s -X POST http://localhost:8081/v1/anonymizations/rt-text/reveal \
  -H "Content-Type: application/json" \
  -d "{\"tenant_id\":\"demo\",\"actor\":\"tester\",\"purpose\":\"verify\",\"content_format\":\"text\",\"content\":$(jq -Rs . <<<"$ANON")}" \
  | jq -r '.deanonymized_content')

echo "restored: $RESTORED"
[ "$RESTORED" = "$ORIGINAL" ] && echo "match: yes" || echo "match: no"
```

JSON round-trip recovers every cleartext value exactly but may **reorder
object keys** and re-emit whitespace, since Go's `encoding/json` marshals
keys alphabetically. Compare semantically (e.g. `jq -S . a == jq -S . b`)
rather than byte-for-byte. The PHI guarantees still hold: every entity
in every string leaf is detected on ingest and restored on reveal.

## Other content formats

`content_format` accepts `text`, `json`, `ndjson`, `logs`, `pdf`, and
`auto`. NDJSON treats each line as a separate JSON document, logs are
mixed text/JSON (with ANSI stripped), and PDF has its own endpoint.
See [`DEVELOPER_GUIDE.md`](DEVELOPER_GUIDE.md) for the PDF / scanned-image
flow.

## Docker image variants

Three Dockerfiles ship with the repo, pick per workload:

| Image | Built via | Size (on-disk) | Backend | Use when |
|---|---|---|---|---|
| `anonde-smoke:patterns` | `make docker-build` | ~41 MB | patterns-only | German clinical text, structured English fields, no narrative names |
| `anonde-smoke:ner` | `make docker-build-ner` | ~2.66 GB | GLiNER base + patterns + OCR + YOLOS sig | production default; Σ ALL ≈ 12.9% leak across 30 corpora; PDF endpoint enabled |
| `anonde-smoke:ner-stack` | `make docker-build-ner-stack` | largest (base + LARGE model) | GLiNER base + LARGE + patterns + YOLOS sig | lowest-leak tier (Σ ALL ≈ 8.4%), ~2× inference latency |

**Which `:latest` is which.** The published tags map to these tiers:

- `ghcr.io/anonde-io/anonde:latest` — patterns image, **~41 MB**.
- `ghcr.io/anonde-io/anonde-ner:latest` — the **base `ner` tier** (GLiNER
  base, **2.66 GB**). This is what the README quickstart and the site hero
  pull. It is *not* the stack tier.
- `ghcr.io/anonde-io/anonde-ner-stack:latest` — the lowest-leak stack tier
  (base + LARGE). Opt in explicitly when you need the 8.4% leak rate.

Sizes were last verified 2026-06-21 on arm64 by re-pulling each tag.

`make docker-run` and `make docker-run-ner` build the image (if needed)
and start the container. The NER container exposes
`/v1/anonymizations/pdf` (which accepts all the legacy CLI knobs as
URL query parameters; see [`DEVELOPER_GUIDE.md`](DEVELOPER_GUIDE.md))
and Prometheus on `:9090`.

The NER image runs offline once built; the ONNX model is baked into
`/models` during build. No HuggingFace Hub calls at runtime.

First request after a cold NER container is slower (~3 s on the base
`ner` image, ~6 s on `ner-stack`) because the ONNX session loads into
memory on first inference. Warm calls then run ~1.5 s (base) / ~3 s
(stack) each.

In-memory vault and store are cleared on each restart, so a token
issued under one variant cannot be revealed after switching to the
other.

## Notes

- All ingest data lives in memory. Retention is configurable via the
  `ANONDE_VAULT_TTL` and `ANONDE_STORE_TTL` env vars, both default `0` —
  no auto-expiry, so entries persist until you `DELETE` them or the
  process restarts. Set e.g. `ANONDE_VAULT_TTL=30m` to bound retention.
  The anonde server is designed for a tenant to plug in their own vault
  + store.
- The same cleartext within a single doc gets the same token. The same
  cleartext across docs does NOT, because `mintToken` is per-(tenant, entity)
  with a monotonic counter, not content-addressable.
- Body size cap: `MAX_CONTENT_BYTES` env var, default 10 MiB.
