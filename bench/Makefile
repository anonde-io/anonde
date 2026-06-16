# bench/Makefile — top-level entry to the anonde bench matrix.
#
# The headline target is `make matrix`: it runs every engine (patterns,
# GLiNER, Presidio, GLiNER-py sidecar) across every corpus
# (German clinical + English PII) and renders a single combined
# REPORT_MATRIX.md that answers "does anonde+patterns+GLiNER win across
# both languages?"
#
# Per-cell outputs live under each corpus's data/ directory as
# `anonde_<engine>.jsonl`. Per-corpus REPORT*.md and results*.csv are
# left intact (each corpus's `make report` knows how to scoresheet its
# own data). The matrix renderer ingests every per-cell findings JSONL
# and produces:
#
#   bench/REPORT_MATRIX.md   single combined report
#   bench/results_matrix.csv flat row-per-(corpus, engine, entity, view)
#
# Subset targets:
#   make matrix-de   German corpora only
#   make matrix-en   English corpora only
#   make matrix-es   Spanish corpora only
#   make matrix-fr   French corpora only
#   make matrix-it   Italian corpora only
#   make data        fetch/generate every corpus (no engine runs)
#   make clean       remove per-corpus data + the matrix outputs
#
# openai-pf is a rendered matrix column but is gated behind RUN_OPENAI_PF=1
# (it is ~80 s/doc on CPU). `make matrix RUN_OPENAI_PF=1` runs it on a
# deterministic OPENAI_PF_SAMPLE-doc subsample; without the flag the
# openai-pf column renders as "–". See the ENGINES block below.
#
# Tuning knobs forwarded to each per-corpus Makefile:
#   ANONDE_BACKEND       which anonde mode (patterns-only|gliner)
#   ANONDE_MODEL         override default model id for the active backend
#   ANONDE_ONNX_FILE     override default ONNX file inside the HF repo
#   LABEL_SET            GLiNER label set: chat|clinical|finance|legal.
#                        Default: per-corpus (each corpus runs its own domain);
#                        setting it forces that label set across every cell.
#   GLINER_MODEL         model id for the GLiNER cells (default: PII base)
#   GLINER_THRESHOLD     GLiNER threshold (default: 0.40)
#   RUN_OPENAI_PF        set to 1 to run the (slow) openai-pf cells
#   OPENAI_PF_SAMPLE     docs the openai-pf cell scores (default: 40)
#   PYTHON               python3 executable (default: python3)

HERE   := $(dir $(abspath $(lastword $(MAKEFILE_LIST))))
ROOT   := $(HERE)..
CORPORA_DIR := $(HERE)corpora

PYTHON   ?= python3
GLINER_MODEL     ?= knowledgator/gliner-pii-base-v1.0
# FP32 ONNX of the BASE GLiNER PII model — the one the
# `ghcr.io/anonde-io/anonde-ner` image actually ships. The bench uses
# the FP32 export (not model_fp16.onnx, which has an
# onnxruntime-incompatible graph and is rejected on session create —
# see bench/probes/fp32_vs_int8/). INT8 (`model_quint8.onnx`) is a
# runtime flag of the same image, not a separate engine, so it no
# longer has its own bench column.
GLINER_ONNX_FILE ?= onnx/model.onnx
# Larger GLiNER PII variant — 3-4x the parameter count of the base
# model. Same HF org, same task; FP32 ONNX. Loaded by the LARGE flat
# decoder that stacks on top of the BASE recognizer in the
# `anonde-ner-stack` engine (and the matching
# `ghcr.io/anonde-io/anonde-ner-stack` image), giving the premium tier
# its lift on the Romance-language cells where the default still
# leaks.
GLINER_LARGE_MODEL     ?= knowledgator/gliner-pii-large-v1.0
# HuggingFace's knowledgator/gliner-pii-large-v1.0 exports its ONNX at
# `onnx/model.onnx` — same layout as the BASE model. A prior comment
# here claimed it shipped at the repo root; that was wrong and broke
# the LARGE download in the gliner-flat recognizer (the downloader
# does an exact file lookup via the HF Hub API, not a directory walk,
# so a missing file 404s and the cell silently falls back to the BASE
# recognizer only — making anonde-ner-stack byte-equivalent to
# anonde-ner). Verified against the HF tree API on 2026-05-27.
GLINER_LARGE_ONNX_FILE ?= onnx/model.onnx
GLINER_THRESHOLD ?= 0.40
ANONDE_MODEL     ?=
ANONDE_ONNX_FILE ?=

# GLiNER label set the NER cells run: chat|clinical|finance|legal. Threaded
# to the runner's --label-set flag, but per-corpus via LABEL_SET_FOR (below)
# so each corpus is measured under its OWN domain, not a blanket default.
# This global is only the FALLBACK for corpora LABEL_SET_FOR doesn't classify,
# and the chat default matches the library + per-corpus Makefile defaults.
# Setting LABEL_SET=<x> on the command line forces every cell to <x> (an
# explicit override beats the per-corpus mapping).
LABEL_SET        ?= chat

# Per-language corpus partitions. ggponc_de is OMITTED from the matrix
# because its corpus requires manual registration + extraction (see
# bench/corpora/ggponc_de/README.md). Run it manually via
# `make -C bench/corpora/ggponc_de all` once the raw archive is in
# bench/corpora/ggponc_de/data/raw/.
#
# conll2003_de is wired in but its loader exits with code 2 in CI: the
# original LDC corpus is gated and no public HF mirror hosts the gold
# spans at time of writing. The loader is forward-compatible — when/if
# a mirror appears, the bench picks it up automatically (no Makefile
# changes needed).
#
# meddocan_es is wired into the matrix (the real-gold ES clinical
# corpus). MEDDOCAN is openly licensed (CC-BY-4.0, Zenodo) — not
# registration-gated — but its loader fetches the archive over the
# network. Like conll2003_de, the loader exits 2 cleanly if Zenodo is
# unreachable: a transient network failure shows the cell as missing
# rather than reddening the whole matrix (the CI matrix is fail-fast:
# false and the renderer tolerates missing cells).
#
# Phase 3 of the multilingual bench expansion adds two synthetic
# slot-based families: synth_finance_{en,de,es,fr,it} (financial
# documents — invoices, statements, KYC, transaction confirmations; one
# shared generator in bench/corpora/synth_finance_en/) and synth_logs
# (enterprise logs — auth/error/access/audit; English-classified, see
# its README for why). All six emit canonical label_map gold types.
#
# Phase 4 adds clinical-domain depth: the synth_clinical generator
# (canonical home: bench/corpora/synth_clinical/) is now parametrised by
# --language, and synth_clinical_{en,fr,it} are thin wrappers around it.
# With synth_clinical (de) + openmed + pmc_de that gives DE clinical
# depth; meddocan_es is the real-gold ES clinical corpus.
DE_CORPORA := openmed pmc_de synth_clinical wiki_de finance_de legal_de \
              wikiann_de germeval_14 conll2003_de adversarial_de \
              ai4privacy_de mapa_de synth_finance_de
EN_CORPORA := ai4privacy_en conll2003_en wnut_17 mapa_en \
              synth_finance_en synth_logs synth_clinical_en
ES_CORPORA := pharmaconer_es ai4privacy_es mapa_es synth_finance_es \
              meddocan_es
# ES / FR / IT partitions populated by Phase 1 of the multilingual bench
# expansion. ai4privacy_es / ai4privacy_fr / ai4privacy_it are the
# Spanish / French / Italian slices of ai4privacy/pii-masking-300k (one
# shared loader, see bench/corpora/ai4privacy_en/cmd/fetch_pii_masking.py).
# The 300k release covers six languages (en/fr/de/it/es/nl) — Spanish is
# new vs the old 200k dataset, hence the ai4privacy_es corpus. No nl
# corpus is wired yet.
#
# Phase 2 adds the MAPA legal/administrative corpus across five
# languages — mapa_{en,de,es,fr,it} — from joelniklaus/mapa (one shared
# loader, see bench/corpora/mapa_en/cmd/fetch_mapa.py). MAPA gives the
# matrix a real-gold legal domain number; the synthetic legal_de corpus
# stays alongside mapa_de (different probes — generated vs real text).
FR_CORPORA := ai4privacy_fr mapa_fr synth_finance_fr synth_clinical_fr
IT_CORPORA := ai4privacy_it mapa_it synth_finance_it synth_clinical_it
ALL_CORPORA := $(DE_CORPORA) $(EN_CORPORA) $(ES_CORPORA) $(FR_CORPORA) $(IT_CORPORA)

# ---- PER-CORPUS LABEL-SET CLASSIFICATION ---------------------------------------
# Each corpus is measured under its OWN domain (clinical/finance/legal/chat) so
# the score is meaningful — a finance corpus scored with clinical labels is not.
# These partitions mirror the LABEL_SET ?= <domain> default in each corpus's own
# Makefile; keep them in sync when adding a corpus.
CLINICAL_CORPORA := openmed pmc_de ggponc_de synth_clinical synth_clinical_en \
                    synth_clinical_fr synth_clinical_it meddocan_es pharmaconer_es
FINANCE_CORPORA  := finance_de synth_finance_de synth_finance_en \
                    synth_finance_es synth_finance_fr synth_finance_it
LEGAL_CORPORA    := mapa_de mapa_en mapa_es mapa_fr mapa_it legal_de
# Everything else (ai4privacy_*, wiki_de, synth_logs, the NER-only corpora)
# falls through to the chat global default.

# LABEL_SET_FOR($1=corpus) → the label set that corpus's cell runs under.
# If LABEL_SET was set explicitly (command line / environment), that wins for
# every cell — an operator forcing one label set across the matrix. Otherwise
# classify per corpus, falling back to $(LABEL_SET) (= chat).
ifneq ($(filter command line environment,$(origin LABEL_SET)),)
LABEL_SET_FOR = $(LABEL_SET)
else
LABEL_SET_FOR = $(if $(filter $(1),$(CLINICAL_CORPORA)),clinical,\
                $(if $(filter $(1),$(FINANCE_CORPORA)),finance,\
                $(if $(filter $(1),$(LEGAL_CORPORA)),legal,$(LABEL_SET))))
endif

# To add a dedicated finance/legal corpus: create bench/corpora/<NAME>/ (loader +
# gold; see _scaffold_finance_legal/README.md), add it to the right *_CORPORA
# partition above AND the matching *_CORPORA classification list here, and set
# LABEL_SET ?= <domain> in its own Makefile. Gold must map through label_map.yaml
# to the canonical types FinancePIILabelToEntity / LegalPIILabelToEntity emit.
# Ad-hoc probe: make -C bench/corpora/<NAME> all ANONDE_BACKEND=gliner LABEL_SET=finance
# --------------------------------------------------------------------------------

# The engines the matrix scores. Each row's name is what compare.py
# routes through the label_map (so prefixes matter — see compare.py).
#
# openai-pf IS a rendered column, but it is gated: it is ~80 sec/doc on
# CPU, far too slow to run the whole corpus on a PR. So it is benchmarked
# on a deterministic subsample (OPENAI_PF_SAMPLE docs, sorted by id) and
# its cell only runs when RUN_OPENAI_PF=1. The `matrix` target and
# bench-full.yml set that flag; the per-push `corpus-%` path / bench.yml
# do NOT, so a PR bench never pays the openai-pf cost. When RUN_OPENAI_PF
# is unset the openai-pf column simply renders as "–" (cell not run).
#
# anonde-ner is the default NER tier — the engine that ships in
# `ghcr.io/anonde-io/anonde-ner`. It loads the BASE GLiNER PII model
# at FP32 (`onnx/model.onnx`) and is the verdict anchor in the
# scorecard. Runs as part of `corpus-%`, so every per-push bench
# scores it.
#
# anonde-ner-stack is the premium NER tier — the engine that ships in
# `ghcr.io/anonde-io/anonde-ner-stack`. It stacks BOTH the BASE FP32
# recognizer AND the LARGE flat-decoder recognizer in the same
# analyzer engine — every doc runs through both inferences and
# RemoveConflicts merges overlaps. Gives the matrix the strongest
# leak-rate numbers; pick this image when you can spare the RAM
# (~2.1 GB image, ~4 GB resident). Gated OUT of `corpus-%`; pulled
# in only by `matrix` / `matrix-*` via the ner-stack-cells target
# below.
ENGINES := anonde-patterns anonde-ner anonde-ner-stack presidio gliner-py openai-pf

# openai-pf gating + subsample size. RUN_OPENAI_PF=1 enables the cell;
# OPENAI_PF_SAMPLE is how many docs (deterministic, sorted by id) the
# openai-pf runner scores. render_matrix.py scores openai-pf over only
# the docs it returned, so the subsample does not show as a fake leak.
RUN_OPENAI_PF     ?=
OPENAI_PF_SAMPLE  ?= 40

# openai-pf runs from its OWN venv. The openai/privacy-filter model needs
# transformers>=5.6 (native "openai_privacy_filter" architecture), but
# gliner==0.2.26 in bench/requirements.txt caps transformers<5.2 and has
# no release that allows 5.6+. The two engines cannot share one env, so
# the openai-pf cell bootstraps .venv-openai-pf from
# requirements-openai-pf.txt. Override OPENAI_PF_PYTHON to reuse an
# existing interpreter that already has transformers>=5.6.
OPENAI_PF_VENV    ?= $(HERE).venv-openai-pf
OPENAI_PF_PYTHON  ?= $(OPENAI_PF_VENV)/bin/python

MATRIX_REPORT := $(HERE)REPORT_MATRIX.md
MATRIX_CSV    := $(HERE)results_matrix.csv

.PHONY: help data matrix matrix-de matrix-en matrix-es matrix-fr matrix-it clean \
        $(addprefix corpus-,$(ALL_CORPORA)) \
        $(addprefix data-,$(ALL_CORPORA))

help:
	@echo ""
	@echo "anonde bench harness"
	@echo "===================="
	@echo ""
	@echo "  make help               show this listing"
	@echo "  make data               fetch/generate every corpus (no engine runs)"
	@echo "  make matrix             run every engine x every corpus, render REPORT_MATRIX.md"
	@echo "  make matrix-de          German subset only"
	@echo "  make matrix-en          English subset only"
	@echo "  make matrix-es          Spanish subset only"
	@echo "  make matrix-fr          French subset only"
	@echo "  make matrix-it          Italian subset only"
	@echo "  make corpus-<NAME>      run the 4 always-on engines on a single corpus"
	@echo "  make clean              remove generated data + reports + the matrix outputs"
	@echo ""
	@echo "Corpora (German):  $(DE_CORPORA)"
	@echo "Corpora (English): $(EN_CORPORA)"
	@echo "Corpora (Spanish): $(ES_CORPORA)"
	@echo "Corpora (French):  $(FR_CORPORA)"
	@echo "Corpora (Italian): $(IT_CORPORA)"
	@echo "Engines: $(ENGINES)"
	@echo ""
	@echo "openai-pf is gated: pass RUN_OPENAI_PF=1 to run its cells"
	@echo "  (~80 s/doc; subsample size OPENAI_PF_SAMPLE=$(OPENAI_PF_SAMPLE) docs)"
	@echo ""
	@echo "GLiNER label set: per-corpus (clinical/finance/legal/chat); LABEL_SET=$(LABEL_SET) is the fallback/override"
	@echo "  finance/legal are wired end-to-end but need a real gold corpus"
	@echo "  (see the FINANCE/LEGAL SCAFFOLD block in bench/Makefile)"
	@echo ""
	@echo "Per-corpus targets also reachable directly:"
	@echo "  make -C bench/corpora/<NAME> all ANONDE_BACKEND=<mode>"
	@echo ""

# ---- data fetch ----------------------------------------------------------------
data: $(addprefix data-,$(ALL_CORPORA))

$(addprefix data-,$(ALL_CORPORA)):
	$(MAKE) -C $(CORPORA_DIR)/$(patsubst data-%,%,$@) data PYTHON=$(PYTHON)

# ---- the matrix ----------------------------------------------------------------
# A cell is a (corpus, engine) pair. For each one we produce a findings
# JSONL at $(CORPORA_DIR)/<corpus>/data/anonde_<engine>.jsonl. The matrix
# renderer then ingests every cell.
#
# Implementation strategy: a parameterised recipe per (corpus, engine).
# We expand this in $(eval ...) loops below; each generated target
# depends on the corpus's data target and shells out to the right runner.

# anonde engine name → analyzer backend.
#   anonde-ner        → backend=gliner   (span decoder, BASE FP32 —
#                                         the default NER image's stack)
#   anonde-ner-stack  → backend=gliner   (BASE FP32 + the LARGE flat
#                                         recognizer added on top via
#                                         the --flat-gliner-* flags)
# Which model id / ONNX file each cell passes is decided by
# GLINER_MODEL_FOR + GLINER_ONNX_FILE_FOR; the extra stack flags are
# appended by CELL_anonde only when $(2) == anonde-ner-stack.
define ANONDE_BACKEND_FOR
$(if $(filter anonde-patterns,$(1)),patterns-only,\
$(if $(filter anonde-ner anonde-ner-stack,$(1)),gliner,\
))
endef

# anonde-* engine name → ONNX file (BASE NER slot).
#   anonde-ner        → FP32 BASE (default NER image)
#   anonde-ner-stack  → FP32 BASE in the NER slot; the LARGE flat
#                       recognizer is added on top via the
#                       --flat-gliner-* flags (see CELL_anonde)
# Any other engine falls back to ANONDE_ONNX_FILE (the generic override
# knob). Kept on ONE line on purpose: a multi-line `define` with
# `\`-continued `$(if ...)` collapses each continuation to a leading
# space, and this value lands inside a quoted `--onnx-file "..."` arg
# where that space is not stripped.
GLINER_ONNX_FILE_FOR = $(if $(filter anonde-ner anonde-ner-stack,$(1)),$(GLINER_ONNX_FILE),$(ANONDE_ONNX_FILE))

# anonde-* engine name → GLiNER model id (BASE NER slot). Both
# anonde-ner and anonde-ner-stack use the production BASE model id
# (GLINER_MODEL, default knowledgator/gliner-pii-base-v1.0). The
# stack engine ALSO adds the LARGE sibling on top via
# --flat-gliner-model (see CELL_anonde); that does not go through
# this macro. Kept as a function-style assignment for symmetry with
# GLINER_ONNX_FILE_FOR above; today it's a constant for every
# in-matrix anonde engine, but leaving the macro in place keeps the
# call site identical if a future engine needs a different base id.
GLINER_MODEL_FOR = $(GLINER_MODEL)

# Corpus → language code. Defaults to `de` when a corpus is in none of the
# explicit partitions. en/es/fr/it are matched against their partitions;
# everything else (the German clinical corpora) falls through to `de`.
define LANG_FOR_CORPUS
$(if $(filter $(1),$(EN_CORPORA)),en,\
$(if $(filter $(1),$(ES_CORPORA)),es,\
$(if $(filter $(1),$(FR_CORPORA)),fr,\
$(if $(filter $(1),$(IT_CORPORA)),it,de))))
endef

# --fold-parity-labels folds STREET_ADDRESS + POSTAL_CODE to LOCATION.
# Needed for every ai4privacy_* corpus (its gold buckets street + zip
# under LOCATION) and every mapa_* corpus (MAPA's coarse gold buckets
# street-level detail under a single ADDRESS type) regardless of
# language, plus the other EN corpora whose gold uses the same
# convention. ai4privacy_de/fr/it and mapa_de/es/fr/it live in the
# DE/ES/FR/IT partitions, so a plain EN_CORPORA filter would miss them —
# match the ai4privacy_ / mapa_ prefixes explicitly.
#
# EXCLUDED: the Phase 3 synth_finance_* / synth_logs and the Phase 4
# synth_clinical_* corpora. Their generators emit gold where street +
# zip map to ADDRESS (synth_finance) or LOCATION_STREET/LOCATION_ZIP map
# to ADDRESS via label_map (synth_clinical) — neither uses the
# ai4privacy LOCATION-bucket convention. Folding anonde's
# STREET_ADDRESS/POSTAL_CODE predictions to LOCATION there would
# mismatch the ADDRESS gold and inflate the leak rate. The negative
# filter keeps fold off for every synth_finance_* / synth_clinical_*
# even though synth_finance_en and synth_clinical_en live in EN_CORPORA
# — this matches the no-fold treatment the German synth_clinical corpus
# (in DE_CORPORA) already gets.
define FOLD_FOR_CORPUS
$(if $(filter synth_finance_% synth_clinical_% synth_logs,$(1)),,$(if $(or $(filter $(1),$(EN_CORPORA)),$(filter ai4privacy_% mapa_%,$(1))),--fold-parity-labels,))
endef

# Local libonnxruntime path for the gliner backend's ORT session. Matches
# the .gitignore'd .tokenlib/ convention. On Linux/CI, override with
# ORT_LIB=/path/to/libonnxruntime.so. Without this the recognizer's init
# fails with `dlopen("onnxruntime.so"): no such file` and silently falls
# back to patterns-only — visible only via the analyzer's swallowed-error
# log line in analyzer/analyzer.go.
ORT_LIB ?= $(ROOT)/.tokenlib/libonnxruntime.dylib

# anonde-* cells: in-process Go runner. Build-tag selected by backend.
define CELL_anonde
# NOT .PHONY: cache existing per-cell outputs so partial matrices don't re-run.
# data-$(1) is .PHONY which triggers make's "if newer than target" check,
# but the data target itself is a no-op if data/corpus.jsonl exists, so the
# cell's output mtime usually wins → cell skipped. Force a re-run by
# `rm bench/corpora/<c>/data/anonde_<engine>.jsonl` before `make matrix`.
$(CORPORA_DIR)/$(1)/data/anonde_$(2).jsonl: | data-$(1)
	@echo "[matrix] $(1) x $(2)"
	cd $(ROOT) && go run \
	  $(if $(filter $(2),anonde-ner anonde-ner-stack),-tags ner,) \
	  ./bench/runners/anonde.go \
	    --in  bench/corpora/$(1)/data/corpus.jsonl \
	    --out bench/corpora/$(1)/data/anonde_$(2).jsonl \
	    --backend $(call ANONDE_BACKEND_FOR,$(2)) \
	    --language $(call LANG_FOR_CORPUS,$(1)) \
	    --model "$(if $(filter $(2),anonde-ner anonde-ner-stack),$(call GLINER_MODEL_FOR,$(2)),)" \
	    --onnx-file "$(call GLINER_ONNX_FILE_FOR,$(2))" \
	    --gliner-threshold $(if $(filter $(2),anonde-ner anonde-ner-stack),$(GLINER_THRESHOLD),0) \
	    $(if $(filter $(2),anonde-ner anonde-ner-stack),--label-set "$(call LABEL_SET_FOR,$(1))",) \
	    $(if $(filter $(2),anonde-ner anonde-ner-stack),--ort-library "$(ORT_LIB)",) \
	    $(if $(filter $(2),anonde-ner-stack),--flat-gliner-model "$(GLINER_LARGE_MODEL)" --flat-gliner-onnx "$(GLINER_LARGE_ONNX_FILE)" --flat-gliner-threshold 0,) \
	    $(call FOLD_FOR_CORPUS,$(1))
endef

# presidio cell: multilingual. Picks the spaCy model per corpus language
# (LANG_FOR_CORPUS macro). Each cell tolerates failure: if the matching
# spaCy model isn't installed locally (e.g. de_core_news_lg on a
# patterns-only dev box), the runner exits 2 and the renderer marks
# the cell as missing rather than aborting the whole matrix.
define CELL_presidio
# NOT .PHONY: cache existing per-cell outputs so partial matrices don't re-run.
# data-$(1) is .PHONY which triggers make's "if newer than target" check,
# but the data target itself is a no-op if data/corpus.jsonl exists, so the
# cell's output mtime usually wins → cell skipped. Force a re-run by
# `rm bench/corpora/<c>/data/anonde_<engine>.jsonl` before `make matrix`.
$(CORPORA_DIR)/$(1)/data/anonde_presidio.jsonl: | data-$(1)
	@echo "[matrix] $(1) x presidio (lang=$(call LANG_FOR_CORPUS,$(1)))"
	$(PYTHON) $(ROOT)/bench/runners/presidio.py \
	  --in  $(CORPORA_DIR)/$(1)/data/corpus.jsonl \
	  --out $(CORPORA_DIR)/$(1)/data/anonde_presidio.jsonl \
	  --language $(call LANG_FOR_CORPUS,$(1)) || \
	  echo "[matrix] WARN: presidio cell for $(1) failed — render will show as missing"
endef

# gliner-py cell: Python sidecar; works on any corpus, English-friendly labels.
# Script is named `gliner_sidecar.py` (not `gliner.py`) because a script
# named `gliner.py` would shadow the installed `gliner` package on the
# script-dir-first import path, causing `from gliner import GLiNER` to
# resolve to itself and fail.
define CELL_gliner_py
# NOT .PHONY: cache existing per-cell outputs so partial matrices don't re-run.
# data-$(1) is .PHONY which triggers make's "if newer than target" check,
# but the data target itself is a no-op if data/corpus.jsonl exists, so the
# cell's output mtime usually wins → cell skipped. Force a re-run by
# `rm bench/corpora/<c>/data/anonde_<engine>.jsonl` before `make matrix`.
$(CORPORA_DIR)/$(1)/data/anonde_gliner-py.jsonl: | data-$(1)
	@echo "[matrix] $(1) x gliner-py"
	$(PYTHON) $(ROOT)/bench/runners/gliner_sidecar.py \
	  --in  $(CORPORA_DIR)/$(1)/data/corpus.jsonl \
	  --out $(CORPORA_DIR)/$(1)/data/anonde_gliner-py.jsonl \
	  --model $(GLINER_MODEL) \
	  --threshold $(GLINER_THRESHOLD) \
	  --engine-label gliner-py || \
	  echo "[matrix] WARN: gliner-py cell for $(1) failed — render will show as missing"
endef

# openai-pf cell: Python sidecar for the OpenAI Privacy Filter model.
# Mirrors CELL_gliner_py (warn-and-skip on failure, NOT .PHONY so a
# completed cell is cached). Two differences from the other cells:
#
#   1. GATED by RUN_OPENAI_PF. openai-pf is ~80 sec/doc on CPU — far too
#      slow for the per-push PR bench (bench.yml, ~30 min budget). When
#      RUN_OPENAI_PF is unset the recipe is a no-op that prints why; the
#      renderer then shows the openai-pf column as "–" for that corpus.
#      `matrix` and bench-full.yml set RUN_OPENAI_PF=1; `corpus-%` /
#      bench.yml do not.
#   2. SUBSAMPLED via --max-docs $(OPENAI_PF_SAMPLE). Even when enabled it
#      only scores the first N docs (deterministic, sorted by id), so the
#      cell stays bounded (40 docs × ~80 s ≈ 53 min). render_matrix.py
#      scores openai-pf over only the docs it returned.
# Bootstrap the isolated openai-pf venv (transformers>=5.6 for the
# openai/privacy-filter native architecture — see requirements-openai-pf.txt).
# Gated on RUN_OPENAI_PF so a plain `matrix` run never pays for it. The
# stamp file makes the (slow) pip install run once, not per corpus cell.
.PHONY: openai-pf-venv
openai-pf-venv: $(OPENAI_PF_VENV)/.stamp
$(OPENAI_PF_VENV)/.stamp: $(HERE)requirements-openai-pf.txt
ifeq ($(strip $(RUN_OPENAI_PF)),)
	@echo "[matrix] SKIP openai-pf venv — RUN_OPENAI_PF unset"
else
	@echo "[matrix] bootstrapping openai-pf venv at $(OPENAI_PF_VENV)"
	$(PYTHON) -m venv $(OPENAI_PF_VENV)
	$(OPENAI_PF_PYTHON) -m pip install --upgrade pip
	$(OPENAI_PF_PYTHON) -m pip install -r $(HERE)requirements-openai-pf.txt
	@touch $@
endif

define CELL_openai_pf
# NOT .PHONY: cache existing per-cell outputs so partial matrices don't re-run.
$(CORPORA_DIR)/$(1)/data/anonde_openai-pf.jsonl: | data-$(1) openai-pf-venv
ifeq ($(strip $(RUN_OPENAI_PF)),)
	@echo "[matrix] SKIP $(1) x openai-pf — RUN_OPENAI_PF unset (cell is ~80 s/doc; set RUN_OPENAI_PF=1 to enable)"
else
	@echo "[matrix] $(1) x openai-pf (sample=$(OPENAI_PF_SAMPLE) docs)"
	$(OPENAI_PF_PYTHON) $(ROOT)/bench/runners/openai_pf.py \
	  --in  $(CORPORA_DIR)/$(1)/data/corpus.jsonl \
	  --out $(CORPORA_DIR)/$(1)/data/anonde_openai-pf.jsonl \
	  --engine-label openai-pf \
	  --max-docs $(OPENAI_PF_SAMPLE) || \
	  echo "[matrix] WARN: openai-pf cell for $(1) failed — render will show as missing"
endif
endef

# Expand per-corpus, per-engine. Presidio now runs on every language
# the matrix supports — the runner picks the right spaCy model from
# the corpus's LANG_FOR_CORPUS classification (en/de/es). If a model
# isn't installed locally the cell is reported as missing rather than
# crashing the build.
$(foreach c,$(ALL_CORPORA),\
  $(foreach e,anonde-patterns anonde-ner anonde-ner-stack,\
    $(eval $(call CELL_anonde,$(c),$(e))))\
  $(eval $(call CELL_gliner_py,$(c)))\
  $(eval $(call CELL_presidio,$(c)))\
  $(eval $(call CELL_openai_pf,$(c))))

# Per-corpus convenience target — runs the always-on engines for one
# corpus. openai-pf and anonde-ner-stack are deliberately NOT
# prerequisites here: this is the target the per-push bench
# (bench.yml) calls via `corpus-openmed` etc., and the per-push bench
# stays lean (anonde-patterns + anonde-ner + gliner-py + presidio).
# openai-pf at ~80 s/doc would blow the PR budget; anonde-ner-stack
# is a fast in-process Go cell but loads ~1.7 GB of LARGE weights on
# top of the BASE ONNX, so it is kept off `corpus-%` to leave the
# per-push bench scope unchanged. Both are pulled in only by
# `matrix` / `matrix-*` (see openai-pf-cells and ner-stack-cells
# below). Presidio runs on every language (en + de + es spaCy
# models); if a given dev box lacks the matching model the cell
# warn-and-skips.
$(addprefix corpus-,$(ALL_CORPORA)): corpus-%: \
    $(CORPORA_DIR)/%/data/anonde_anonde-patterns.jsonl \
    $(CORPORA_DIR)/%/data/anonde_anonde-ner.jsonl \
    $(CORPORA_DIR)/%/data/anonde_gliner-py.jsonl \
    $(CORPORA_DIR)/%/data/anonde_presidio.jsonl

# openai-pf cells, separated out so only the full-matrix targets depend on
# them. Each cell self-gates on RUN_OPENAI_PF (no-op + skip message when
# unset), so even when this prerequisite list is materialised the cost is
# zero unless RUN_OPENAI_PF=1 is passed.
.PHONY: openai-pf-cells $(addprefix openai-pf-,$(ALL_CORPORA))
openai-pf-cells: $(addprefix $(CORPORA_DIR)/,$(addsuffix /data/anonde_openai-pf.jsonl,$(ALL_CORPORA)))

# anonde-ner-stack cells, separated out the same way as openai-pf-cells:
# only the full-matrix targets (`matrix` / `matrix-*`) depend on them, so
# the per-push `corpus-%` path / bench.yml never build the stack cell.
# Each cell loads BOTH the BASE FP32 ONNX and the LARGE flat-decoder
# ONNX (the LARGE weights are ~1.7 GB), so cells run a touch slower
# than the single-model anonde-ner variant but stay well inside the
# matrix cell budget.
.PHONY: ner-stack-cells $(addprefix ner-stack-,$(ALL_CORPORA))
ner-stack-cells: $(addprefix $(CORPORA_DIR)/,$(addsuffix /data/anonde_anonde-ner-stack.jsonl,$(ALL_CORPORA)))

# Per-corpus anonde-ner-stack convenience target — `make
# ner-stack-openmed`. Used by bench-full.yml so each matrix cell
# builds only its own corpus's stack cell without overriding ALL_CORPORA.
$(addprefix ner-stack-,$(ALL_CORPORA)): ner-stack-%: \
    $(CORPORA_DIR)/%/data/anonde_anonde-ner-stack.jsonl

# Per-corpus openai-pf convenience target — `make openai-pf-openmed
# RUN_OPENAI_PF=1`. Used by bench-full.yml so each matrix cell runs only
# its own corpus's openai-pf cell without overriding ALL_CORPORA.
$(addprefix openai-pf-,$(ALL_CORPORA)): openai-pf-%: \
    $(CORPORA_DIR)/%/data/anonde_openai-pf.jsonl

# Materialise the full matrix (always-on engines + anonde-ner-stack +
# openai-pf), then render the combined report. Run openai-pf with the
# gate on:
#   make matrix RUN_OPENAI_PF=1
matrix: $(addprefix corpus-,$(ALL_CORPORA)) ner-stack-cells openai-pf-cells render

# Per-language subset matrices. Each depends on its corpora's always-on
# cells plus the openai-pf cells (self-gated on RUN_OPENAI_PF). FR / IT
# mirror ES exactly — Phase 1 populated FR_CORPORA / IT_CORPORA with the
# ai4privacy French / Italian slices.
matrix-de: $(addprefix corpus-,$(DE_CORPORA)) \
           $(addprefix $(CORPORA_DIR)/,$(addsuffix /data/anonde_anonde-ner-stack.jsonl,$(DE_CORPORA))) \
           $(addprefix $(CORPORA_DIR)/,$(addsuffix /data/anonde_openai-pf.jsonl,$(DE_CORPORA)))
	$(PYTHON) $(HERE)scoring/render_matrix.py \
	  --corpora-root $(CORPORA_DIR) \
	  --corpus "$(DE_CORPORA)" \
	  --engine "$(ENGINES)" \
	  --label-map $(HERE)scoring/label_map.yaml \
	  --out $(MATRIX_REPORT) \
	  --csv $(MATRIX_CSV)

matrix-en: $(addprefix corpus-,$(EN_CORPORA)) \
           $(addprefix $(CORPORA_DIR)/,$(addsuffix /data/anonde_anonde-ner-stack.jsonl,$(EN_CORPORA))) \
           $(addprefix $(CORPORA_DIR)/,$(addsuffix /data/anonde_openai-pf.jsonl,$(EN_CORPORA)))
	$(PYTHON) $(HERE)scoring/render_matrix.py \
	  --corpora-root $(CORPORA_DIR) \
	  --corpus "$(EN_CORPORA)" \
	  --engine "$(ENGINES)" \
	  --label-map $(HERE)scoring/label_map.yaml \
	  --out $(MATRIX_REPORT) \
	  --csv $(MATRIX_CSV)

matrix-es: $(addprefix corpus-,$(ES_CORPORA)) \
           $(addprefix $(CORPORA_DIR)/,$(addsuffix /data/anonde_anonde-ner-stack.jsonl,$(ES_CORPORA))) \
           $(addprefix $(CORPORA_DIR)/,$(addsuffix /data/anonde_openai-pf.jsonl,$(ES_CORPORA)))
	$(PYTHON) $(HERE)scoring/render_matrix.py \
	  --corpora-root $(CORPORA_DIR) \
	  --corpus "$(ES_CORPORA)" \
	  --engine "$(ENGINES)" \
	  --label-map $(HERE)scoring/label_map.yaml \
	  --out $(MATRIX_REPORT) \
	  --csv $(MATRIX_CSV)

matrix-fr: $(addprefix corpus-,$(FR_CORPORA)) \
           $(addprefix $(CORPORA_DIR)/,$(addsuffix /data/anonde_anonde-ner-stack.jsonl,$(FR_CORPORA))) \
           $(addprefix $(CORPORA_DIR)/,$(addsuffix /data/anonde_openai-pf.jsonl,$(FR_CORPORA)))
	$(PYTHON) $(HERE)scoring/render_matrix.py \
	  --corpora-root $(CORPORA_DIR) \
	  --corpus "$(FR_CORPORA)" \
	  --engine "$(ENGINES)" \
	  --label-map $(HERE)scoring/label_map.yaml \
	  --out $(MATRIX_REPORT) \
	  --csv $(MATRIX_CSV)

matrix-it: $(addprefix corpus-,$(IT_CORPORA)) \
           $(addprefix $(CORPORA_DIR)/,$(addsuffix /data/anonde_anonde-ner-stack.jsonl,$(IT_CORPORA))) \
           $(addprefix $(CORPORA_DIR)/,$(addsuffix /data/anonde_openai-pf.jsonl,$(IT_CORPORA)))
	$(PYTHON) $(HERE)scoring/render_matrix.py \
	  --corpora-root $(CORPORA_DIR) \
	  --corpus "$(IT_CORPORA)" \
	  --engine "$(ENGINES)" \
	  --label-map $(HERE)scoring/label_map.yaml \
	  --out $(MATRIX_REPORT) \
	  --csv $(MATRIX_CSV)

.PHONY: render
render:
	$(PYTHON) $(HERE)scoring/render_matrix.py \
	  --corpora-root $(CORPORA_DIR) \
	  --corpus "$(ALL_CORPORA)" \
	  --engine "$(ENGINES)" \
	  --label-map $(HERE)scoring/label_map.yaml \
	  --out $(MATRIX_REPORT) \
	  --csv $(MATRIX_CSV)

clean:
	@for c in $(ALL_CORPORA); do \
	  $(MAKE) -s -C $(CORPORA_DIR)/$$c clean || true; \
	done
	rm -f $(MATRIX_REPORT) $(MATRIX_CSV)
