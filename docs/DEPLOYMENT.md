# Deployment

anonde ships two Docker variants of the same `cmd/platform` HTTP service. Pick per workload.

| File | What it ships | Image size | When to use |
|---|---|---:|---|
| `Dockerfile.platform` | Pure Go binary, no NER, no CGO | ~12 MB | patterns-only deployments; max throughput |
| `Dockerfile.platform-ner` | Same binary + libonnxruntime + baked GLiNER model | ~470 MB | production: detects PERSON/ORG/etc. via GLiNER |

## Fly.io

Two configs target the same app `anonde-platform` in `iad`:

```bash
fly deploy --config fly.toml       # patterns-only build, ~12 MB image
fly deploy --config fly.ner.toml   # NER build, ~470 MB image
```

The second deploy fully replaces the first. Both serve traffic on `https://anonde-platform.fly.dev`. Verify which is live:

```bash
fly logs -a anonde-platform | grep "analyzer backend:"
# → "analyzer backend: patterns-only (no NER)"  OR
# → "analyzer backend: gliner (model=...)"
```

## NER image internals

- Uses `gcr.io/distroless/cc-debian12` (needs glibc for libonnxruntime).
- Downloads `libonnxruntime.so.1.26.0` from Microsoft's release tarball at build time and copies it to `/usr/lib/x86_64-linux-gnu/libonnxruntime.so.1`.
- Bakes the GLiNER ONNX + tokenizer into `/models/` so first-request startup needs no outbound network.
- Warms the recognizer at process start via `WARMUP_ON_START=1` (set in `fly.ner.toml`) so the first user request doesn't pay the model-init cost.

First request after a cold NER deploy is slow (5–30 s) because the ONNX session loads into memory on first inference. The health check has a 15 s grace period in `fly.ner.toml` for this reason. Subsequent calls are ~10–100 ms each.

In-memory vault and store are cleared on each redeploy, so a token issued under one variant cannot be revealed after switching to the other.

## Env vars

### NER variant (defaults wired in `Dockerfile.platform-ner`)

```bash
ANALYZER_BACKEND=gliner
GLINER_MODELS_DIR=/models
GLINER_MODEL=knowledgator/gliner-pii-base-v1.0
GLINER_ONNX_FILE=onnx/model_quint8.onnx
GLINER_THRESHOLD=0.40
ORT_SO_PATH=/usr/lib/x86_64-linux-gnu/libonnxruntime.so.1
```

### Vault + request limits (both variants)

| Var | Default | What |
|---|---|---|
| `MEMORY_VAULT_TTL` | `5m` | token ↔ cleartext retention |
| `MEMORY_STORE_TTL` | `5m` | anonymized-document retention |
| `MAX_CONTENT_BYTES` | 10 MiB | request body cap |

## CI

`.github/workflows/bench.yml` runs on every push to `main` and every PR whose changes touch `analyzer/**`, `bench/**`, `cmd/platform/**`, or the build chain:

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
