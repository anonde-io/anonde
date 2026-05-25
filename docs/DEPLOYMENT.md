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
