# anonde server — local quickstart

Run the HTTP API locally and round-trip a document through
**ingest → detokenize → reveal**. Works with plain text and JSON. The
server auto-detects language (German vs English), so the caller never
has to specify it.

## 1. Start the server

```bash
ANALYZER_BACKEND=patterns ANONDE_ADDR=:8081 go run ./cmd/anonde/
```

- `ANALYZER_BACKEND=patterns` — pattern recognizers only, no NER model
  download. Fastest start and exercises the German recognizer kernel
  end-to-end. Use `hugot` (default) for a stronger NER pass — first run
  downloads ~250 MB.
- `ANONDE_ADDR=:8081` — HTTP listen address. Defaults to `:8080`.

Health check:

```bash
curl -s http://localhost:8081/healthz
# {"status":"ok"}
```

## 2. Ingest text

```bash
curl -s -X POST http://localhost:8081/v1/ingest \
  -H "Content-Type: application/json" \
  -d '{
    "tenant_id": "demo",
    "doc_id":    "letter-001",
    "content_format": "text",
    "content": "Patient: Anna Schmidt, geb. 15.03.1985\nKlinik: Universitätsklinikum Heidelberg\nE-Mail: anna.schmidt@klinik.de"
  }'
```

Response (abbreviated):

```json
{
  "tenant_id": "demo",
  "doc_id": "letter-001",
  "anonymized_content":
    "Patient: <PERSON_DEMO_000001>, geb. <DATE_TIME_DEMO_000002>\nKlinik: <ORGANIZATION_DEMO_000003>\nE-Mail: <EMAIL_ADDRESS_DEMO_000004>",
  "detected_entity_size": 4,
  "findings": [ /* per-span: Start, End, Score, EntityType, RecognizerName */ ],
  "tokens":   [ /* {token, entity_type, start, end} */ ]
}
```

The cleartext is stored in an in-memory vault keyed by token. Save the
`anonymized_content` and ship it to your downstream system — your tenant
keeps the originals.

Notes:
- No `language` field needed — auto-detected from the content.
- Add `"language": "de"` or `"en"` to override.
- `tenant_id` and `doc_id` are required; tokens are namespaced by them.

## 3. Ingest JSON

```bash
curl -s -X POST http://localhost:8081/v1/ingest \
  -H "Content-Type: application/json" \
  -d '{
    "tenant_id": "demo",
    "doc_id":    "record-002",
    "content_format": "json",
    "content": "{\"patient\":\"Hans Müller\",\"email\":\"hans@klinik.de\",\"notes\":\"geboren am 22.04.1970 in München\"}"
  }'
```

JSON ingest walks every string leaf, finds entities per-leaf, and
substitutes tokens in place. The shape of the JSON is preserved
verbatim — only string values change.

Response `anonymized_content`:

```json
{"patient":"<PERSON_DEMO_000005>","email":"<EMAIL_ADDRESS_DEMO_000006>","notes":"geboren am <DATE_TIME_DEMO_000007> in <LOCATION_DEMO_000008>"}
```

## 4. Detokenize (lookup tokens → cleartext)

You hold a few tokens from the ingest response and want the originals.
Detokenize requires `actor` + `purpose` for audit. The default policy in
`cmd/anonde/main.go` (`StaticPolicy`) allows everything; in production
you'd swap in your own `PolicyAuthorizer`.

```bash
curl -s -X POST http://localhost:8081/v1/detokenize \
  -H "Content-Type: application/json" \
  -d '{
    "tenant_id": "demo",
    "doc_id":    "letter-001",
    "actor":     "billing-team",
    "purpose":   "invoice-generation",
    "tokens":    ["<PERSON_DEMO_000001>", "<EMAIL_ADDRESS_DEMO_000004>"]
  }'
```

Response:

```json
{
  "tenant_id": "demo",
  "doc_id": "letter-001",
  "resolved": {
    "<PERSON_DEMO_000001>": "Anna Schmidt",
    "<EMAIL_ADDRESS_DEMO_000004>": "anna.schmidt@klinik.de"
  }
}
```

Tokens not linked to the doc you reference are rejected — even if they
exist for another doc under the same tenant.

## 5. Reveal (substitute tokens back into content)

You have the anonymized output and want the original document. Reveal
auto-walks the content, replaces tokens with cleartext, and respects
the original format.

### Text

```bash
curl -s -X POST http://localhost:8081/v1/reveal \
  -H "Content-Type: application/json" \
  -d '{
    "tenant_id": "demo",
    "doc_id":    "letter-001",
    "actor":     "doctor",
    "purpose":   "clinical-review",
    "content_format": "text",
    "content":   "Patient: <PERSON_DEMO_000001>, geb. <DATE_TIME_DEMO_000002>\nKlinik: <ORGANIZATION_DEMO_000003>\nE-Mail: <EMAIL_ADDRESS_DEMO_000004>"
  }'
```

Returns the original German text with every token replaced.

### JSON

```bash
curl -s -X POST http://localhost:8081/v1/reveal \
  -H "Content-Type: application/json" \
  -d '{
    "tenant_id": "demo",
    "doc_id":    "record-002",
    "actor":     "doctor",
    "purpose":   "clinical-review",
    "content_format": "json",
    "content":   "{\"patient\":\"<PERSON_DEMO_000005>\",\"email\":\"<EMAIL_ADDRESS_DEMO_000006>\",\"notes\":\"geboren am <DATE_TIME_DEMO_000007> in <LOCATION_DEMO_000008>\"}"
  }'
```

JSON reveal walks the same way ingest did — string leaves only. Object
keys, numbers, booleans, structure all stay untouched.

## End-to-end round-trip

Text round-trip is byte-exact:

```bash
ORIGINAL='Patient: Hans Müller, Klinik: Universitätsklinikum Heidelberg.'

ANON=$(curl -s -X POST http://localhost:8081/v1/ingest \
  -H "Content-Type: application/json" \
  -d "{\"tenant_id\":\"demo\",\"doc_id\":\"rt-text\",\"content_format\":\"text\",\"content\":$(jq -Rs . <<<"$ORIGINAL")}" \
  | jq -r '.anonymized_content')

echo "anon:     $ANON"

RESTORED=$(curl -s -X POST http://localhost:8081/v1/reveal \
  -H "Content-Type: application/json" \
  -d "{\"tenant_id\":\"demo\",\"doc_id\":\"rt-text\",\"actor\":\"tester\",\"purpose\":\"verify\",\"content_format\":\"text\",\"content\":$(jq -Rs . <<<"$ANON")}" \
  | jq -r '.deanonymized_content')

echo "restored: $RESTORED"
[ "$RESTORED" = "$ORIGINAL" ] && echo "match: yes" || echo "match: no"
```

JSON round-trip recovers every cleartext value exactly but may **reorder
object keys** and re-emit whitespace — Go's `encoding/json` marshals
keys alphabetically. Compare semantically (e.g. `jq -S . a == jq -S . b`)
rather than byte-for-byte. The PHI guarantees still hold: every entity
in every string leaf is detected on ingest and restored on reveal.

## Other content formats

`content_format` accepts `text`, `json`, `ndjson`, `logs`, `pdf`, and
`auto`. NDJSON treats each line as a separate JSON document, logs are
mixed text/JSON (with ANSI stripped), and PDF is base64-encoded — see
`internal/content/content.go`.

## Docker image variants

Two Dockerfiles ship with the repo, pick per workload:

| Image | Built from | Size | Backend | Use when |
|---|---|---|---|---|
| `anonde:patterns` | `Dockerfile.anonde` | ~12 MB | patterns-only | German clinical text, structured English fields (Patient:, MRN), no narrative names |
| `anonde:ner` | `Dockerfile.anonde-ner` | ~275 MB | hugot ONNX NER + patterns | English narrative needs proper PERSON detection; willing to pay 30× image size for ~90% name recall |

```bash
# default (patterns-only)
docker build -f Dockerfile.anonde -t anonde:patterns .

# NER variant (model baked in at build time)
docker build -f Dockerfile.anonde-ner -t anonde:ner .
```

On Fly, the cleanest way to flip the same app between the two variants
is to use a per-variant config file. Two configs ship with the repo,
both targeting the same app `anonde-platform`:

```bash
fly deploy --config fly.toml       # patterns-only build, ~12 MB image
fly deploy --config fly.ner.toml   # NER build, ~275 MB image
```

The second deploy fully replaces the first. Both serve traffic on
`https://anonde-platform.fly.dev`. Verify which is live:

```bash
fly logs -a anonde-platform | grep "analyzer backend:"
# → "analyzer backend: patterns-only (no NER)"  OR
# → "analyzer backend: hugot (model=...)"
```

The NER image runs offline once built — the ONNX model is baked into
`/models` during build (see the `DOWNLOAD_MODELS_ONLY=1` bootstrap in
`cmd/anonde/main.go`). No HuggingFace Hub calls at runtime.

First request after an NER deploy is slow (5–30 s) because hugot loads
the ONNX session into memory on first inference. The health check has
a 15 s grace period in `fly.ner.toml` for this reason. Subsequent
calls are ~10–100 ms each.

In-memory vault and store are cleared on each redeploy, so a token
issued under one variant cannot be revealed after switching to the
other.

## Notes

- All ingest data lives in memory (TTL configurable via
  `MEMORY_VAULT_TTL` and `MEMORY_STORE_TTL` env vars). The anonde server is
  designed for a tenant to plug in their own vault + store.
- The same cleartext within a single doc gets the same token. The same
  cleartext across docs does NOT — `mintToken` is per-(tenant, entity)
  with a monotonic counter, not content-addressable.
- Body size cap: `MAX_CONTENT_BYTES` env var, default 10 MiB.
