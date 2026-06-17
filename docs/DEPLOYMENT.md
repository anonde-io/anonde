# Deployment

anonde ships three Docker variants of the same `cmd/anonde` HTTP service. Pick per workload.

| File | What it ships | Image size | When to use |
|---|---|---:|---|
| `Dockerfile.anonde` | Pure Go binary, no NER, no CGO | ~12 MB | patterns-only deployments; max throughput |
| `Dockerfile.anonde-ner` | Same binary + libonnxruntime + baked GLiNER BASE model | ~770 MB | production: detects PERSON/ORG/etc. via GLiNER. Bench Σ ALL ≈ 12.9% leak rate across 30 corpora. |
| `Dockerfile.anonde-ner-stack` | Same as `-ner` plus the LARGE GLiNER variant baked in too | ~2.1 GB | lowest-leak tier: registers BOTH base (span decoder) and LARGE (flat decoder) recognizers in one analyzer engine. Bench Σ ALL ≈ 8.4%. ~2× per-request inference latency vs `-ner` (both models run concurrently per request); peak RAM ~2.8 GB at single-instance, more with pooling. |

## Running a variant

Three peer entry points; all build the same image you'd ship.

| Workflow | Patterns | NER | NER stack |
|---|---|---|---|
| Make | `make docker-run` | `make docker-run-ner` | (build only: `make docker-build-ner-stack`) |
| Compose | `docker compose --profile patterns up` | `docker compose --profile ner up` | `docker compose --profile ner-stack up` |
| Raw docker | `docker build -f Dockerfile.anonde -t anonde:patterns . && docker run --rm -p 8081:8080 anonde:patterns` | see [`Dockerfile.anonde-ner`](../Dockerfile.anonde-ner) header | see [`Dockerfile.anonde-ner-stack`](../Dockerfile.anonde-ner-stack) header |

Compose profiles are mutually exclusive: only one runs per `docker compose up`. All publish the API on `${ANONDE_PORT:-8081}`; the NER profiles additionally expose Prometheus on `${METRICS_PORT:-9090}`. No volumes; models are baked into the NER images; persist the vault with a bbolt path (see "Vault + request limits" below) if you need state across restarts.

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
| `patterns` | No NER. 70 regex/checksum pattern recognizers only. | default Go build |
| `gliner` | One GLiNER PII recognizer (span decoder, BASE model). | `-tags ner` + CGO |
| `gliner-flat` | One GLiNER recognizer with the flat / token decoder (LARGE-style 4-input BIO ONNX exports). | `-tags ner` + CGO |
| `gliner-stack` | BOTH the span-decoder base + the flat-decoder recognizer in one engine. The conflict resolver unions findings. Lowest-leak option; pairs with `Dockerfile.anonde-ner-stack`. | `-tags ner` + CGO |
| `gliner-ensemble` | Multi-model GLiNER ensemble; gated by `ANONDE_NER_STACK=id1,id2,...` env var. | `-tags ner` + CGO |

### NER variant (defaults wired in `Dockerfile.anonde-ner`)

```bash
ANALYZER_BACKEND=gliner
GLINER_MODELS_DIR=/models
GLINER_MODEL=knowledgator/gliner-pii-base-v1.0
GLINER_QUANT=fp32                       # FP32 by default (production)
GLINER_THRESHOLD=0.40
ORT_SO_PATH=/lib/libonnxruntime.so.1    # arch-neutral path; image is multi-arch
```

> **Model default note.** The bare `-tags ner` binary's `gliner` backend defaults to the multilingual `onnx-community/gliner_multi_pii-v1` when `GLINER_MODEL` is unset. The shipped NER images and the recommended production config pin `knowledgator/gliner-pii-base-v1.0` via `GLINER_MODEL` (the documented production default) — set it explicitly if you run your own binary.

Memory-constrained deployments can opt back into INT8 at runtime with `GLINER_QUANT=int8` — it auto-resolves the correct per-repo quantized filename (`onnx/model_quint8.onnx` for knowledgator, `onnx/model_quantized.onnx` for onnx-community) without a rebuild. INT8 saves ~240 MB image size at the cost of ~6pp Σ ALL leak rate on multilingual legal / clinical text (measured in the bench matrix). For a smaller pre-built image, rebuild with the INT8 ONNX baked in; runtime `GLINER_QUANT` is the cheaper flip when the FP32 file is already on disk.

| Var | Default | What |
|---|---|---|
| `GLINER_QUANT` | `fp32` | ONNX precision selector: `fp32` (production, lowest leak), `int8` (smallest, ~6pp more leak), or `fp16`. Auto-resolves the conventional filename per HF repo; `GLINER_ONNX_FILE` overrides it with an explicit in-repo path. (Note: `fp16` is currently rejected by onnxruntime for `gliner-pii-base-v1.0`; use `fp32`.) |

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
| `ANONDE_ORT_INTER_OP_THREADS` | ORT default (1) | Threads used to run independent ops in parallel. Rarely worth tweaking for GLiNER; its compute graph is mostly sequential. |
| `ANONDE_ORT_GRAPH_OPT_LEVEL` | ORT default (`basic`) | One of `disabled`, `basic`, `extended`, `all`. Higher levels can shave 5–15% per inference at the cost of longer first-call init. Try `extended` first; `all` may break on specific ONNX exports. |

### GLiNER / NER tuning

These knobs change detection behaviour and output, not just performance. Defaults are tuned for recall; reach for them when you see specific false positives or want a stricter precision profile.

| Var | Default | What |
|---|---|---|
| `ANONDE_ALLOW_NER_FALLBACK` | unset (fail-closed) | When an NER backend is selected but the model can't load/verify, the server **exits** rather than silently serving patterns-only under a NER label. Set `1` to instead log a loud ERROR and degrade to patterns-only (reduced recall, PII leaks possible). Off is the safe default for a redaction tool. |
| `GLINER_THRESHOLD` | recognizer default (`0.40`) | Global span-confidence floor. The single highest-impact GLiNER knob. Multilingual variants usually want lower (~0.25); the English base ~0.40. |
| `GLINER_PERSON_THRESHOLD` | unset | Per-class override for `PERSON`. Used directly (not `min()`'d), so you can raise a noisy class above the global floor to cut false positives. |
| `GLINER_ORG_THRESHOLD` | unset | Per-class override for `ORGANIZATION`. |
| `GLINER_LOCATION_THRESHOLD` | unset | Per-class override for `LOCATION`. |
| `GLINER_NRP_THRESHOLD` | unset | Per-class override for `NRP` (nationality / religion / political group). |
| `GLINER_STRICT` | unset (off) | `1` enables the bench-picked STRICT precision profile: per-class floors (PERSON `0.50`, ORG/LOC/NRP `0.55`) for any class without an explicit override, **and** the structural shape filter (see below). |
| `GLINER_SPAN_FILTER` | unset (money guard on) | The NER path always runs a universal **money guard** (drops currency-amount `ID`/`POSTAL_CODE` spans; bench-proven leak-safe on every corpus). `1` additionally enables the opt-in **structural shape filter** (UUID / locale / semver / model-slug / hex / base64 / SCREAMING_SNAKE on fuzzy types + stoplist) — a precision tool that trades recall on PII-dense traffic, so it is off by default. Implied on by `GLINER_STRICT=1`. `GLINER_LABEL_SET=legal` applies this filter plus the legal role/statute/exhibit profile automatically. |
| `GLINER_STOPLIST` | empty | Comma-separated extra denylist terms appended (lower-cased) to the shape filter's built-in stoplist. Only effective when the shape filter is on. |
| `GLINER_POOL_SIZE` | 1 (single recognizer) | Integer ≥ 2 builds an N-instance pool for the BASE recognizer to serve concurrent requests (each instance serialises its own ONNX session). Sized against RAM, not vCPU — see Pool sizing above. |
| `ANONDE_NER_STACK` | unset | Comma-separated GLiNER model IDs. When set with `ANALYZER_BACKEND=gliner`, builds a multi-model ensemble (OR-union of findings) instead of the single-model path. A value that trims to empty (e.g. `,,,`) fails loudly at boot rather than silently disabling NER. |
| `ANONDE_NER_STACK_PARALLEL` | unset (sequential) | `1` runs ensemble members concurrently (one goroutine each) when latency matters more than peak RAM. Default runs them sequentially. |
| `WARMUP_ON_START` | unset | `1` fires one startup Analyze and pre-warms every pool instance in parallel, so the first user requests don't each pay the 5–30 s cold ONNX init. Recommended for any NER deploy behind a load balancer. |
| `ANONDE_NER_VERIFY_TIMEOUT` | `5m` | Timeout for the boot-time fail-closed NER verification (above). Raise on slow disks / cold model caches; lower for a tighter boot SLA. |
| `ANONDE_WARMUP_TIMEOUT` | `5m` | Timeout for the `WARMUP_ON_START` priming call. |

### Vault + request limits (both variants)

| Var | Default | What |
|---|---|---|
| `ANONDE_VAULT_TTL` | `0` (no expiry) | token ↔ cleartext retention. `0` means entries never auto-expire — they live until you `DELETE` them or the process restarts (in-memory backend) / are evicted. Set e.g. `30m` to bound retention. |
| `ANONDE_STORE_TTL` | `0` (no expiry) | anonymized-document retention. Same `0` = no auto-expiry semantics as above. |
| `MAX_CONTENT_BYTES` | 10 MiB | request body cap |

### Persistent data directory

All three shipped images set `ANONDE_DATA_DIR=/var/lib/anonde` and
declare it as a Docker `VOLUME`. Everything anonde persists on disk
lives under that one path:

| File | Purpose |
|---|---|
| `/var/lib/anonde/install_id` | telemetry install ID; stable across restarts |
| `/var/lib/anonde/anonde.db` | bbolt vault DB (only when `STORE_BACKEND=bbolt`) |

For durability across `docker rm`, mount a named volume to the same
path: `-v anonde-data:/var/lib/anonde`. The `docker-compose.yml`
already does this per profile. Without a named volume Docker creates
an anonymous volume that survives `stop`/`start` but not `rm`.

Override knobs:

| Var | Effect |
|---|---|
| `ANONDE_DATA_DIR` | the anchor; both the install_id and the bbolt DB live under it |
| `XDG_DATA_HOME` | telemetry-only fallback; when set without `ANONDE_DATA_DIR`, install_id lands at `$XDG_DATA_HOME/anonde/install_id` |

Library users and bare-binary deployments see no behaviour change —
without the anchor, install_id falls back to
`$XDG_DATA_HOME/anonde/install_id` (or `~/.local/share/anonde/install_id`)
and bbolt to CWD-relative `anonde.db`, as before.

## PDF + OCR

There are two PDF surfaces, both backed by the same
`internal/content` primitives:

1. **`content_format: "pdf"` on `POST /v1/anonymizations`:** base64
   PDF in, tokenised text + vault out. Reversible via the standard
   `/reveal` endpoint. Always available; doesn't need
   `ANONDE_PDF_ENABLED`.
2. **`POST /v1/anonymizations/pdf`:** raw PDF body in, redacted PDF
   body out, with the original retained for
   `GET /v1/anonymizations/{id}/reveal-pdf`. Opt-in via
   `ANONDE_PDF_ENABLED=1` because it shells out to `pdftoppm` +
   `tesseract` and (optionally) loads a vision model. Returns
   `501 Not Implemented` when the env var isn't set.

Both extract the PDF text layer via `ledongthuc/pdf` first. When the
layer is empty or shorter than `ANONDE_OCR_TEXT_FLOOR` bytes (default
64), i.e. an image-only scan with no text layer, anonde transparently
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
  - `X-Anonde-Id`: the minted anonymization id (`anon_<hex>`), needed
    for `/reveal-pdf` and `DELETE /v1/anonymizations/{id}`.
  - `X-Anonde-Tenant`: echo of the resolved tenant.
  - `X-Anonde-Entities`: total redacted span count.
  - `X-Anonde-Entity-Types`: number of distinct entity types found.
  - `X-Anonde-Entity-Count`: repeated header, one `TYPE=N` per
    detected entity type (e.g. `PERSON=4`, `EMAIL_ADDRESS=2`).

`GET /v1/anonymizations/{id}/reveal-pdf` takes the same tenant
header / query and returns the original PDF bytes. Subject to the same
`ANONDE_STORE_TTL` as text anonymizations: 404 once expired (only if a
non-zero TTL is set) or deleted.

### Env vars

| Var | Default | What |
|---|---|---|
| `ANONDE_PDF_ENABLED` | unset | Set to `1` to mount the PDF endpoint pair AND eagerly load the YOLOS signature detector (~500 MB resident). When unset the routes return `501` with a hint pointing at this var. The signature model is always loaded when PDF is enabled; there is no per-deploy way to skip it. |
| `ANONDE_SIGNATURE_MODEL_PATH` | _(baked path in NER image)_ | Override path to the signature ONNX. The `anonde-ner` image bakes one at `/models/signature/yolos-base-signature-${SIGNATURE_QUANT}.onnx`. |
| `SIGNATURE_QUANT` | `fp32` | Build arg for `Dockerfile.anonde-ner` selecting the signature ONNX precision baked into the image. `fp32` (~1.13 GB image, recommended), `fp16` (~960 MB), `int8` (~870 MB; measurably worse signature recall, more missed signatures). |
| `SIGNATURE_THRESHOLD` | `0.20` | YOLOS confidence floor. Lower = more aggressive coverage (catches faint logos / stamps the default would miss), higher = fewer false positives. `0.18` is the lowest safe value before the model starts firing on dense text blocks. The default was lowered from the model-published `0.25` to `0.20` after a live test against scanned poprire / real-estate / insurance forms surfaced missed heraldic logos. |
| `ANONDE_OCR_ENABLED` | _(unset → on if both binaries present)_ | Set to `false` / `0` / `off` to disable the OCR fallback even when the binaries are installed. |
| `ANONDE_OCR_LANGS` | `eng+deu+fra+spa+ita+ron` | Tesseract language string. Restrict to known-corpus languages (e.g. `eng` alone) for faster OCR; each loaded model costs ~30–50 MB RAM. |
| `ANONDE_OCR_DPI` | `300` | Rasterisation DPI passed to `pdftoppm -r`. Drop to `200` for faster OCR on clean scans; raise to `400+` for low-quality photographs of documents. |
| `ANONDE_OCR_TEXT_FLOOR` | `64` | Byte threshold below which the text-layer extraction is treated as empty and OCR fires. Small floor catches PDFs whose text layer holds only stray whitespace / single-line metadata. |

Tesseract page-segmentation mode is fixed at PSM 3 (fully automatic);
this preserves table-row reading order on multi-column scans like
government forms, which PSM 6 ("single block") loses.

### Per-request PDF knobs

Every knob the (retired) `anonymize-pdf` CLI exposed binds from URL
query parameters on `POST /v1/anonymizations/pdf`. The request body is
the raw PDF (`Content-Type: application/pdf`); the tenant comes from
the `X-Anonde-Tenant` header or `?tenant=<id>`.

| Query param | Default | What |
|---|---|---|
| `mode` | `visual` | `visual` draws black boxes over each PII span on the original page rasters. `text` emits a re-rendered text PDF with `#` substitutions. |
| `operator` | `mask` | In `text` mode, the anonymizer operator: `mask` (`#`s) or `redact` (`<REDACTED>`). |
| `mask_char` | `#` | Character used by the `mask` operator. |
| `ocr_langs` | _(empty → server `ANONDE_OCR_LANGS`)_ | Tesseract languages, plus-separated. URL-encode as `eng%2Bdeu`. |
| `entities` | _(empty → all)_ | Repeated; detector allow-list, e.g. `entities=PERSON&entities=LOCATION`. |
| `score_threshold` + `score_threshold_set` | analyzer default | Set both to override the boot-time score floor. `score_threshold_set` lets the wire format distinguish "field absent" from "field present and 0". |
| `disable_ner` | `false` | Skip NER recognizers in the analyzer pipeline for this request. |
| `dpi` | `200` | Visual-mode rasterisation DPI. |
| `box_padding` | `2` | Pixels of padding around each PII box in visual mode (covers OCR baseline jitter). |
| `disable_visual_heuristic` | `false` | Visual mode: turn off the ink-density heuristic. Inverted polarity so the zero value keeps the heuristic on. |

The YOLOS signature detector is not a per-request toggle; it's loaded
at boot whenever `ANONDE_PDF_ENABLED=1`. To run without it, leave PDF
disabled entirely.

## CI

`.github/workflows/bench.yml` runs on every push to `main` and every PR whose changes touch `analyzer/**`, `bench/**`, `cmd/anonde/**`, or the build chain:

1. Builds the default (no-CGO) target.
2. Builds the `-tags ner` target with CGO.
3. Runs the Go unit-test suite.
4. Runs `make corpus-openmed && make corpus-synth_clinical`: patterns + GLiNER + GLiNER-py sidecar across two German corpora.
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
