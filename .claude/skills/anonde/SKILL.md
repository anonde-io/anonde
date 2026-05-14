---
name: anonde
description: |
  Apply when working on the anonde codebase (PII detection + anonymization, German-first, local-first, Fly.io deployed). Loads project-specific facts: production stack, file-path map, current bench snapshot, deploy + CI runbooks. The conceptual / pattern-level material lives in the `pii-engineer` skill — invoke both together when designing or debugging.
allowed-tools: Read, Bash, Edit, Write, Grep, Glob
---

# anonde — repo-specific reference

> Concepts and general PII-engineering patterns live in the **`pii-engineer`** skill. This skill carries only the facts that are anonde-specific and will rot if the codebase changes (file paths, commit hashes, deploy URLs, bench numbers).

## Production stack (verified 2026-05-13)

| | What ships |
|---|---|
| **Public URL** | `https://anonde-platform.fly.dev` |
| **Fly app** | `anonde-platform` in `iad` |
| **Image** | `Dockerfile.anonde-ner` — `distroless/cc-debian12` base, ~470 MB, CGO=1, libonnxruntime 1.26.0 at `/usr/lib/x86_64-linux-gnu/libonnxruntime.so.1`, GLiNER model baked into `/models/` |
| **Backend** | `ANALYZER_BACKEND=gliner`, model `knowledgator/gliner-pii-base-v1.0`, ONNX `onnx/model_quint8.onnx`, threshold `0.40` |
| **Build tag** | `-tags hugot` (historical name — enables both Hugot AND GLiNER recognizers) |
| **Default build** | `Dockerfile.anonde` — pure Go, no CGO, patterns-only (~12 MB) |
| **Fly config (NER)** | `fly.ner.toml` |
| **Fly config (patterns)** | `fly.toml` |

## File-path map

| Concern | File |
|---|---|
| Default analyzer + 52-recognizer registry | `anonde.go::patternRecognizers()` |
| Hugot/GLiNER constructors | `hugot_on.go` (real) / `hugot_off.go` (stub) |
| Go-native GLiNER recognizer | `analyzer/recognizers/ner_gliner.go` (`-tags hugot`) |
| GLiNER config struct | `analyzer/recognizers/gliner_config.go` |
| GLiNER labels + label→canonical | `gliner_config.go::DefaultPIILabels` / `DefaultLabelToEntity` |
| Conflict resolver (NER preference) | `analyzer/result.go::shouldReplace` — see `pii-engineer` for the rationale |
| Swallowed-error canary log | `analyzer/analyzer.go` (~line 215) — see `pii-engineer` for why this is load-bearing |
| Adjacent-merge in anonymizer | `anonymizer/anonymizer.go::MergeAdjacentSameType` (exported because core.Service pre-merges) |
| HTTP transport | `cmd/anonde/main.go` + `internal/api/{http,connect_server,grpc_server,proto_logic}.go` + `internal/core/{service,types}.go` + `internal/store/memory.go` |
| Bench harness root | `bench/` — Makefile + corpora/runners/probes/scoring/microbench/ |
| Bench Go runner | `bench/runners/anonde.go` |
| Python sidecars | `bench/runners/{gliner_sidecar,presidio,openai_pf}.py` |
| Diagnostic probes | `bench/probes/{hugot,gliner,diff_gliner}/` |
| Report renderer | `bench/scoring/render_matrix.py` |
| CI workflow | `.github/workflows/bench.yml` |

## Repo-specific gotchas (ordered by likelihood)

These are the silent-fallback-class bugs we've hit on THIS codebase. Concept-level discussion is in `pii-engineer`; this list is "where to look first" when debugging.

1. **`gliner: INIT FAILED` absent from logs** → check `analyzer/analyzer.go` for `analyzer: recognizer error (swallowed)` lines. That's anonde's silent-fallback canary.
2. **`--ort-library` not passed by the bench Makefile** → cells fall back to patterns. Default `ORT_LIB ?= $(ROOT)/.tokenlib/libonnxruntime.dylib` in `bench/Makefile`; CI overrides to the `.so` path via env var.
3. **GLiNER ONNX file ambiguity** → `knowledgator/gliner-pii-base-v1.0` ships 3 ONNX variants. Always set `GLINER_ONNX_FILE=onnx/model_quint8.onnx` (Makefile default).
4. **Python sidecar name collision** → file is `bench/runners/gliner_sidecar.py` NOT `gliner.py`, to avoid shadowing the installed `gliner` package.
5. **distroless base mismatch** → use `distroless/cc-debian12` (has glibc) for the NER image; `distroless/static-debian12` lacks the dynamic loader.
6. **Conflict resolver NER preference set** → `nerPreferredEntities` in `analyzer/result.go`. Modifying this set will visibly shift leak rate on the bench.
7. **Anonymizer "no token mapped" error** → fixed by pre-merging in `internal/core/service.go` via `anonymizer.MergeAdjacentSameType`.

## Bench numbers — 2026-05-13 snapshot

Anonde-gliner leak rate vs best baseline, per corpus:

| Corpus | Domain | patterns | **gliner** | Δ |
|---|---|---:|---:|---:|
| openmed | DE clinical (GraSCCo) | 22.8% | **17.2%** | ↓5.7pp |
| synth_clinical | DE clinical (slot-gen) | 11.8% | **11.1%** | ↓0.6pp |
| ai4privacy_en | EN mixed (5000 docs) | 55.4% | **25.0%** | ↓30.4pp (beats Presidio 28.4%) |
| finance_de | DE financial (slot-gen) | 18.1% | **9.8%** | ↓8.3pp |
| legal_de | DE legal (slot-gen) | 9.4% | **6.9%** | ↓2.5pp |
| wikiann_de | DE Wikipedia (real) | P=0.94 R=0.62 | **P=0.86 R=0.87 F1=0.86** | — |

wikiann_de is the precision-probe-on-real-text result; the other 5 are leak rate on synthetic / annotated gold. **anonde-gliner wins leak rate on every gold corpus.** Use these as anchors when validating a future change — a 5pp regression on openmed leak is a meaningful red flag.

## Corpora — quick reference

| Corpus | Has gold? | Source |
|---|---|---|
| `openmed` | ✓ expert | GraSCCo PHI from Zenodo, auto-download |
| `synth_clinical` | ✓ slot | Anonde's own generator |
| `finance_de` | ✓ slot | Anonde's own generator (Kontoauszug / Überweisung / Kreditantrag / Depot / KYC) |
| `legal_de` | ✓ slot | Anonde's own generator (Klageschrift / Beschluss / Vergleich / Vollmacht / Anwaltsschreiben) |
| `wikiann_de` | ✓ expert | WikiAnn/de from HF Datasets, streamed |
| `ai4privacy_en` | ✓ | ai4privacy/pii-masking-43k from HF, 5000-doc sample |
| `pmc_de` | ✗ | PubMed Central API; precision probe only |
| `wiki_de` | ✗ | German Wikipedia MediaWiki API; precision probe only |
| `ggponc_de` | requires DUA | German oncology guidelines; manual setup, excluded from auto-matrix |

## Bench recipes (anonde-specific)

```bash
# Full matrix — 1-3 hours wall-clock
make -C bench matrix

# Subsets
make -C bench matrix-de              # German only
make -C bench matrix-en              # English (ai4privacy_en) only
make -C bench corpus-openmed         # one corpus, all engines (cached cells skipped)

# Specific cell (cheapest)
make -C bench bench/corpora/openmed/data/anonde_anonde-gliner.jsonl

# Override the GLiNER model / threshold / ONNX file
make -C bench corpus-openmed \
  GLINER_THRESHOLD=0.30 \
  GLINER_MODEL=onnx-community/gliner_multi_pii-v1 \
  GLINER_ONNX_FILE=onnx/model_quantized.onnx \
  ORT_LIB=.tokenlib/libonnxruntime.dylib

# Render from existing per-cell JSONLs (no re-run)
python3 bench/scoring/render_matrix.py \
  --corpora-root bench/corpora \
  --corpus openmed --corpus synth_clinical --corpus ai4privacy_en \
  --corpus finance_de --corpus legal_de --corpus wikiann_de \
  --corpus pmc_de --corpus wiki_de \
  --engine anonde-patterns --engine anonde-gliner --engine gliner-py \
  --engine openai-pf --engine presidio \
  --label-map bench/scoring/label_map.yaml \
  --out bench/REPORT_MATRIX.md --csv bench/results_matrix.csv

# Force a single cell to re-run
rm bench/corpora/<c>/data/anonde_<engine>.jsonl && make -C bench corpus-<c>
```

## Deployment runbook (anonde-specific)

```bash
# 1. Build + verify locally
go build ./... && go build -tags hugot ./...
go test ./analyzer/... ./anonymizer/... ./internal/...

# 2. Local Docker smoke (slower on Apple Silicon via Rosetta)
docker build -f Dockerfile.anonde-ner -t anonde-ner:test .
docker run --rm -d --name anonde-test -e WARMUP_ON_START=1 -p 18080:8080 anonde-ner:test
sleep 8 && curl -sS http://localhost:18080/healthz
docker rm -f anonde-test

# 3. Deploy to Fly
fly deploy --config fly.ner.toml -a anonde-platform

# 4. Wake the machine (auto-suspend on idle)
curl -sS https://anonde-platform.fly.dev/healthz

# 5. End-to-end smoke
curl -sS -X POST https://anonde-platform.fly.dev/v1/anonymizations \
  -H "Content-Type: application/json" \
  -d '{"tenant_id":"smoke","doc_id":"t1","content":"Patient Herr Müller, geboren 14.03.1962, Hauptstr. 8, 10115 Berlin, Tel 030-12345678","language":"de"}'

# 6. Verify GLiNER actually fired
fly logs -a anonde-platform | grep "gliner:" | head -5
# Want: "gliner: ready ..." + "gliner: analyze ... raw_candidates=N" with N>0
```

## CI workflow (anonde-specific)

`.github/workflows/bench.yml`:

- Triggers: push to `main`, PRs, manual `workflow_dispatch` (no inputs)
- Path filter: bench-relevant code only
- Default subset: `make corpus-openmed && make corpus-synth_clinical` (3 engines)
- Wall-clock: ~10-15 min warm, ~20 min cold
- Guard rail: fails if 0 NER-attributable findings (the silent-fallback canary — see `pii-engineer` for the test)

The full DE_CORPORA list now includes `finance_de`, `legal_de`, `wikiann_de` but CI only runs the canonical two for speed. Trigger the full matrix manually via `gh workflow run bench.yml` if you want everything.

## How to add a recognizer (anonde-specific paths)

1. Create `analyzer/recognizers/my_pattern.go` with the `EntityRecognizer` interface (see `pii-engineer` for the pattern).
2. Register in `anonde.go::patternRecognizers()` in the appropriate region block.
3. If it's NER (model-backed), name suffix `NERRecognizer` is mandatory + add to `nerRecognizerNames` in `analyzer/result.go` if it emits NER-preferred types.
4. If the recognizer's entity type differs from anonde canonical, add a mapping to `bench/scoring/label_map.yaml::anonde:`.
5. Run `go test ./analyzer/recognizers/...` + at least one bench corpus to confirm no regression.

## Memory + cross-session context

`.claude/memory/` (gitignored) carries the deeper history:

- `MEMORY.md` — index
- `project_goal.md` — what anonde is + competes with
- `bench_findings_2026_05_13.md` — debug history (silent-fallback saga, multilingual variant rejection, etc.)

Sync from Claude's global memory cache via `.claude/sync_memory.sh`. Read at the start of any non-trivial task.

---
*This skill carries facts that rot. Update the bench-numbers table and file-path map whenever production diverges. Concepts go in `pii-engineer` and don't need rotation.*
