# Deployment

anonde ships three Docker variants of the same `cmd/anonde` HTTP service. Pick per workload.

| File | What it ships | Image size | When to use |
|---|---|---:|---|
| `Dockerfile.anonde` | Pure Go binary, no NER, no CGO | ~12 MB | patterns-only deployments; max throughput |
| `Dockerfile.anonde-ner` | Same binary + libonnxruntime + baked GLiNER BASE model | ~770 MB | production: detects PERSON/ORG/etc. via GLiNER. Bench Σ ALL ≈ 12.9% leak rate across 30 corpora. |
| `Dockerfile.anonde-ner-stack` | Same as `-ner` plus the LARGE GLiNER variant baked in too | ~2.1 GB | lowest-leak tier: registers BOTH base (span decoder) and LARGE (flat decoder) recognizers in one analyzer engine. Bench Σ ALL ≈ 8.4%. ~2× per-request inference latency vs `-ner` (both models run concurrently per request); peak RAM ~2.8 GB at single-instance, more with pooling. |

## NER image internals

- Uses `gcr.io/distroless/cc-debian12` (needs glibc for libonnxruntime).
- Downloads `libonnxruntime.so.1.26.0` from Microsoft's release tarball at build time and copies it to `/usr/lib/x86_64-linux-gnu/libonnxruntime.so.1`.
- Bakes the GLiNER ONNX + tokenizer into `/models/` so first-request startup needs no outbound network.
- Warms the recognizer at process start via `WARMUP_ON_START=1` so the first user request doesn't pay the model-init cost.

First request after a cold NER deploy is slow (5–30 s) because the ONNX session loads into memory on first inference. Subsequent calls are ~10–100 ms each.

In-memory vault and store are cleared on each redeploy, so a token issued under one variant cannot be revealed after switching to the other.

## Env vars

### Analyzer backend selection

`ANALYZER_BACKEND` picks the NER stack. Defaults to `patterns` (no NER, the small image's behaviour).

| Value | What it does | Build requirement |
|---|---|---|
| `patterns` | No NER. 52 regex/checksum pattern recognizers only. | default Go build |
| `hugot` | One XLM-R PII transformer via hugot's ONNX runtime. | `-tags hugot` + CGO |
| `gliner` | One GLiNER PII recognizer (span decoder, BASE model). | `-tags hugot` + CGO |
| `gliner-flat` | One GLiNER recognizer with the flat / token decoder (LARGE-style 4-input BIO ONNX exports). | `-tags hugot` + CGO |
| `gliner-stack` | BOTH the span-decoder base + the flat-decoder recognizer in one engine. The conflict resolver unions findings. Lowest-leak option; pairs with `Dockerfile.anonde-ner-stack`. | `-tags hugot` + CGO |
| `gliner-ensemble` | Multi-model GLiNER ensemble; gated by `ANONDE_NER_STACK=id1,id2,...` env var. | `-tags hugot` + CGO |
| `ollama` | NER via a local Ollama daemon (no in-process model). | default Go build |

### NER variant (defaults wired in `Dockerfile.anonde-ner`)

```bash
ANALYZER_BACKEND=gliner
GLINER_MODELS_DIR=/models
GLINER_MODEL=knowledgator/gliner-pii-base-v1.0
GLINER_ONNX_FILE=onnx/model.onnx        # FP32 by default (production)
GLINER_THRESHOLD=0.40
ORT_SO_PATH=/lib/libonnxruntime.so.1    # arch-neutral path; image is multi-arch
```

Memory-constrained deployments can opt back into INT8 by rebuilding with `GLINER_ONNX_FILE=onnx/model_quint8.onnx` (saves ~240 MB image size at the cost of ~6pp Σ ALL leak rate on multilingual legal / clinical text — measured in the bench matrix).

### Stack variant (defaults wired in `Dockerfile.anonde-ner-stack`)

Everything from the NER variant plus:

```bash
ANALYZER_BACKEND=gliner-stack
ANONDE_GLINER_FLAT_MODEL=knowledgator/gliner-pii-large-v1.0
ANONDE_GLINER_FLAT_ONNX_FILE=model.onnx       # repo-root, not under onnx/
# ANONDE_GLINER_FLAT_THRESHOLD=                 # defaults to GLINER_THRESHOLD
```

### Pool sizing (concurrency under load)

Each `GLiNERRecognizer` serialises its `Analyze()` calls on an internal mutex (the ONNX session isn't safe for concurrent `Run()`). For multi-request concurrency, opt into N-instance pools:

| Var | Default | What |
|---|---|---|
| `GLINER_POOL_SIZE` | 1 (single recognizer) | Integer ≥ 2 → builds an N-instance pool for the BASE recognizer (`gliner` and `gliner-stack`) or the FLAT recognizer (`gliner-flat`). |
| `ANONDE_GLINER_FLAT_POOL_SIZE` | 1 | Integer ≥ 2 → N-instance pool for the FLAT slot of `gliner-stack` only. Separate from BASE because LARGE is ~3× the RAM. |
| `WARMUP_ON_START` | unset | `1` fires a startup Analyze + pre-warms every pool instance in parallel, so the first user requests don't pay 5–30 s cold init per instance. |

Memory cost is the binding constraint: ~500 MB per BASE instance, ~1.4 GB per LARGE instance. Sample sizings:

- 4 GB VM, gliner-stack: `GLINER_POOL_SIZE=2 ANONDE_GLINER_FLAT_POOL_SIZE=1` ≈ 1 GB BASE + 1.4 GB FLAT + ~500 MB overhead ≈ 2.9 GB peak.
- 8 GB VM, gliner-stack: `GLINER_POOL_SIZE=4 ANONDE_GLINER_FLAT_POOL_SIZE=2` ≈ 2 GB + 2.8 GB + overhead ≈ 5.3 GB peak.

### HTTP concurrency budget

| Var | Default | What |
|---|---|---|
| `ANONDE_MAX_CONCURRENT_REQUESTS` | unset (no limit) | Integer ≥ 1 caps in-flight requests at the HTTP layer. Over-cap requests return `HTTP 429 Too Many Requests` with `Retry-After: 1`. Use to backpressure bursts before they queue past the pool and OOM the host. Rule of thumb: set to `1.5 × GLINER_POOL_SIZE`. |

### ORT session tuning

Applies to every GLiNER recognizer (single + pooled). Defaults match onnxruntime's built-in choices; set explicitly only when you have evidence to.

| Var | Default | What |
|---|---|---|
| `ANONDE_ORT_INTRA_OP_THREADS` | ORT default (≈ num cores) | Threads used inside one ONNX op (matmul / attention). Set lower if you want to leave cores for HTTP serving; set explicitly to mismatch host vCPU detection. |
| `ANONDE_ORT_INTER_OP_THREADS` | ORT default (1) | Threads used to run independent ops in parallel. Rarely worth tweaking for GLiNER — its compute graph is mostly sequential. |
| `ANONDE_ORT_GRAPH_OPT_LEVEL` | ORT default (`basic`) | One of `disabled`, `basic`, `extended`, `all`. Higher levels can shave 5–15% per inference at the cost of longer first-call init. Try `extended` first; `all` may break on specific ONNX exports. |

### Vault + request limits (both variants)

| Var | Default | What |
|---|---|---|
| `MEMORY_VAULT_TTL` | `5m` | token ↔ cleartext retention |
| `MEMORY_STORE_TTL` | `5m` | anonymized-document retention |
| `MAX_CONTENT_BYTES` | 10 MiB | request body cap |

## PDF + OCR

There are two PDF surfaces, both backed by the same
`internal/content` primitives:

1. **`content_format: "pdf"` on `POST /v1/anonymizations`** — base64
   PDF in, tokenised text + vault out. Reversible via the standard
   `/reveal` endpoint. Always available; doesn't need
   `ANONDE_PDF_ENABLED`.
2. **`POST /v1/anonymizations/pdf`** — raw PDF body in, redacted PDF
   body out, with the original retained for
   `GET /v1/anonymizations/{id}/reveal-pdf`. Opt-in via
   `ANONDE_PDF_ENABLED=1` because it shells out to `pdftoppm` +
   `tesseract` and (optionally) loads a vision model. Returns
   `501 Not Implemented` when the env var isn't set.

Both extract the PDF text layer via `ledongthuc/pdf` first. When the
layer is empty or shorter than `ANONDE_OCR_TEXT_FLOOR` bytes (default
64) — i.e. an image-only scan with no text layer — anonde transparently
rasterises each page with `pdftoppm` and OCRs it with `tesseract`,
then feeds the joined text into the normal analyzer pipeline. Pure
shell-out: no CGO, no Go dependencies.

The `anonde-ner` and `anonde-ner-stack` images install
`poppler-utils` + `tesseract-ocr` with the
`eng+deu+fra+spa+ita+ron` language packs, so OCR is on by default
there. The patterns-only image (`anonde`) does not bundle them; the
OCR helper is a no-op when `pdftoppm` / `tesseract` aren't on `PATH`,
so behaviour for non-scanned PDFs is preserved and the image stays
~12 MB. To enable OCR on a custom image, install both binaries plus
the language packs you need; nothing else changes in the server.

### `POST /v1/anonymizations/pdf` shape

- Request: raw `application/pdf` body. (Earlier ad-hoc handler also
  accepted `multipart/form-data` with a `file` field; that was dropped
  when the endpoint moved into the proto-defined `AnonymizePDF` RPC so
  the gRPC / Connect / REST surfaces share one canonical shape. Wrap
  the file in a raw POST body to keep wire-compat.) Tenant via the
  `X-Anonde-Tenant: <id>` header (preferred) or `?tenantId=<id>` query
  (the same convention as `DELETE /v1/anonymizations/{id}?tenantId=…`).
- Response: `application/pdf` body (the redacted PDF) plus these response
  headers:
  - `X-Anonde-Id` — the minted anonymization id (`anon_<hex>`), needed
    for `/reveal-pdf` and `DELETE /v1/anonymizations/{id}`.
  - `X-Anonde-Tenant` — echo of the resolved tenant.
  - `X-Anonde-Entities` — total redacted span count.
  - `X-Anonde-Entity-Types` — number of distinct entity types found.
  - `X-Anonde-Entity-Count` — repeated header, one `TYPE=N` per
    detected entity type (e.g. `PERSON=4`, `EMAIL_ADDRESS=2`).

`GET /v1/anonymizations/{id}/reveal-pdf` takes the same tenant
header / query and returns the original PDF bytes. Subject to the same
`MEMORY_STORE_TTL` as text anonymizations — 404 once expired or
deleted.

### Env vars

| Var | Default | What |
|---|---|---|
| `ANONDE_PDF_ENABLED` | unset | Set to `1` to mount the PDF endpoint pair. When unset the routes return `501` with a hint pointing at this var. |
| `ANONDE_PDF_VISION_MODEL` | unset | Set to `1` to load the YOLOS signature detector at boot. Lets the visual redactor cover signatures, stamps, and logos that no OCR will see. Costs ~500 MB RAM at the FP32 default. |
| `ANONDE_SIGNATURE_MODEL_PATH` | _(baked path in NER image)_ | Override path to the signature ONNX. The `anonde-ner` image bakes one at `/models/signature/yolos-base-signature-${SIGNATURE_QUANT}.onnx`. |
| `SIGNATURE_QUANT` | `fp32` | Build arg for `Dockerfile.anonde-ner` selecting the signature ONNX precision baked into the image. `fp32` (~1.13 GB image, recommended), `fp16` (~960 MB), `int8` (~870 MB; measurably worse signature recall — more missed signatures). |
| `ANONDE_OCR_ENABLED` | _(unset → on if both binaries present)_ | Set to `false` / `0` / `off` to disable the OCR fallback even when the binaries are installed. |
| `ANONDE_OCR_LANGS` | `eng+deu+fra+spa+ita+ron` | Tesseract language string. Restrict to known-corpus languages (e.g. `eng` alone) for faster OCR — each loaded model costs ~30–50 MB RAM. |
| `ANONDE_OCR_DPI` | `300` | Rasterisation DPI passed to `pdftoppm -r`. Drop to `200` for faster OCR on clean scans; raise to `400+` for low-quality photographs of documents. |
| `ANONDE_OCR_TEXT_FLOOR` | `64` | Byte threshold below which the text-layer extraction is treated as empty and OCR fires. Small floor catches PDFs whose text layer holds only stray whitespace / single-line metadata. |

Tesseract page-segmentation mode is fixed at PSM 3 (fully automatic);
this preserves table-row reading order on multi-column scans like
government forms, which PSM 6 ("single block") loses.

### `anonymize-pdf` CLI

`cmd/anonymize-pdf` ships a one-shot CLI that wraps the same
`internal/content` primitives — useful for batch / offline workflows
or for callers who want to redact PDFs without standing up the HTTP
server. `Dockerfile.anonymize-pdf` builds an image with the binary +
`poppler-utils` + `tesseract` + `libonnxruntime` already in place.

```bash
docker build -f Dockerfile.anonymize-pdf -t anonymize-pdf .

# First run downloads ~880 MB of models (GLiNER PII + YOLOS signature
# FP32) into the named volume. Subsequent runs reuse the cache.
docker run --rm \
  -v "$PWD:/data" \
  -v anonde-models:/root/.cache/anonde \
  anonymize-pdf /data/in.pdf /data/out.pdf
```

Flags (see `anonymize-pdf -help` for the full list):

| Flag | Default | What |
|---|---|---|
| `--mode` | `visual` | `visual` draws black boxes over each PII span on the original page rasters (Private AI / Limina shape). `text` emits a re-rendered text PDF with `#` substitutions. |
| `--operator` | `mask` | In `text` mode, the anonymizer operator: `mask` (`#`s) or `redact` (`<REDACTED>`). |
| `--mask-char` | `#` | Character used by the `mask` operator. |
| `--backend` | `auto` | `auto` picks `gliner` when the `-tags hugot` build is present, else patterns. Force with `patterns` / `gliner` / `hugot`. |
| `--langs` | _(empty → `ANONDE_OCR_LANGS`)_ | Tesseract languages, comma- or plus-separated. |
| `--entities` | _(empty → all)_ | Comma-separated detector allow-list (e.g. `PERSON,LOCATION`). |
| `--score-threshold` | `0.3` | Minimum recognizer score (matches the HTTP server default). |
| `--dpi` | `200` | Visual-mode rasterisation DPI. |
| `--box-padding` | `2` | Pixels of padding around each PII box in visual mode (covers OCR baseline jitter). |
| `--visual-heuristic` | `true` | In visual mode, also redact ink regions not covered by confident OCR — catches signatures, stamps, logos when the signature model isn't loaded. |
| `--signature-model` | `false` | Load the YOLOS signature ONNX. Requires `-tags hugot` build and a one-time model download. |
| `--signature-model-path` | _(downloads to `~/.cache/anonde/models/signature/`)_ | Override the signature ONNX path. |
| `--dump-text` | _(empty)_ | Optional path to dump the extracted (post-OCR) text for debugging. |

The CLI exits non-zero on any pipeline error and prints a per-type
finding summary to stderr; stdout is kept clean for scripting.

## CI

`.github/workflows/bench.yml` runs on every push to `main` and every PR whose changes touch `analyzer/**`, `bench/**`, `cmd/anonde/**`, or the build chain:

1. Builds the default (no-CGO) target.
2. Builds the `-tags hugot` target with CGO.
3. Runs the Go unit-test suite.
4. Runs `make corpus-openmed && make corpus-synth_clinical` — patterns + GLiNER + GLiNER-py sidecar across two German corpora.
5. Renders `bench/REPORT_MATRIX.md` and uploads it (+ `results_matrix.csv` + per-cell findings JSONLs) as workflow artifacts.
6. **Guard rail**: fails the job if either GLiNER cell produced 0 NER-attributable findings (caught a real silent-fallback bug the first time it landed).

The headline is rendered into the GitHub Actions job summary, so PR reviewers see numbers without downloading artifacts.

Local dev installs the bench Python deps via:

```bash
pip install -r bench/requirements.txt
```

The Presidio cell needs `presidio-analyzer + spacy + en_core_web_lg` separately (~700 MB; not in `requirements.txt` to keep CI fast):

```bash
pip install presidio-analyzer spacy
python -m spacy download en_core_web_lg
```
