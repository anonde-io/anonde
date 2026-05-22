# bench/Makefile — top-level entry to the anonde bench matrix.
#
# The headline target is `make matrix`: it runs every engine (patterns,
# GLiNER, hugot, Presidio, GLiNER-py sidecar) across every corpus
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
#   make matrix-fr   French corpora only  (ai4privacy_fr)
#   make matrix-it   Italian corpora only (ai4privacy_it)
#   make data        fetch/generate every corpus (no engine runs)
#   make clean       remove per-corpus data + the matrix outputs
#
# openai-pf is a rendered matrix column but is gated behind RUN_OPENAI_PF=1
# (it is ~80 s/doc on CPU). `make matrix RUN_OPENAI_PF=1` runs it on a
# deterministic OPENAI_PF_SAMPLE-doc subsample; without the flag the
# openai-pf column renders as "–". See the ENGINES block below.
#
# Tuning knobs forwarded to each per-corpus Makefile:
#   ANONDE_BACKEND       which anonde mode (patterns-only|hugot|gliner)
#   ANONDE_MODEL         override default model id for the active backend
#   ANONDE_ONNX_FILE     override default ONNX file inside the HF repo
#   NER_SCORE_FLOOR      drop NER predictions below this score
#   GLINER_MODEL         model id for the GLiNER cells (default: PII base)
#   GLINER_THRESHOLD     GLiNER threshold (default: 0.40)
#   HUGOT_MODEL          model id for the hugot cells (default: multilang PII)
#   RUN_OPENAI_PF        set to 1 to run the (slow) openai-pf cells
#   OPENAI_PF_SAMPLE     docs the openai-pf cell scores (default: 40)
#   PYTHON               python3 executable (default: python3)

HERE   := $(dir $(abspath $(lastword $(MAKEFILE_LIST))))
ROOT   := $(HERE)..
CORPORA_DIR := $(HERE)corpora

PYTHON   ?= python3
GLINER_MODEL     ?= knowledgator/gliner-pii-base-v1.0
# The repo ships three ONNX variants (model.onnx, model_fp16.onnx,
# model_quint8.onnx); hugot's downloader can't pick on its own. Pin the
# int8-quantised build for CI/bench — same ~196 MB file the recognizer
# falls back to when ModelName == default AND OnnxFilePath == "".
GLINER_ONNX_FILE ?= onnx/model_quint8.onnx
GLINER_THRESHOLD ?= 0.40
HUGOT_MODEL      ?=
ANONDE_MODEL     ?=
ANONDE_ONNX_FILE ?=
NER_SCORE_FLOOR  ?= 0

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
DE_CORPORA := openmed pmc_de synth_clinical wiki_de finance_de legal_de \
              wikiann_de germeval_14 conll2003_de adversarial_de \
              ai4privacy_de
EN_CORPORA := ai4privacy_en conll2003_en wnut_17
ES_CORPORA := pharmaconer_es ai4privacy_es
# ES / FR / IT partitions populated by Phase 1 of the multilingual bench
# expansion. ai4privacy_es / ai4privacy_fr / ai4privacy_it are the
# Spanish / French / Italian slices of ai4privacy/pii-masking-300k (one
# shared loader, see bench/corpora/ai4privacy_en/cmd/fetch_pii_masking.py).
# The 300k release covers six languages (en/fr/de/it/es/nl) — Spanish is
# new vs the old 200k dataset, hence the ai4privacy_es corpus. No nl
# corpus is wired yet.
FR_CORPORA := ai4privacy_fr
IT_CORPORA := ai4privacy_it
ALL_CORPORA := $(DE_CORPORA) $(EN_CORPORA) $(ES_CORPORA) $(FR_CORPORA) $(IT_CORPORA)

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
# anonde-hugot stays dropped from the auto-matrix: hugot's XLM-R PII model
# is ~25 000× slower than patterns and the openmed cell proved it strictly
# underperforms anonde-gliner (F1 0.473 vs 0.529 on GraSCCo). It is still
# reachable as a one-off:
#   make -C bench/corpora/<NAME> all ANONDE_BACKEND=hugot
ENGINES := anonde-patterns anonde-gliner presidio gliner-py openai-pf

# openai-pf gating + subsample size. RUN_OPENAI_PF=1 enables the cell;
# OPENAI_PF_SAMPLE is how many docs (deterministic, sorted by id) the
# openai-pf runner scores. render_matrix.py scores openai-pf over only
# the docs it returned, so the subsample does not show as a fake leak.
RUN_OPENAI_PF     ?=
OPENAI_PF_SAMPLE  ?= 40

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
	@echo "  make matrix-fr          French subset only (ai4privacy_fr)"
	@echo "  make matrix-it          Italian subset only (ai4privacy_it)"
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

define ANONDE_BACKEND_FOR
$(if $(filter anonde-patterns,$(1)),patterns-only,\
$(if $(filter anonde-hugot,$(1)),hugot,\
$(if $(filter anonde-gliner,$(1)),gliner,\
)))
endef

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
# under LOCATION) regardless of language, plus the other EN corpora
# whose gold uses the same convention. ai4privacy_de/fr/it live in the
# DE/FR/IT partitions, so a plain EN_CORPORA filter would miss them —
# match the ai4privacy_ prefix explicitly.
define FOLD_FOR_CORPUS
$(if $(or $(filter $(1),$(EN_CORPORA)),$(filter ai4privacy_%,$(1))),--fold-parity-labels,)
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
	  $(if $(filter $(2),anonde-hugot anonde-gliner),-tags hugot,) \
	  ./bench/runners/anonde.go \
	    --in  bench/corpora/$(1)/data/corpus.jsonl \
	    --out bench/corpora/$(1)/data/anonde_$(2).jsonl \
	    --backend $(call ANONDE_BACKEND_FOR,$(2)) \
	    --language $(call LANG_FOR_CORPUS,$(1)) \
	    --ner-score-floor $(NER_SCORE_FLOOR) \
	    --model "$(if $(filter $(2),anonde-gliner),$(GLINER_MODEL),$(if $(filter $(2),anonde-hugot),$(HUGOT_MODEL),))" \
	    --onnx-file "$(if $(filter $(2),anonde-gliner),$(GLINER_ONNX_FILE),$(ANONDE_ONNX_FILE))" \
	    --gliner-threshold $(if $(filter $(2),anonde-gliner),$(GLINER_THRESHOLD),0) \
	    $(if $(filter $(2),anonde-gliner),--ort-library "$(ORT_LIB)",) \
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
define CELL_openai_pf
# NOT .PHONY: cache existing per-cell outputs so partial matrices don't re-run.
$(CORPORA_DIR)/$(1)/data/anonde_openai-pf.jsonl: | data-$(1)
ifeq ($(strip $(RUN_OPENAI_PF)),)
	@echo "[matrix] SKIP $(1) x openai-pf — RUN_OPENAI_PF unset (cell is ~80 s/doc; set RUN_OPENAI_PF=1 to enable)"
else
	@echo "[matrix] $(1) x openai-pf (sample=$(OPENAI_PF_SAMPLE) docs)"
	$(PYTHON) $(ROOT)/bench/runners/openai_pf.py \
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
  $(foreach e,anonde-patterns anonde-gliner,\
    $(eval $(call CELL_anonde,$(c),$(e))))\
  $(eval $(call CELL_gliner_py,$(c)))\
  $(eval $(call CELL_presidio,$(c)))\
  $(eval $(call CELL_openai_pf,$(c))))

# Per-corpus convenience target — runs the four always-on engines for one
# corpus. openai-pf is deliberately NOT a prerequisite here: this is the
# target the per-push bench (bench.yml) calls via `corpus-openmed` etc.,
# and openai-pf at ~80 s/doc would blow the PR budget. The openai-pf cell
# is pulled in only by `matrix` / `matrix-*` (see openai-pf-cells below).
# Presidio runs on every language (en + de + es spaCy models); if a given
# dev box lacks the matching model the cell warn-and-skips.
$(addprefix corpus-,$(ALL_CORPORA)): corpus-%: \
    $(CORPORA_DIR)/%/data/anonde_anonde-patterns.jsonl \
    $(CORPORA_DIR)/%/data/anonde_anonde-gliner.jsonl \
    $(CORPORA_DIR)/%/data/anonde_gliner-py.jsonl \
    $(CORPORA_DIR)/%/data/anonde_presidio.jsonl

# openai-pf cells, separated out so only the full-matrix targets depend on
# them. Each cell self-gates on RUN_OPENAI_PF (no-op + skip message when
# unset), so even when this prerequisite list is materialised the cost is
# zero unless RUN_OPENAI_PF=1 is passed.
.PHONY: openai-pf-cells $(addprefix openai-pf-,$(ALL_CORPORA))
openai-pf-cells: $(addprefix $(CORPORA_DIR)/,$(addsuffix /data/anonde_openai-pf.jsonl,$(ALL_CORPORA)))

# Per-corpus openai-pf convenience target — `make openai-pf-openmed
# RUN_OPENAI_PF=1`. Used by bench-full.yml so each matrix cell runs only
# its own corpus's openai-pf cell without overriding ALL_CORPORA.
$(addprefix openai-pf-,$(ALL_CORPORA)): openai-pf-%: \
    $(CORPORA_DIR)/%/data/anonde_openai-pf.jsonl

# Materialise the full matrix (four always-on engines + openai-pf), then
# render the combined report. Run openai-pf with the gate on:
#   make matrix RUN_OPENAI_PF=1
matrix: $(addprefix corpus-,$(ALL_CORPORA)) openai-pf-cells render

# Per-language subset matrices. Each depends on its corpora's always-on
# cells plus the openai-pf cells (self-gated on RUN_OPENAI_PF). FR / IT
# mirror ES exactly — Phase 1 populated FR_CORPORA / IT_CORPORA with the
# ai4privacy French / Italian slices.
matrix-de: $(addprefix corpus-,$(DE_CORPORA)) \
           $(addprefix $(CORPORA_DIR)/,$(addsuffix /data/anonde_openai-pf.jsonl,$(DE_CORPORA)))
	$(PYTHON) $(HERE)scoring/render_matrix.py \
	  --corpora-root $(CORPORA_DIR) \
	  --corpus "$(DE_CORPORA)" \
	  --engine "$(ENGINES)" \
	  --label-map $(HERE)scoring/label_map.yaml \
	  --out $(MATRIX_REPORT) \
	  --csv $(MATRIX_CSV)

matrix-en: $(addprefix corpus-,$(EN_CORPORA)) \
           $(addprefix $(CORPORA_DIR)/,$(addsuffix /data/anonde_openai-pf.jsonl,$(EN_CORPORA)))
	$(PYTHON) $(HERE)scoring/render_matrix.py \
	  --corpora-root $(CORPORA_DIR) \
	  --corpus "$(EN_CORPORA)" \
	  --engine "$(ENGINES)" \
	  --label-map $(HERE)scoring/label_map.yaml \
	  --out $(MATRIX_REPORT) \
	  --csv $(MATRIX_CSV)

matrix-es: $(addprefix corpus-,$(ES_CORPORA)) \
           $(addprefix $(CORPORA_DIR)/,$(addsuffix /data/anonde_openai-pf.jsonl,$(ES_CORPORA)))
	$(PYTHON) $(HERE)scoring/render_matrix.py \
	  --corpora-root $(CORPORA_DIR) \
	  --corpus "$(ES_CORPORA)" \
	  --engine "$(ENGINES)" \
	  --label-map $(HERE)scoring/label_map.yaml \
	  --out $(MATRIX_REPORT) \
	  --csv $(MATRIX_CSV)

matrix-fr: $(addprefix corpus-,$(FR_CORPORA)) \
           $(addprefix $(CORPORA_DIR)/,$(addsuffix /data/anonde_openai-pf.jsonl,$(FR_CORPORA)))
	$(PYTHON) $(HERE)scoring/render_matrix.py \
	  --corpora-root $(CORPORA_DIR) \
	  --corpus "$(FR_CORPORA)" \
	  --engine "$(ENGINES)" \
	  --label-map $(HERE)scoring/label_map.yaml \
	  --out $(MATRIX_REPORT) \
	  --csv $(MATRIX_CSV)

matrix-it: $(addprefix corpus-,$(IT_CORPORA)) \
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
