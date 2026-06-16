# bench/corpora/pmc_de — precision probe against German clinical case
# reports from PubMed Central Open Access. No HuggingFace, no DUA, no API
# key — just NCBI E-utilities over plain HTTP.

HERE   := $(dir $(abspath $(lastword $(MAKEFILE_LIST))))
ROOT   := $(HERE)../../..
DATA   := $(HERE)data
CORPUS := $(DATA)/corpus.jsonl
ANONDE_OUT := $(DATA)/anonde.jsonl
REPORT := $(HERE)REPORT.md
CSV    := $(HERE)results.csv

ANONDE_BACKEND   ?= patterns-only
LANGUAGE         ?= de
MAX_DOCS         ?= 200
PYTHON           ?= python3
ANONDE_MODEL     ?=
ANONDE_ONNX_FILE ?=
# GLiNER label set for NER runs: chat|clinical|finance|legal. Threaded to
# the runner via --label-set. This corpus self-declares its domain
# (clinical/PHI corpus); ignored on the patterns-only backend.
LABEL_SET        ?= clinical

GO_TAGS := $(if $(filter gliner,$(ANONDE_BACKEND)),-tags ner)

.PHONY: all data anonde report clean

all: anonde report

$(CORPUS):
	mkdir -p $(DATA)
	$(PYTHON) $(HERE)download.py --out $@ --max $(MAX_DOCS)

data: $(CORPUS)

# Re-uses the unified runner_anonde from bench/runners (single source of truth).
.PHONY: anonde
anonde: $(CORPUS)
	cd $(ROOT) && go run $(GO_TAGS) ./bench/runners/anonde.go \
		--in $(CORPUS) --out $(ANONDE_OUT) \
		--backend $(ANONDE_BACKEND) --language $(LANGUAGE) \
		--model "$(ANONDE_MODEL)" --onnx-file "$(ANONDE_ONNX_FILE)" \
		--label-set $(LABEL_SET)

# Re-uses the precision-probe analyser from wiki_de (also generic).
.PHONY: report
report:
	$(PYTHON) $(HERE)../wiki_de/analyze.py \
		--corpus $(CORPUS) \
		--anonde $(ANONDE_OUT) \
		--out $(REPORT) \
		--csv $(CSV) \
		--title "PubMed Central German case-report precision probe" \
		--source "German clinical case reports from PMC Open Access"

clean:
	rm -f $(CORPUS) $(ANONDE_OUT) $(REPORT) $(CSV)
