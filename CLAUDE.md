# Claude context for the anonde repo

This file is auto-loaded by Claude Code at session start. It carries the
working assumptions and cross-session findings that future sessions need
in order to be useful from the first message, without re-deriving them.

## Project

anonde is a **local-first PII anonymize / de-anonymize toolkit**,
positioned as a competitor to Microsoft Presidio. German is first-class;
English / multilingual is second-class but supported through the same
recognizers + GLiNER NER. Runtime is Go-only by constraint — Python is
acceptable for benchmarks and dev tooling but not in the production hot
path. Deployment target is Fly.io machines (see `fly.toml` for the
patterns-only variant, `fly.ner.toml` for the NER variant).

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

- Two Fly variants share the `anonde-platform` app in `iad`:
  - `fly.toml` → `Dockerfile.platform` → patterns-only (~12 MB image)
  - `fly.ner.toml` → `Dockerfile.platform-ner` → GLiNER PII baked in
    (~470 MB image, CGO_ENABLED=1, distroless/cc-debian12, bundled
    libonnxruntime.so.1.26.0)
- The NER variant runs `ANALYZER_BACKEND=gliner` with the English-base
  model `knowledgator/gliner-pii-base-v1.0` at threshold 0.40. See
  [`bench_findings_2026_05_13.md`](.claude/memory/bench_findings_2026_05_13.md)
  for the reasoning (multilingual variant rejected for score-calibration
  brittleness).
- Per-request `disable_ner: true` falls back to patterns-only when NER
  latency isn't wanted.

## Build tags

- Default build: pure Go, no NER, no CGO. Production-safe everywhere.
- `-tags hugot`: enables the in-process ONNX recognizers
  (HugotNERRecognizer + GLiNERRecognizer). Used by `Dockerfile.platform-ner`.
- For the GLiNER path, CGO is required AND libonnxruntime.so must be
  reachable at runtime via `ORT_SO_PATH` (set by the Dockerfile).

## Bench harness

Everything lives under `bench/`. The top-level entry is `bench/Makefile`:

  - `make -C bench help` lists every target.
  - `make -C bench matrix` runs **every engine × every corpus** and renders
    one combined `bench/REPORT_MATRIX.md`. Engines: anonde-patterns,
    anonde-gliner (production), anonde-hugot, presidio, gliner-py.
    Corpora: openmed + ggponc_de + pmc_de + synth_clinical + wiki_de
    (German) and ai4privacy_en (English).
  - `make -C bench matrix-de` / `matrix-en` for language subsets.
  - `make -C bench/corpora/<NAME> all` runs one corpus directly; per-corpus
    Makefiles accept `ANONDE_BACKEND={patterns-only|hugot|gliner}`,
    `ANONDE_MODEL`, `ANONDE_ONNX_FILE`, `NER_SCORE_FLOOR`, etc.

The Go bench runner is `bench/runners/anonde.go` (single source of truth).
Python sidecars for non-Go backends: `bench/runners/{presidio,gliner,
openai_pf}.py`. Shared scoring: `bench/scoring/{compare.py,render_matrix.py,
merge_findings.py,label_map.yaml}`. Diagnostic probes (not in the matrix):
`bench/probes/{hugot,gliner,diff_gliner}/`.
