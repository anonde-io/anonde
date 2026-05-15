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
#   make matrix-de   skip English corpora
#   make matrix-en   skip German corpora
#   make data        fetch/generate every corpus (no engine runs)
#   make clean       remove per-corpus data + the matrix outputs
#
# Tuning knobs forwarded to each per-corpus Makefile:
#   ANONDE_BACKEND       which anonde mode (patterns-only|hugot|gliner)
#   ANONDE_MODEL         override default model id for the active backend
#   ANONDE_ONNX_FILE     override default ONNX file inside the HF repo
#   NER_SCORE_FLOOR      drop NER predictions below this score
#   GLINER_MODEL         model id for the GLiNER cells (default: PII base)
#   GLINER_THRESHOLD     GLiNER threshold (default: 0.40)
#   HUGOT_MODEL          model id for the hugot cells (default: multilang PII)
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
# OpenAI Privacy Filter — first run pulls ~2.8 GB safetensors from HF Hub.
# Point at a pre-downloaded snapshot dir to skip the download.
OPENAI_PF_MODEL  ?= openai/privacy-filter

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
              wikiann_de germeval_14 conll2003_de adversarial_de
EN_CORPORA := ai4privacy_en conll2003_en wnut_17
ES_CORPORA := pharmaconer_es
ALL_CORPORA := $(DE_CORPORA) $(EN_CORPORA) $(ES_CORPORA)

# The five engines the matrix scores. Each row's name is what compare.py
# routes through the label_map (so prefixes matter — see compare.py).
ENGINES := anonde-patterns anonde-gliner presidio gliner-py openai-pf
# anonde-hugot is dropped from the auto-matrix because its XLM-R PII model
# is ~25,000x slower than patterns and the openmed cell already proved it
# strictly underperforms anonde-gliner (F1 0.473 vs 0.529 on GraSCCo).
# openai-pf is in the ENGINES list (so render_matrix.py picks up its
# existing openmed/pmc_de cells) but excluded from the corpus-* auto deps
# because at ~80 sec/doc on CPU it makes large corpora untractable (160+
# min for synth_clinical alone). To bench it on a new corpus:
#   make bench/corpora/<NAME>/data/anonde_openai-pf.jsonl
# Run hugot manually similarly:
#   make -C bench/corpora/<NAME> all ANONDE_BACKEND=hugot

MATRIX_REPORT := $(HERE)REPORT_MATRIX.md
MATRIX_CSV    := $(HERE)results_matrix.csv

.PHONY: help data matrix matrix-de matrix-en clean \
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
	@echo "  make corpus-<NAME>      run every engine on a single corpus (NAME e.g. openmed)"
	@echo "  make clean              remove generated data + reports + the matrix outputs"
	@echo ""
	@echo "Corpora (German):  $(DE_CORPORA)"
	@echo "Corpora (English): $(EN_CORPORA)"
	@echo "Corpora (Spanish): $(ES_CORPORA)"
	@echo "Engines: $(ENGINES)"
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

define LANG_FOR_CORPUS
$(if $(filter $(1),$(EN_CORPORA)),en,$(if $(filter $(1),$(ES_CORPORA)),es,de))
endef

define FOLD_FOR_CORPUS
$(if $(filter $(1),$(EN_CORPORA)),--fold-parity-labels,)
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

# presidio cell: English only; on German corpora we skip.
define CELL_presidio
# NOT .PHONY: cache existing per-cell outputs so partial matrices don't re-run.
# data-$(1) is .PHONY which triggers make's "if newer than target" check,
# but the data target itself is a no-op if data/corpus.jsonl exists, so the
# cell's output mtime usually wins → cell skipped. Force a re-run by
# `rm bench/corpora/<c>/data/anonde_<engine>.jsonl` before `make matrix`.
$(CORPORA_DIR)/$(1)/data/anonde_presidio.jsonl: | data-$(1)
	@echo "[matrix] $(1) x presidio"
	$(PYTHON) $(ROOT)/bench/runners/presidio.py \
	  --in  $(CORPORA_DIR)/$(1)/data/corpus.jsonl \
	  --out $(CORPORA_DIR)/$(1)/data/anonde_presidio.jsonl || \
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

# openai-pf cell: OpenAI Privacy Filter — 128-expert MoE token classifier
# released April 2026. Included as the "best in market" baseline from a
# major commercial vendor. English-trained; weak on German clinical text
# in earlier benches (98% leak on GraSCCo) — kept in the matrix for the
# multilingual comparison story. First run downloads ~2.8 GB safetensors
# into ~/.cache/huggingface; override the path with OPENAI_PF_MODEL_PATH.
# Cell tolerates failure: a `|| true` on the runner exit lets render_matrix
# show the row as missing rather than aborting the whole matrix.
define CELL_openai_pf
# NOT .PHONY: cache existing per-cell outputs so partial matrices don't re-run.
# data-$(1) is .PHONY which triggers make's "if newer than target" check,
# but the data target itself is a no-op if data/corpus.jsonl exists, so the
# cell's output mtime usually wins → cell skipped. Force a re-run by
# `rm bench/corpora/<c>/data/anonde_<engine>.jsonl` before `make matrix`.
$(CORPORA_DIR)/$(1)/data/anonde_openai-pf.jsonl: | data-$(1)
	@echo "[matrix] $(1) x openai-pf"
	$(PYTHON) $(ROOT)/bench/runners/openai_pf.py \
	  --in  $(CORPORA_DIR)/$(1)/data/corpus.jsonl \
	  --out $(CORPORA_DIR)/$(1)/data/anonde_openai-pf.jsonl \
	  --model "$(OPENAI_PF_MODEL)" \
	  --engine-label openai-pf || \
	  echo "[matrix] WARN: openai-pf cell for $(1) failed — render will show as missing"
endef

# Expand per-corpus, per-engine. Presidio only runs on English corpora —
# the spaCy/en_core_web_lg model is English-only.
$(foreach c,$(ALL_CORPORA),\
  $(foreach e,anonde-patterns anonde-gliner,\
    $(eval $(call CELL_anonde,$(c),$(e))))\
  $(eval $(call CELL_gliner_py,$(c)))\
  $(eval $(call CELL_openai_pf,$(c))))

$(foreach c,$(EN_CORPORA),\
  $(eval $(call CELL_presidio,$(c))))

# Per-corpus convenience target — run every engine for one corpus.
$(addprefix corpus-,$(DE_CORPORA)): corpus-%: \
    $(CORPORA_DIR)/%/data/anonde_anonde-patterns.jsonl \
    $(CORPORA_DIR)/%/data/anonde_anonde-gliner.jsonl \
    $(CORPORA_DIR)/%/data/anonde_gliner-py.jsonl

$(addprefix corpus-,$(EN_CORPORA)): corpus-%: \
    $(CORPORA_DIR)/%/data/anonde_anonde-patterns.jsonl \
    $(CORPORA_DIR)/%/data/anonde_anonde-gliner.jsonl \
    $(CORPORA_DIR)/%/data/anonde_gliner-py.jsonl \
    $(CORPORA_DIR)/%/data/anonde_presidio.jsonl

# Spanish corpora: Presidio's default model is English-only, so we skip
# Presidio for ES. PharmaCoNER is also a precision probe (no PHI gold),
# so the bench cell measures false-positive count, not leak rate.
$(addprefix corpus-,$(ES_CORPORA)): corpus-%: \
    $(CORPORA_DIR)/%/data/anonde_anonde-patterns.jsonl \
    $(CORPORA_DIR)/%/data/anonde_anonde-gliner.jsonl \
    $(CORPORA_DIR)/%/data/anonde_gliner-py.jsonl

# Materialise the full matrix, then render the combined report.
matrix: $(addprefix corpus-,$(ALL_CORPORA)) render

matrix-de: $(addprefix corpus-,$(DE_CORPORA))
	$(PYTHON) $(HERE)scoring/render_matrix.py \
	  --corpora-root $(CORPORA_DIR) \
	  --corpus $(DE_CORPORA) \
	  --engine $(ENGINES) \
	  --label-map $(HERE)scoring/label_map.yaml \
	  --out $(MATRIX_REPORT) \
	  --csv $(MATRIX_CSV)

matrix-en: $(addprefix corpus-,$(EN_CORPORA))
	$(PYTHON) $(HERE)scoring/render_matrix.py \
	  --corpora-root $(CORPORA_DIR) \
	  --corpus $(EN_CORPORA) \
	  --engine $(ENGINES) \
	  --label-map $(HERE)scoring/label_map.yaml \
	  --out $(MATRIX_REPORT) \
	  --csv $(MATRIX_CSV)

matrix-es: $(addprefix corpus-,$(ES_CORPORA))
	$(PYTHON) $(HERE)scoring/render_matrix.py \
	  --corpora-root $(CORPORA_DIR) \
	  --corpus $(ES_CORPORA) \
	  --engine $(ENGINES) \
	  --label-map $(HERE)scoring/label_map.yaml \
	  --out $(MATRIX_REPORT) \
	  --csv $(MATRIX_CSV)

.PHONY: render
render:
	$(PYTHON) $(HERE)scoring/render_matrix.py \
	  --corpora-root $(CORPORA_DIR) \
	  --corpus $(ALL_CORPORA) \
	  --engine $(ENGINES) \
	  --label-map $(HERE)scoring/label_map.yaml \
	  --out $(MATRIX_REPORT) \
	  --csv $(MATRIX_CSV)

clean:
	@for c in $(ALL_CORPORA); do \
	  $(MAKE) -s -C $(CORPORA_DIR)/$$c clean || true; \
	done
	rm -f $(MATRIX_REPORT) $(MATRIX_CSV)
