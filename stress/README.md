# stress

Load + edge-case tier for the anonde HTTP service. Runs against a
**real container** (every `Dockerfile.anonde{,-ner}`
variant) and exercises the production code path — env defaults, baked
models, glibc base image, CGO + libonnxruntime, tesseract + poppler,
the works.

This is **not** a merge gate. It's slow (image builds, model
loading, 15–30s attack windows × 2 variants × 5 cases) and
hardware-dependent. The merge gate is the patterns-only e2e tier in
[`e2e/`](../e2e/).

## Quick start

```bash
# Run the whole matrix locally (Docker required; first run pulls
# the NER image layers, ~10–15 min cold):
make stress

# Just patterns (~30 s if the image is cached):
go test -tags stress -run 'TestStress.*/patterns' -v ./stress/...

# One case across all variants:
go test -tags stress -run 'TestStress_PIIDense' -v ./stress/...
```

CI: triggered via `workflow_dispatch` or the weekly cron in
`.github/workflows/stress.yml`. Results are appended to the workflow
job summary.

## Why containers, not in-process

A pool-saturation or OOM test only proves anything against the actual
production artefact. In-process Go boot skips:

- The Docker entrypoint + env-var defaults baked into the image.
- libonnxruntime loading from the canonical path.
- The OCR shell-out chain (`pdftoppm` + `tesseract`).
- The signature-detector model bake.
- Real network round-trips through the gateway marshalers.

testcontainers-go consumes the existing Dockerfiles unchanged, so what
the test stresses is byte-identical to what a self-hoster pulls.

## Cases

| Test | Variants | What it asserts |
|---|---|---|
| `TestStress_PIIDense` | all | Sustained PII-dense load. Throughput regression guard, p99 envelope per variant, `anonde_entities_detected_total > 0`. |
| `TestStress_PoolSaturation` | ner | Concurrent requests > `ANONDE_MAX_CONCURRENT_REQUESTS`. Zero 5xx, container alive after; some 429s expected (warns if none). |
| `TestStress_PDFLargeDoc` | ner | Visual PDF redaction over a multi-page doc. Catches OCR + GLiNER + draw regressions. |
| `TestStress_BodyCap` | all | Oversized bodies → 4xx, never 5xx. Currently warns on the REST-gateway gap (see memory `rest-gateway-body-cap-gap`). |
| `TestStress_MultiTenant` | all | Tenant A blasts the server; tenant B `/v1/health` probe traffic stays under p99 budget. Fairness guard. |
| `TestStress_Cluster_StatefulRoundTrip` | patterns | N=3 backends behind an in-process sticky-session proxy. Hash `(tenant, id) → backend`, anonymize → reveal across the cluster. Asserts every backend got work AND sticky routing is deterministic (reveal lands on the mint backend). |

Cases queued for follow-up (see `stress_test.go` header comment):
TTL races, JSON recursion bomb, unicode adversarial, backend-down
failover (needs a shared store first — see [cluster.go](cluster.go)
header).

## Pre-flight checks

```bash
# Docker reachable?
docker info >/dev/null && echo ok

# Free disk for the NER image build (~1.13 GB):
df -h $(docker info --format '{{.DockerRootDir}}' 2>/dev/null || echo /var/lib/docker)
```

## Variant tuning knobs

`stress.Variants` in `harness.go` is the matrix. Each entry sets a few
env overrides on top of the Dockerfile defaults — notably
`GLINER_POOL_SIZE` and `ANONDE_MAX_CONCURRENT_REQUESTS`, so the
pool-saturation test has something interesting to do. Touch carefully:
the assertion thresholds in `stress_test.go` are calibrated to these
values.

## Why vegeta

Battle-tested constant-RPS attacker, HDR-backed latency histogram,
status-code aggregation. Wrapper in `loadgen.go` is intentionally
thin — the test code stays focused on the anonde-specific assertions
(no 5xx, entity counters tick, post-attack container is alive).
