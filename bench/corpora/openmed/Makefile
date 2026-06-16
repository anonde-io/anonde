# bench/corpora/openmed — German PHI recall bench against GraSCCo_PHI.
#
# Targets:
#   make data       # fetch + load GraSCCo_PHI -> data/corpus.jsonl
#   make anonde     # run anonde -> data/anonde.jsonl
#   make report     # compare -> REPORT.md + results.csv
#   make all        # everything

HERE   := $(dir $(abspath $(lastword $(MAKEFILE_LIST))))
ROOT   := $(HERE)../../..
DATA   := $(HERE)data
CORPUS := $(DATA)/corpus.jsonl
ANONDE_OUT := $(DATA)/anonde.jsonl
REPORT := $(HERE)REPORT.md
CSV    := $(HERE)results.csv

GRASCCO_URL := https://zenodo.org/records/11502329/files/grascco_phi_annotation_json.zip
GRASCCO_ZIP := $(DATA)/grascco_phi_annotation_json.zip

ANONDE_BACKEND   ?= patterns-only
LANGUAGE         ?= de
PYTHON           ?= python3
# NER backend knobs — only meaningful when ANONDE_BACKEND=gliner.
# Empty ANONDE_MODEL keeps the recognizer-package default
# (onnx-community/multilang-pii-ner-ONNX, int8). Override to bench
# alternative backends (GLiNER, ai4privacy fine-tunes, …) without code
# changes. ANONDE_ONNX_FILE picks a specific ONNX file inside the HF repo
# when it ships multiple variants.
ANONDE_MODEL     ?=
ANONDE_ONNX_FILE ?=
# 0 = recognizer default; negative = disabled. Per-model tuning happens here.
# GLiNER label set for NER runs: chat|clinical|finance|legal. Threaded to
# the runner via --label-set. This corpus self-declares its domain
# (clinical/PHI corpus); ignored on the patterns-only backend.
LABEL_SET        ?= clinical

# Only build the NER transitive deps when the backend actually needs them.
GO_TAGS := $(if $(filter gliner,$(ANONDE_BACKEND)),-tags ner)

.PHONY: all data anonde report clean

all: anonde report

$(GRASCCO_ZIP):
	mkdir -p $(DATA)
	curl -L -o $@ $(GRASCCO_URL)

$(CORPUS): $(GRASCCO_ZIP)
	$(PYTHON) $(HERE)loader_grascco.py --in $(GRASCCO_ZIP) --out $@

data: $(CORPUS)

.PHONY: anonde
anonde: $(CORPUS)
	cd $(ROOT) && go run $(GO_TAGS) ./bench/runners/anonde.go \
		--in $(CORPUS) --out $(ANONDE_OUT) \
		--backend $(ANONDE_BACKEND) --language $(LANGUAGE) \
		--model "$(ANONDE_MODEL)" --onnx-file "$(ANONDE_ONNX_FILE)" \
		--label-set $(LABEL_SET)

.PHONY: report
report:
	$(PYTHON) $(ROOT)/bench/scoring/compare.py \
		--gold $(CORPUS) \
		--engine anonde=$(ANONDE_OUT) \
		--label-map $(ROOT)/bench/scoring/label_map.yaml \
		--out $(REPORT) \
		--csv $(CSV)

clean:
	rm -f $(ANONDE_OUT) $(REPORT) $(CSV)
