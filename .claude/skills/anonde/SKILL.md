---
name: anonde
description: |
  Load anonde project context. Invoke when working on the anonde codebase — any task touching the analyzer, anonymizer, NER backends, bench matrix, recognizer additions, deployment to Fly.io, or the CI bench workflow. Carries production-state facts, common-gotcha canaries, file-path map, and short runbooks for bench / deploy / debug.
allowed-tools: Read, Bash, Edit, Write, Grep, Glob
---

# anonde — load-bearing project context

> Single-source reference distilled from the May 2026 GLiNER deployment + bench-consolidation work. When this is loaded, do NOT re-derive anything below from the codebase — it's been verified empirically. Re-verify only if claims here contradict what you observe.

## What anonde is

Local-first PII detection + reversible anonymization toolkit in Go. Competitor to Microsoft Presidio. **German clinical text is first-class**; English + 5 other languages secondary. Production runtime is Go-only (Python is acceptable for bench/dev tooling, never in the hot path).

Deployment target: Fly.io machine `anonde-platform` in `iad`, accessible at `https://anonde-platform.fly.dev`.

## Production stack (don't re-derive)

| Component | What ships |
|---|---|
| **Image** | `Dockerfile.platform-ner` — `distroless/cc-debian12` base, ~470 MB, CGO=1, libonnxruntime 1.26.0 bundled at `/usr/lib/x86_64-linux-gnu/libonnxruntime.so.1`, GLiNER model baked into `/models/` |
| **Backend** | `ANALYZER_BACKEND=gliner`, model `knowledgator/gliner-pii-base-v1.0`, ONNX file `onnx/model_quint8.onnx`, threshold 0.40 |
| **Build tag** | `-tags hugot` (historical name — enables both Hugot AND GLiNER recognizers) |
| **Default build** | Pure Go, no CGO, patterns-only — `Dockerfile.platform` (~12 MB) |

`fly.toml` deploys patterns-only; `fly.ner.toml` deploys NER. Same Fly app, two configs.

## Gotchas that bit us — check these first when debugging

When a bench cell or production deploy "works" but feels wrong, walk this list in order:

1. **`gliner: INIT FAILED` log line absent?** Check the bench runner / platform logs for `analyzer: recognizer error (swallowed)` in `analyzer/analyzer.go:215`. That's the canary added specifically because GLiNER errors are otherwise invisible (the engine continues with patterns-only output).
2. **Non-round sigmoid scores absent?** Run `python3 -c "import json; print(set(round(f['score']*20)%20 for d in (json.loads(l) for l in open(PATH)) for f in d['findings'] if 0.3<f['score']<0.99))"` — if it's `{0}` (all multiples of 0.05), GLiNER didn't fire. The output is all from pattern recognizers.
3. **`--ort-library` not passed to runner?** GLiNER cells in `bench/Makefile` MUST pass `--ort-library "$(ORT_LIB)"`. Default Makefile var: `ORT_LIB ?= $(ROOT)/.tokenlib/libonnxruntime.dylib` (Mac) / override to `.so` on Linux/CI. Without this, GLiNER silently falls back to patterns.
4. **GLiNER ONNX file unspecified?** `knowledgator/gliner-pii-base-v1.0` ships THREE ONNX variants (`onnx/model.onnx`, `onnx/model_fp16.onnx`, `onnx/model_quint8.onnx`). The downloader refuses to pick. Always set `GLINER_ONNX_FILE=onnx/model_quint8.onnx` (Makefile default) or pass via `--onnx-file`.
5. **Distroless base wrong?** `distroless/static-debian12` (default for Go) lacks libc — incompatible with libonnxruntime's dlopen. Use `distroless/cc-debian12` for the NER variant.
6. **Python sidecar shadows package?** `bench/runners/gliner_sidecar.py` (NOT `gliner.py`!) — the file name would otherwise shadow the installed `gliner` package on the script-dir-first import path.
7. **Anonymizer "no token mapped" error?** `mergeAdjacentSameType` in `anonymizer/anonymizer.go` collapses adjacent same-type spans AFTER the service registers tokens. Pre-merge in `internal/platform/service.go` via the exported `anonymizer.MergeAdjacentSameType`. Done in commit `cb14341`-ish.

## Where to look (file-path map)

| Concern | File |
|---|---|
| Default analyzer + recognizer registration | `anonde.go` (`patternRecognizers()` returns 52 recognizers) |
| Patterns-only constructor | `anonde.go::DefaultAnalyzerEngine` |
| Hugot/GLiNER constructors | `hugot_on.go` (real) / `hugot_off.go` (fail-fast stub) — `-tags hugot` gate |
| The Go-native GLiNER recognizer | `analyzer/recognizers/ner_gliner.go` (`-tags hugot`) |
| GLiNER config struct | `analyzer/recognizers/gliner_config.go` (no build tag — visible from stub) |
| GLiNER labels + label→canonical map | `gliner_config.go::DefaultPIILabels` / `DefaultLabelToEntity` |
| Conflict resolver (NER preference) | `analyzer/result.go::shouldReplace` (the NER-preferred entity set is hardcoded there) |
| Swallowed-recognizer-error canary log | `analyzer/analyzer.go` (around line 215) |
| Platform HTTP service | `cmd/platform/main.go` + `internal/platform/{service,http,memory}.go` |
| Bench harness root | `bench/` — top-level Makefile + corpora/runners/probes/scoring/microbench/ |
| Bench runner (Go) | `bench/runners/anonde.go` (used by both patterns-only and gliner cells) |
| Python sidecar (GLiNER) | `bench/runners/gliner_sidecar.py` |
| Python sidecar (Presidio) | `bench/runners/presidio.py` |
| Diagnostic probes | `bench/probes/{hugot,gliner,diff_gliner}/` |
| Combined report renderer | `bench/scoring/render_matrix.py` (human-friendly format) |
| CI workflow | `.github/workflows/bench.yml` |

## Bench harness — how to drive it

```bash
# Full matrix (5 engines × all gold corpora) — typically 1-3 hours wall clock
make -C bench matrix

# Subsets
make -C bench matrix-de              # German corpora only
make -C bench matrix-en              # English (ai4privacy_en) only
make -C bench corpus-openmed         # one corpus, all engines

# Specific cell (cheapest to iterate)
make -C bench bench/corpora/openmed/data/anonde_anonde-gliner.jsonl

# Override knobs
make -C bench corpus-openmed \
  GLINER_THRESHOLD=0.30 \
  GLINER_MODEL=onnx-community/gliner_multi_pii-v1 \
  GLINER_ONNX_FILE=onnx/model_quantized.onnx \
  ORT_LIB=.tokenlib/libonnxruntime.dylib

# Render the matrix from existing per-cell JSONLs (no re-run)
python3 bench/scoring/render_matrix.py \
  --corpora-root bench/corpora \
  --corpus openmed --corpus synth_clinical --corpus ai4privacy_en \
  --corpus finance_de --corpus legal_de --corpus wikiann_de \
  --corpus pmc_de --corpus wiki_de \
  --engine anonde-patterns --engine anonde-gliner --engine gliner-py \
  --engine openai-pf --engine presidio \
  --label-map bench/scoring/label_map.yaml \
  --out bench/REPORT_MATRIX.md --csv bench/results_matrix.csv
```

Cells are cached (output exists → skip). Force a re-run: `rm bench/corpora/<c>/data/anonde_<engine>.jsonl` then `make`.

## Corpora — what each one is for

| Corpus | Domain | Has gold? | Use for |
|---|---|---|---|
| `openmed` | GraSCCo PHI (synthetic DE clinical) | ✓ expert | Hero corpus; the canonical anonde benchmark |
| `synth_clinical` | Anonde's own slot-based DE clinical | ✓ slot | Regression detection (we own the generator) |
| `finance_de` | Synthetic DE financial (slot-based) | ✓ slot | Domain generalization — financial PII |
| `legal_de` | Synthetic DE legal (slot-based) | ✓ slot | Domain generalization — legal PII |
| `wikiann_de` | Real DE Wikipedia (PER/LOC/ORG) | ✓ expert | **Precision probe** — real natural prose |
| `ai4privacy_en` | Synthetic EN mixed (5000 docs) | ✓ | English head-to-head vs Presidio + OpenAI PF |
| `pmc_de` | Real DE PubMed case reports | ✗ | Over-flag probe (no F1) |
| `wiki_de` | Real DE medical Wikipedia | ✗ | Over-flag probe (no F1) |
| `ggponc_de` | DE oncology guidelines | ✗ (DUA) | Manual setup only — excluded from auto-matrix |

## Engines in the matrix

| Engine | What it is | Role |
|---|---|---|
| `anonde-patterns` | 52 regex + checksum recognizers, no NER | Floor / no-NER baseline |
| `anonde-gliner` | patterns + GLiNER (production!) | The thing we ship |
| `anonde-hugot` | patterns + Hugot/XLM-R PII (legacy) | Regression detection; excluded from CI subset (slow on CPU) |
| `gliner-py` | GLiNER via Python `gliner` package | Cross-runtime parity check on our Go wrap |
| `presidio` | Microsoft Presidio (spaCy `en_core_web_lg`) | External baseline competitor (EN only) |
| `openai-pf` | OpenAI Privacy Filter (April 2026) | Commercial-vendor baseline; catastrophic on German, slow (excluded from CI) |

## Bench numbers worth knowing (2026-05-13 matrix)

anonde-gliner leak rate vs best baseline, per corpus:

| Corpus | patterns | **gliner** | Δ |
|---|---:|---:|---:|
| openmed (DE clinical) | 22.8% | **17.2%** | ↓5.7pp |
| synth_clinical (DE clinical) | 11.8% | **11.1%** | ↓0.6pp |
| ai4privacy_en (EN mixed) | 55.4% | **25.0%** | ↓30.4pp (beats Presidio 28.4%) |
| finance_de (DE financial) | 18.1% | **9.8%** | ↓8.3pp |
| legal_de (DE legal) | 9.4% | **6.9%** | ↓2.5pp |
| wikiann_de (DE Wikipedia) | P=0.94, R=0.62 | **P=0.86, R=0.87, F1=0.86** | — |

**anonde-gliner wins on every gold corpus by leak rate.** Strict F1 drops slightly on openmed because GLiNER spans are wider than gold (Müller vs Herr Müller); leak rate captures the real-world recall correctly.

## Deployment runbook

```bash
# 1. Build + verify locally
go build ./... && go build -tags hugot ./...
go test ./analyzer/... ./anonymizer/... ./internal/...

# 2. Local Docker smoke (slow on Apple Silicon via Rosetta)
docker build -f Dockerfile.platform-ner -t anonde-platform-ner:test .
docker run --rm -d --name anonde-test -e WARMUP_ON_START=1 -p 18080:8080 anonde-platform-ner:test
sleep 8 && curl -sS http://localhost:18080/healthz
docker rm -f anonde-test

# 3. Deploy to Fly
fly deploy --config fly.ner.toml -a anonde-platform

# 4. Wake the machine
curl -sS https://anonde-platform.fly.dev/healthz

# 5. End-to-end smoke (German clinical PHI)
curl -sS -X POST https://anonde-platform.fly.dev/v1/ingest \
  -H "Content-Type: application/json" \
  -d '{"tenant_id":"smoke","doc_id":"t1","content":"Patient Herr Müller, geboren 14.03.1962, Hauptstr. 8, 10115 Berlin, Tel 030-12345678","language":"de"}'

# 6. Confirm GLiNER actually fired (the silent-fallback failure mode)
fly logs -a anonde-platform | grep "gliner:" | head -5
# Want: "gliner: ready ..." + per-request "gliner: analyze ... raw_candidates=N" with N>0
```

## CI workflow shape

`.github/workflows/bench.yml`:
- Triggers: push to `main`, PRs, manual `workflow_dispatch` (no inputs configured)
- Path filter: only fires on bench-relevant code changes
- Steps: `go build` (both tags) → unit tests → `make corpus-openmed && make corpus-synth_clinical` (3 engines each) → `render_matrix.py` → guard-rail check (fails if 0 NER-attributable findings) → upload artifacts
- Wall clock: ~10-15 min warm-cache, ~20 min cold

The guard rail is the line that catches silent regressions — preserve it through any CI refactor.

## How to add a new recognizer

```go
// File: analyzer/recognizers/my_pattern.go
type MyRecognizer struct{}
func (r *MyRecognizer) Name() string                 { return "MyRecognizer" }
func (r *MyRecognizer) SupportedEntities() []string  { return []string{"MY_ENTITY"} }
func (r *MyRecognizer) SupportedLanguages() []string { return []string{"de", "en", "*"} }
func (r *MyRecognizer) Analyze(...) (...) { ... }
```

Then register in `anonde.go::patternRecognizers()` in the right region block. If the recognizer relies on a model, name it `*NERRecognizer` (suffix matters — controls `DisableNER` filtering + the capitalised-word heuristic in `analyzer/analyzer.go`). For NER-emitted unstructured types (PERSON/ORG/LOC/AGE/PROFESSION/NRP), update `nerRecognizerNames` in `analyzer/result.go` so the conflict resolver prefers it over heuristic patterns.

Add a label_map entry in `bench/scoring/label_map.yaml` under `anonde:` if the canonical type differs from the entity-type string the recognizer emits.

## How to add a new bench corpus

Mirror `bench/corpora/synth_clinical/`:
- `Makefile` (data/anonde/report/clean targets — copy the synth_clinical template)
- `README.md` (provenance, coverage, license, run recipe)
- `loader.py` or `generate.py` (emits `data/corpus.jsonl` with `{id, text, entities}`)
- `data/.gitkeep`

Add the corpus name to `DE_CORPORA` or `EN_CORPORA` in `bench/Makefile`. Run `make data && make corpus-NAME` to validate.

`data/` is gitignored — corpora regenerate from upstream (Zenodo/HF/in-process generator).

## What NOT to do

- **Don't introduce a CGO dep** in code that compiles under the default (no-tags) build path. The `Dockerfile.platform` image is `CGO_ENABLED=0`.
- **Don't reach for `yalue/onnxruntime_go` from outside `-tags hugot` paths** — it's CGO-only, breaks the default build.
- **Don't add `.PHONY` to bench cell targets** — they're file-targets specifically so the cache works. Use order-only deps (`| data-$(1)`) to wire data fetch without invalidating cells.
- **Don't bake Presidio + spaCy into `bench/requirements.txt`** — those are the Presidio cell's deps, ~700 MB; keep them out of CI to keep wall-clock down. Install locally only:
  ```bash
  pip install presidio-analyzer spacy && python -m spacy download en_core_web_lg
  ```
- **Don't commit anything under `bench/corpora/*/data/`** — gitignored; corpora regenerate.
- **Don't claim a bench result** without checking the per-corpus `gliner: analyze ... raw_candidates=N` log shows N>0 across the corpus. Silent fallback to patterns IS the failure mode this skill exists to prevent.

## Memory files for cross-session context

The `.claude/memory/` dir holds the deeper history:

- `MEMORY.md` — index (one line per memory)
- `project_goal.md` — what anonde is and competes with
- `bench_findings_2026_05_13.md` — full bench-session history including the silent-fallback debug saga

These are gitignored (per-machine) but synced from Claude's global memory via `.claude/sync_memory.sh`. Read them at the start of any non-trivial task.

---
*Skill version 1 — codifying the May 2026 GLiNER deployment + bench-consolidation knowledge. Update this file when production state diverges from the production-stack table above.*
