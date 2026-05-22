# bench/corpora/synth_clinical_it — Italian slice of the synthetic
# clinical bench (Phase 4 of the multilingual bench expansion).
#
# Thin wrapper: the generator, templates and vocab are SHARED with every
# synth_clinical_* corpus and live in ../synth_clinical/. This Makefile
# only differs in LANGUAGE. Mirrors the synth_finance_* / ai4privacy_*
# shared-loader pattern. See ../synth_clinical/README.md for the full
# design.

HERE   := $(dir $(abspath $(lastword $(MAKEFILE_LIST))))
ROOT   := $(HERE)../../..
DATA   := $(HERE)data
CORPUS := $(DATA)/corpus.jsonl
ANONDE_OUT := $(DATA)/anonde.jsonl
REPORT := $(HERE)REPORT.md
CSV    := $(HERE)results.csv

# Shared generator — single source of truth for every synth_clinical_*
# corpus. Lives in synth_clinical/; this corpus only changes LANGUAGE.
GENERATOR := $(HERE)../synth_clinical/generate.py

ANONDE_BACKEND   ?= patterns-only
LANGUAGE         ?= it
PER_SUBLANGUAGE  ?= 30
SEED             ?= 20260512
RECONCILER       ?= none
AUDITOR          ?= none
OLLAMA_MODEL     ?= llama3.2:3b
AUDITOR_MODEL    ?= llama3.1:8b
PYTHON           ?= python3
ANONDE_MODEL     ?=
ANONDE_ONNX_FILE ?=
NER_SCORE_FLOOR  ?= 0

GO_TAGS := $(if $(filter hugot gliner,$(ANONDE_BACKEND)),-tags hugot)

.PHONY: all data anonde report clean

all: anonde report

$(CORPUS):
	mkdir -p $(DATA)
	$(PYTHON) $(GENERATOR) --out $@ --language $(LANGUAGE) \
		--per-sublanguage $(PER_SUBLANGUAGE) --seed $(SEED)

data: $(CORPUS)

.PHONY: anonde
anonde: $(CORPUS)
	cd $(ROOT) && go run $(GO_TAGS) ./bench/runners/anonde.go \
		--in $(CORPUS) --out $(ANONDE_OUT) \
		--backend $(ANONDE_BACKEND) --language $(LANGUAGE) \
		--model "$(ANONDE_MODEL)" --onnx-file "$(ANONDE_ONNX_FILE)" \
		--ner-score-floor $(NER_SCORE_FLOOR) \
		--reconciler $(RECONCILER) --ollama-model $(OLLAMA_MODEL) \
		--auditor $(AUDITOR) --auditor-model $(AUDITOR_MODEL)

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
