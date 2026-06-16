# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

It carries the working assumptions and cross-session findings that future
sessions need in order to be useful from the first message, without
re-deriving them. Keep it an index — push detail into
`.claude/memory/` and `docs/`.

## Project

anonde is a **local-first PII anonymize / de-anonymize toolkit**,
positioned as a competitor to Microsoft Presidio. **Distribution is
OSS + self-hosted**: users `go get` the library or `docker pull` the
image and run it on their own host. We do not run a hosted service —
the public demo URL is our reference deploy for benchmarks and live
examples, not a product. Every default has to be safe on a stranger's
laptop or single small VM.

German is first-class; English / multilingual is second-class but
supported through the same recognizers + GLiNER NER. Runtime is Go-only
by constraint — Python is acceptable for benchmarks and dev tooling but
not in the production hot path. The repo ships two image variants —
patterns-only (~12 MB) and NER (~770 MB, GLiNER FP32 + libonnxruntime
baked in; ~530 MB with `GLINER_QUANT=int8`) — that run anywhere Docker
does.

## Persistent memory

Indexed in [`.claude/memory/MEMORY.md`](.claude/memory/MEMORY.md). Read it
at the start of any non-trivial task. Each entry is one focused file in
`.claude/memory/`; treat the index like a table of contents.

When you learn something cross-session-worth-knowing (a product decision,
a bench result, a non-obvious operational fact), add a new file under
`.claude/memory/` and update `MEMORY.md` with a one-line pointer. Do not
inline content into this file — keep `CLAUDE.md` an index, not a journal.

What belongs in memory: project decisions, bench numbers, deployment
quirks, model selections, integration gotchas. What does NOT: code
patterns derivable from the codebase, recent git history, ephemeral task
state.

## Production deployment shape

- Two image variants ship from the repo:
  - `Dockerfile.anonde` → patterns-only (~12 MB image)
  - `Dockerfile.anonde-ner` → GLiNER PII baked in (~770 MB image,
    CGO_ENABLED=1, distroless/cc-debian12, bundled
    libonnxruntime.so.1.26.0)
- The NER variant runs `ANALYZER_BACKEND=gliner` with the English-base
  model `knowledgator/gliner-pii-base-v1.0` at threshold 0.40, loaded
  from the **FP32** ONNX (`onnx/model.onnx`) by default — the bench
  matrix proved INT8 leaks ~6pp more PII overall (Σ ALL 20.7% FP32 vs
  26.6% INT8). Memory-constrained deployments can flip back with
  `GLINER_QUANT=int8` (saves ~240 MB image at the cost of recall on
  multilingual legal / clinical text). See
  [`bench_findings_2026_05_13.md`](.claude/memory/bench_findings_2026_05_13.md)
  for the model-selection reasoning (multilingual variant rejected for
  score-calibration brittleness).
- Per-request `disable_ner: true` falls back to patterns-only when NER
  latency isn't wanted.

## Build tags

- Default build: pure Go, no NER, no CGO. Production-safe everywhere.
- `-tags ner`: enables the in-process GLiNER recognizers
  (GLiNERRecognizer + flat / stack / ensemble variants). Used by
  `Dockerfile.anonde-ner`.
- For the GLiNER path, CGO is required AND libonnxruntime.so must be
  reachable at runtime via `ORT_SO_PATH` (set by the Dockerfile).

## Common commands

Day-to-day dev targets live in the top-level [`Makefile`](Makefile);
`make help` lists every one with its description. Highlights:

- `make build` / `make build-ner` — default Go build / `-tags ner` build.
- `make test` — full test suite. `make test-api` for `internal/api/...` only.
  Run a single test with `go test ./internal/api/ -run TestName`.
- `make ci` — what CI runs (`go vet ./...` + `go test ./...`).
- `make proto` — regenerate `gen/` after editing anything under `proto/`.
  `proto/anonde/v1/anonde.proto` is the single source of truth; never
  edit `gen/` by hand. `make tools` installs the protoc-gen-* plugins.
- `make run` / `make run-ner` — start the server on `:8081` locally.
- `make docker-build` / `make docker-run` + `make smoke` — round-trip
  ingest → reveal → delete against the running container.

The bench harness is a separate world; see "Bench harness" below.

## Code layout

The full pipeline diagram + the non-obvious conflict-resolution rule
(NER beats patterns for PERSON/ORG/LOC/AGE/PROFESSION/NRP regardless of
score) is in [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md). Key entry
points:

- [`cmd/anonde/main.go`](cmd/anonde/main.go) — server bootstrap: wires
  analyzer + anonymizer + vault + store via env (`ANALYZER_BACKEND`,
  `WARMUP_ON_START`, `DOWNLOAD_MODELS_ONLY`, TTLs).
- [`internal/core/service.go`](internal/core/service.go) — transport-agnostic
  orchestration of analyze → anonymize → store. Both the gRPC and REST
  handlers call into this one service.
- [`internal/api/`](internal/api/) — three transports on one port: REST
  (grpc-gateway), Connect (Connect/JSON, Connect/Proto, gRPC-Web), and
  native gRPC. Wire JSON is snake_case but camelCase inputs are also
  accepted.
- [`analyzer/`](analyzer/) — recognizer registry + parallel dispatch +
  `RemoveConflicts`. The 70 pattern recognizers live in
  `analyzer/recognizers/`; the `-tags ner` GLiNER recognizers are
  `ner_gliner.go` / `ner_gliner_flat.go` (+ pool / ensemble variants) in
  the same directory.
- [`anonymizer/`](anonymizer/) — operators (Replace, Redact, Mask, Hash,
  Encrypt, Synthesize) and the adjacent-span merge.
- [`internal/store/`](internal/store/) — in-memory + bbolt vault/store
  backends behind one interface.

## Named subagents (on-demand by lane)

Three named subagents live under [`.claude/agents/`](.claude/agents/),
each pre-loaded for one lane. Dispatch by name via the `Agent` tool
when a task naturally lands in a lane; run several in parallel only
when work genuinely fans out across lanes (e.g. a PR review touching
recognizer + Dockerfile + API).

- **`hex`** — PII detection / NER / bench correctness. Pairs with the
  `pii-engineer` skill (concepts) + `anonde` (project paths). Dispatch
  for recognizer logic, NER backends, conflict resolution, leak-rate
  bench, score thresholds, silent-fallback bugs.
- **`patch`** — OSS / self-hosting / release engineering. Pairs with
  `oss-engineer` (concepts) + `anonde` (project paths). Dispatch for
  Dockerfiles, build tags, CI workflows, releases, contributor docs,
  env-var surface. Respects the `avoid_fly_mentions` standing memory.
- **`vault`** — anonde codebase + transport. Pairs with `anonde`
  primarily, `pii-engineer` / `oss-engineer` as supporting context.
  Dispatch for HTTP API surface, `internal/core/service.go`
  orchestration, anonymizer operators, store backends, "where is X?"
  questions.

Not every task needs a subagent — trivial edits stay on the main
agent. When in doubt, the lane-decision rule is "if I'd load the
corresponding skill, dispatch to its agent instead so the skill
context lives in a subagent window, not the main one."

## Bench harness

Everything lives under `bench/`. The top-level entry is `bench/Makefile`:

  - `make -C bench help` lists every target.
  - `make -C bench matrix` runs **every engine × every corpus** and renders
    one combined `bench/REPORT_MATRIX.md`. Engines: anonde-patterns,
    anonde-ner (default NER image), anonde-ner-stack (premium image),
    presidio, gliner-py, openai-pf.
    Corpora: openmed + ggponc_de + pmc_de + synth_clinical + wiki_de
    (German) and ai4privacy_en (English).
  - `make -C bench matrix-de` / `matrix-en` for language subsets.
  - `make -C bench/corpora/<NAME> all` runs one corpus directly; per-corpus
    Makefiles accept `ANONDE_BACKEND={patterns-only|gliner}`,
    `ANONDE_MODEL`, `ANONDE_ONNX_FILE`, `LABEL_SET`, etc.

The Go bench runner is `bench/runners/anonde.go` (single source of truth).
Python sidecars for non-Go backends: `bench/runners/{presidio,gliner,
openai_pf}.py`. Shared scoring: `bench/scoring/{compare.py,render_matrix.py,
merge_findings.py,label_map.yaml}`. Diagnostic probes (not in the matrix):
`bench/probes/{hugot,gliner,diff_gliner}/`.
