# bench/corpora/ai4privacy_es — Spanish PII bench on the ai4privacy
# multilingual corpus. Phase 1 of the multilingual bench expansion.
#
# This corpus is a thin wrapper: the dataset, the loader, and the gold
# schema are identical to ai4privacy_en — only the language slice
# differs. The shared loader lives at
# ../ai4privacy_en/cmd/fetch_pii_masking.py and is parametrised by
# `--language es`.
#
# Targets:
#   make data        # fetch ai4privacy es slice -> data/corpus.jsonl
#   make anonde      # run anonde -> data/anonde.jsonl
#   make presidio    # run Presidio -> data/presidio.jsonl  (needs es_core_news_lg)
#   make report      # compare -> REPORT.md + results.csv
#   make all         # everything

HERE   := $(dir $(abspath $(lastword $(MAKEFILE_LIST))))
ROOT   := $(HERE)../../..
DATA   := $(HERE)data
CORPUS := $(DATA)/corpus.jsonl
ANONDE_OUT := $(DATA)/anonde.jsonl
PRESIDIO_OUT := $(DATA)/presidio.jsonl
REPORT := $(HERE)REPORT.md
CSV    := $(HERE)results.csv

# Shared loader — single source of truth for every ai4privacy_* corpus.
LOADER := $(HERE)../ai4privacy_en/cmd/fetch_pii_masking.py

ANONDE_BACKEND   ?= patterns-only
LANGUAGE         ?= es
MAX_DOCS         ?= 5000
PYTHON           ?= python3
ANONDE_MODEL     ?=
ANONDE_ONNX_FILE ?=
NER_SCORE_FLOOR  ?= 0
# GLiNER label set for NER runs: chat|clinical|finance|legal. Threaded to
# the runner via --label-set. This corpus self-declares its domain
# (general corpus; chat is the global default); ignored on the patterns-only backend.
LABEL_SET        ?= chat
PRESIDIO_ENGINE  ?= default

GO_TAGS := $(if $(filter hugot gliner,$(ANONDE_BACKEND)),-tags hugot)

.PHONY: all data anonde presidio report clean

all: anonde report

$(CORPUS):
	mkdir -p $(DATA)
	$(PYTHON) $(LOADER) --out $@ --max $(MAX_DOCS) --language $(LANGUAGE)

data: $(CORPUS)

# Re-uses the unified runner_anonde from bench/runners. --fold-parity-labels
# folds STREET_ADDRESS + POSTAL_CODE to LOCATION since ai4privacy gold
# buckets street + zip under LOCATION (same in every language slice).
.PHONY: anonde
anonde: $(CORPUS)
	cd $(ROOT) && go run $(GO_TAGS) ./bench/runners/anonde.go \
		--in $(CORPUS) --out $(ANONDE_OUT) \
		--backend $(ANONDE_BACKEND) --language $(LANGUAGE) \
		--model "$(ANONDE_MODEL)" --onnx-file "$(ANONDE_ONNX_FILE)" \
		--ner-score-floor $(NER_SCORE_FLOOR) \
		--label-set $(LABEL_SET) \
		--fold-parity-labels

.PHONY: presidio
presidio: $(CORPUS)
	$(PYTHON) $(ROOT)/bench/runners/presidio.py \
		--in $(CORPUS) --out $(PRESIDIO_OUT) \
		--language $(LANGUAGE) \
		--engine $(PRESIDIO_ENGINE)

# Strict-mode entity compare (uses bench/scoring/compare.py with the
# `presidio` engine name → maps Presidio's labels through label_map.yaml).
.PHONY: report
report:
	$(PYTHON) $(ROOT)/bench/scoring/compare.py \
		--gold $(CORPUS) \
		--engine anonde=$(ANONDE_OUT) \
		$(if $(wildcard $(PRESIDIO_OUT)),--engine presidio=$(PRESIDIO_OUT),) \
		--label-map $(ROOT)/bench/scoring/label_map.yaml \
		--out $(REPORT) \
		--csv $(CSV)

clean:
	rm -f $(ANONDE_OUT) $(PRESIDIO_OUT) $(REPORT) $(CSV)
