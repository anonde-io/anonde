---
name: platform-engineer
description: |
  Apply when designing, building, deploying, or operating a Go-native local-first service. Covers multi-stage Dockerfiles + distroless variant selection, Fly.io deployment shape (auto-stop, warmup, machine sizing), HTTP API design (versioning, idempotency, tenant scoping, content-format negotiation), in-memory state lifecycle (TTL, eviction, OOM safety), Go build matrix (build tags, CGO opt-in, cross-arch), CI/CD design (path filters, caching, guard rails, artifacts), observability discipline, secrets handling, and cold-start vs steady-state tradeoffs. Concept-level; pair with a project skill for codebase-specific paths.
allowed-tools: Read, Bash, Edit, Write, Grep, Glob
---

# Platform engineer — patterns for local-first Go services

> Generic platform-engineering patterns extracted from production work on small-team Go services (HTTP API + optional ML backend, deployed on Fly.io). Reusable across any project of the same shape.

## What "local-first" implies for platform design

A local-first service either runs on the user's box or in a single small cloud machine, *not* in a fan-out of horizontally scaled microservices. That changes most platform decisions:

- **No service mesh** — direct HTTP. Skip Istio, Linkerd, Connect-RPC's full proto registry if you're under 5 endpoints.
- **No database tier required** — in-memory stores with TTL are often enough; persistence is optional and per-tenant.
- **Single-binary deployment** — Go's native cross-compile + distroless image. Don't introduce Python sidecars unless mandatory; never use them in the hot path.
- **Cold-start matters** — auto-stop machines wake on first request. Optimise the first 5 seconds, not the 1000th.
- **Observability is local** — `fly logs` / `docker logs` is your dashboard. No Datadog by default. Log discipline matters more than tooling.

## Containerization — multi-stage Dockerfiles

Two-stage pattern: builder produces the binary, runtime image strips everything else. The pieces:

```dockerfile
# --- build stage ---
FROM --platform=linux/amd64 golang:1.26-bookworm AS build
WORKDIR /src
RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates curl tar build-essential && \
    rm -rf /var/lib/apt/lists/*

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download

COPY . .
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=1 GOOS=linux GOARCH=amd64 \
    go build -tags <opt-in> -o /out/svc ./cmd/svc

# --- runtime stage ---
FROM gcr.io/distroless/cc-debian12
COPY --from=build /out/svc /svc
ENV ANALYZER_BACKEND=<production-default> ...
EXPOSE 8080
ENTRYPOINT ["/svc"]
```

### Distroless variant selection

| Image | Has | When to use |
|---|---|---|
| `gcr.io/distroless/static-debian12` | nothing dynamic — no glibc, no dynamic loader | Pure-Go statically-linked binary; default for CGO=0 builds |
| `gcr.io/distroless/cc-debian12` | glibc + libgcc + libstdc++ | CGO=1 builds; binaries that dlopen a shared lib (libonnxruntime, libxla, …) |
| `gcr.io/distroless/base-debian12` | glibc + libssl + libgomp + tzdata | When you need more system libs (rare for Go) |
| `debian:bookworm-slim` | everything | When you need a shell for debugging; trades 60+ MB for the convenience |

**Don't mix CGO=1 binaries with `distroless/static`** — the binary will silently fail to dlopen its shared lib because there's no dynamic linker. This is a high-frequency footgun.

### Multi-arch considerations

Apple Silicon dev + amd64 Fly target = cross-compile pain with CGO. Two safe paths:

1. **Pin build to linux/amd64** via `FROM --platform=linux/amd64 ...` on both build AND runtime stages. Docker Desktop on macOS uses Rosetta to run amd64 binaries; slower locally but matches deploy exactly and avoids cross-compile-with-CGO setup.
2. **Native build per arch** with `BUILDPLATFORM` and `crossbuild-essential-amd64` apt package. More setup, faster local builds.

Choose (1) unless local-build speed is provably blocking. (1) catches the "binary works locally but not on Fly" class of bugs that (2) hides.

## Fly.io deployment shape

The toml file shapes everything. Key knobs:

```toml
app = "my-svc"
primary_region = "iad"  # or your closest region

[build]
  dockerfile = "Dockerfile"

[env]
  WARMUP_ON_START = "1"   # see "Cold-start strategy" below

[http_service]
  internal_port = 8080
  force_https = true
  auto_stop_machines = "suspend"   # NOT "stop" — see below
  auto_start_machines = true
  min_machines_running = 0          # cost-optimal; fine if cold-start is OK

  [http_service.concurrency]
    type = "requests"
    hard_limit = 100
    soft_limit = 25

  [[http_service.checks]]
    interval = "10s"
    timeout = "5s"
    grace_period = "60s"      # Fly clamps >60s silently — don't lie to yourself
    method = "GET"
    path = "/healthz"
```

### `auto_stop_machines`: `suspend` beats `stop`

- `stop`: machine is killed; cold start re-loads everything (Docker layers, Go process, model weights). 5–30 s on a typical workload.
- `suspend`: machine sleeps with RAM snapshotted. Wake is 1–3 s and crucially **the model stays loaded**. The in-memory vault also survives.
- Cost difference: negligible (suspend keeps a tiny RAM-image around).

Use `suspend` unless you genuinely need cold restarts to clear leaked state.

### `min_machines_running = 0` requires WARMUP_ON_START

When a machine wakes from cold (first request after a long idle, or after Fly maintenance), the first request pays the full warmup cost. If warmup is >5 seconds, that request hits the healthcheck `grace_period` and Fly may consider it unhealthy.

Mitigation: pre-load the heavy stuff at process start, BEFORE the HTTP listener binds:

```go
if os.Getenv("WARMUP_ON_START") == "1" {
    timeout := durationFromEnv("PLATFORM_WARMUP_TIMEOUT", 5*time.Minute)
    ctx, cancel := context.WithTimeout(context.Background(), timeout)
    defer cancel()
    if _, err := engine.Analyze(ctx, "warmup probe", cfg); err != nil {
        log.Fatalf("warmup failed: %v", err)
    }
}
listener := http.Server{ ... }
```

If warmup consistently exceeds `grace_period`, **don't tune the grace_period — scale memory/CPU instead.** A grace period of 120 s silently becomes 60 s; you'll spend hours debugging healthcheck flakiness chasing a setting Fly ignores.

### Machine sizing rules of thumb

| Workload | Suggested machine |
|---|---|
| Pure pattern / regex / no ML | `shared-cpu-1x:256MB` (cheapest) |
| ML model ~200 MB int8 ONNX + lib | `shared-cpu-1x:2048MB` (1 GB for model + workspace + Go heap) |
| Mid-size LLM via Ollama (3B model) | `performance-1x:4096MB` minimum |
| Heavy throughput, latency-sensitive | `performance-2x:4096MB` (dedicated cores, not shared) |

Run the bench / a representative load on the chosen size BEFORE going to prod. `shared-cpu-1x` is bursty — under contention it stalls.

## HTTP API design for local-first

Stick to REST + JSON. Don't reach for gRPC / Connect-RPC unless you have a stub-generation reason. Skip GraphQL.

### Endpoint shape

```
POST /v1/ingest      → input goes in, transformed output comes out + tokens
POST /v1/detokenize  → tokens go in, cleartext comes out (vault lookup)
POST /v1/reveal      → tokens-in-text go in, cleartext-in-text comes out
GET  /healthz        → 200 if ready; everything else is a bug
```

Version every business endpoint as `/v1/`. Reserve `/v2/` for breaking changes; never version `/healthz` (it's infrastructure-side).

### Always include multi-tenant scoping

Every request that touches stored state needs `tenant_id`:

```json
{
  "tenant_id": "acme-corp",
  "doc_id": "incident-2026-04-17-001",
  "content": "...",
  "language": "en"
}
```

Even if you're shipping single-tenant on day 1, take `tenant_id` from the start. Retrofitting multi-tenancy after the fact is the most common rewrite for early-stage SaaS.

### Idempotency via `doc_id`

The combination `(tenant_id, doc_id)` should be enough to dedup retries. Choose a stable doc_id source (sha256 of content, or upstream system's ID).

### Content-format negotiation

Real-world callers send heterogeneous content. Accept an optional `content_format` field:

```json
{"content_format": "text"}    // default — plain prose
{"content_format": "json"}    // recurse through string leaves
{"content_format": "ndjson"}  // each line a JSON doc
{"content_format": "logs"}    // each line independent, strip ANSI, repair invalid UTF-8
```

Strip ANSI escape codes (`\x1b[...m`) before recognizers run — they otherwise produce span boundary bugs and false-positive flags on the escape sequences.

### Request-size hard cap

```go
MAX_CONTENT_BYTES = 10485760  // 10 MiB default

if int64(len(body)) > maxBytes {
    http.Error(w, "request too large", http.StatusRequestEntityTooLarge)
    return
}
```

Set this even for "internal" services — a runaway client uploads gigabytes faster than you can ssh in to kill the machine.

## In-memory state lifecycle

Local-first services often have in-memory stores (token vault, document cache, rate-limit counter). Three rules to avoid OOM:

### TTL on every store

```go
type entry struct {
    val       string
    expiresAt time.Time
}
```

Default TTL: 5 minutes. Make it env-configurable (`MEMORY_VAULT_TTL=5m`). Setting `0` means "no expiry" — only useful in tests.

### Sweep, don't reap-on-write

Run a background sweeper every 30 s that removes expired entries. Reaping on write is a latency spike under load.

### Bound the store with a hard ceiling

```go
const maxEntries = 10_000
if len(s.byKey) >= maxEntries {
    // LRU evict, or reject with 503
}
```

Without a ceiling, a misbehaving client can OOM the machine in seconds. Free-tier Fly machines have 256–1024 MB; the floor disappears fast.

## Go build matrix discipline

Build tags are how you keep heavy deps out of the default build path. Pattern:

- **Default build**: pure Go, no CGO. `go build ./...` works on any machine, produces a static binary.
- **Tagged build**: opt-in CGO + heavy native deps. `go build -tags hugot ./cmd/svc`.

```go
//go:build hugot
package recognizers
// real implementation that uses libonnxruntime
```

```go
//go:build !hugot
package recognizers
// fail-fast stub that log.Fatalf's if invoked
func DefaultAnalyzerEngineWithGLiNERConfig(_ Config) *Engine {
    log.Fatalf("not available: rebuild with -tags hugot")
    return nil
}
```

**Don't reach for a CGO-required package from outside the tag's gate**, or default builds will pull it transitively and break. `go build ./...` is your canary — it should be green on every commit, on every machine.

### CGO contagion is real

Any direct import of a CGO-only package leaks CGO requirements through the build graph. Verify with:

```bash
CGO_ENABLED=0 go build ./...    # must succeed
CGO_ENABLED=0 go build -tags <heavy-tag> ./cmd/svc  # depends on the heavy path
```

The second can legitimately fail (CGO needed for the tagged path). The first MUST pass.

## CI/CD design

GitHub Actions for OSS / small-team, with discipline:

### Path filters keep CI cheap

```yaml
on:
  pull_request:
    paths:
      - 'cmd/**'
      - 'internal/**'
      - 'analyzer/**'
      - 'go.mod'
      - '.github/workflows/**'
```

Doc-only PRs skip CI. Most reviewers want this; surprisingly many repos don't have it.

### Cache aggressively

`actions/setup-go` and `actions/setup-python` have built-in caching:

```yaml
- uses: actions/setup-go@v5
  with:
    go-version-file: 'go.mod'
    cache-dependency-path: go.sum

- uses: actions/setup-python@v5
  with:
    python-version: '3.12'
    cache: 'pip'
    cache-dependency-path: bench/requirements.txt
```

For binary deps (libonnxruntime, etc.), use `actions/cache` keyed on the version:

```yaml
- name: Cache libonnxruntime
  uses: actions/cache@v4
  with:
    path: .tokenlib
    key: libonnxruntime-${{ env.ORT_VERSION }}-linux-x64
```

### Guard rails > soft warnings

If a check matters, **fail the job**. CI annotations that say "consider fixing X" get ignored. CI red gets fixed.

Example: a bench harness silently falling back to a slower backend is undetectable from "bench succeeded but found no entities" unless you add an explicit check that asserts the heavier engine actually ran.

### Artifact upload on `always()`

```yaml
- name: Upload bench artifacts
  if: always()  # even on failure — that's when you most want them
  uses: actions/upload-artifact@v4
  with:
    name: bench-${{ github.run_id }}
    path: bench/REPORT.md
    retention-days: 14
```

Sub-question to always ask: "if this fails, what would I want to see in the artifact tab?" That's what you upload.

### Job summary > buried logs

```yaml
- name: Headline → job summary
  if: always()
  run: |
    echo "## Result" >> $GITHUB_STEP_SUMMARY
    echo "..." >> $GITHUB_STEP_SUMMARY
```

The summary is what PR reviewers see without clicking through job logs. Put the numbers there. Burying them in step output is a UX bug.

### Concurrency: cancel-in-progress

```yaml
concurrency:
  group: bench-${{ github.ref }}
  cancel-in-progress: true
```

Otherwise a busy PR queues six 15-minute jobs and burns your free-tier minutes.

## Observability discipline

Log levels and what belongs where:

| Level | What goes here |
|---|---|
| `log.Printf` (no level prefix) | Steady-state ops events: requests with status + duration, lifecycle (server listening / backend init), recoverable warnings |
| `log.Fatalf` | Boot-time fatal: misconfigured backend, missing required env var, model file unreadable. *Never* `Fatalf` from a request handler — return HTTP 500 |
| Structured fields | Tenant + doc IDs + entity counts. Don't log raw `content` even at debug — that's a PII leak from the PII tool |

### Mandatory log lines

- `server listening on :8080` — bind happened
- `backend = <name>` — what's actually configured
- Per-request line at the end of the handler: method, path, status, duration_ms, tenant, doc — one line per request, never multi-line
- Errors surfaced from sub-components, even when recovered, with enough context to grep (`component=X error=Y`)

### Silent-error rule

If your code catches an error and continues, **log it once at the boundary**. The single most expensive class of bugs in services this shape is "the optional backend silently failed and the request returned a degraded response with no log line". Make degradation visible.

## Secrets discipline

- **Env vars only.** Never commit a key, even to a private repo. `.env` files go in `.gitignore` from the start.
- **`fly secrets set` for cloud.** Not in `fly.toml`'s `[env]` block. The toml is committed; secrets aren't.
- **Validate at boot, not first use.** If `STRIPE_KEY` is required, check at startup and `log.Fatalf` if missing. A request handler discovering it's missing 6 hours into prod is the worst time.

## Cold-start vs steady-state

For ML-backed services on auto-stop machines, the cold start is the load-bearing UX issue:

1. **Bake heavy artefacts into the image** (model weights, libraries). Do NOT lazy-download at first request — first user pays the network round trip.
2. **`WARMUP_ON_START`** runs one trivial inference before binding the listener — error visible in machine logs, not as a stuck request.
3. **Suspend > stop** keeps the model resident across idle.
4. **Healthcheck `grace_period: 60s`** — Fly clamps higher values silently.

For non-ML services, just turn `WARMUP_ON_START` off — the boot cost is dominated by Go runtime init (a few ms) and you're shipping a static binary anyway.

## What NOT to do

- **Don't write a custom service mesh.** You're a local-first service. Stay flat. Two HTTP services that need to talk = `http.Post`.
- **Don't add a database tier "for safety"** before you have evidence in-memory + TTL isn't enough. Persistence is the most expensive long-term commitment in the system.
- **Don't put secret rotation logic in v1.** Manual rotation is fine for the first year. Automated rotation belongs after you have actual scale.
- **Don't add OpenTelemetry / Prometheus exporters on day 1.** `log.Printf` + `fly logs` does 80% of what most early-stage observability needs. Add metrics when you have a specific question you can't answer from logs (latency-distribution alerting, error rate over time).
- **Don't ship a multi-Dockerfile setup without a clear reason.** Two Dockerfiles == two test surfaces. Justify each.
- **Don't introduce a Python sidecar to make some part "easier" unless it's a one-shot bench/dev tool.** A second runtime is a 5x increase in deployment surface area.
- **Don't add `auto_stop_machines = "stop"` if your warmup is >2 s.** Use `suspend`.

## Pair with project-specific skills

Skills like `anonde` cite specific file paths, deploy commands, Fly app names, and current production state. This skill stays evergreen — nothing here is timestamp-bound. When in doubt: concept-level lives here, project-level lives in the per-repo skill.
