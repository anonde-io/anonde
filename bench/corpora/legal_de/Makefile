# bench/corpora/legal_de — full-F1 bench against synthetic German legal
# text across five document types (Klageschrift, Beschluss, Vergleich,
# Vollmacht, Anwaltsschreiben).
#
# Gold spans are produced by the generator itself (slot-based), so this
# bench gives proper strict / partial / type-only F1. Purpose: probe
# whether anonde generalises from clinical text to legal prose.

HERE   := $(dir $(abspath $(lastword $(MAKEFILE_LIST))))
ROOT   := $(HERE)../../..
DATA   := $(HERE)data
CORPUS := $(DATA)/corpus.jsonl
ANONDE_OUT := $(DATA)/anonde.jsonl
REPORT := $(HERE)REPORT.md
CSV    := $(HERE)results.csv

ANONDE_BACKEND   ?= patterns-only
LANGUAGE         ?= de
PER_DOCTYPE      ?= 30
SEED             ?= 20260513
RECONCILER       ?= none
AUDITOR          ?= none
OLLAMA_MODEL     ?= llama3.2:3b
AUDITOR_MODEL    ?= llama3.1:8b
PYTHON           ?= python3
ANONDE_MODEL     ?=
ANONDE_ONNX_FILE ?=
NER_SCORE_FLOOR  ?= 0
# GLiNER label set for NER runs: chat|clinical|finance|legal. Threaded to
# the runner via --label-set. This corpus self-declares its domain
# (legal/administrative corpus); ignored on the patterns-only backend.
LABEL_SET        ?= legal

GO_TAGS := $(if $(filter hugot gliner,$(ANONDE_BACKEND)),-tags hugot)

.PHONY: all data anonde report clean

all: anonde report

$(CORPUS):
	mkdir -p $(DATA)
	$(PYTHON) $(HERE)generate.py --out $@ --per-doctype $(PER_DOCTYPE) --seed $(SEED)

data: $(CORPUS)

# Re-uses the unified runner_anonde from bench/runners (single source of truth).
.PHONY: anonde
anonde: $(CORPUS)
	cd $(ROOT) && go run $(GO_TAGS) ./bench/runners/anonde.go \
		--in $(CORPUS) --out $(ANONDE_OUT) \
		--backend $(ANONDE_BACKEND) --language $(LANGUAGE) \
		--model "$(ANONDE_MODEL)" --onnx-file "$(ANONDE_ONNX_FILE)" \
		--ner-score-floor $(NER_SCORE_FLOOR) \
		--label-set $(LABEL_SET) \
		--ollama-model $(OLLAMA_MODEL)

# Re-uses compare.py + label_map.yaml from bench/scoring — gold labels
# follow the same GraSCCo-style conventions used by synth_clinical so the
# existing label_map mappings work without changes.
.PHONY: report
report:
	$(PYTHON) $(ROOT)/bench/scoring/compare.py \
		--gold $(CORPUS) \
		--engine anonde=$(ANONDE_OUT) \
		--label-map $(ROOT)/bench/scoring/label_map.yaml \
		--out $(REPORT) \
		--csv $(CSV)

clean:
	rm -f $(CORPUS) $(ANONDE_OUT) $(REPORT) $(CSV)
