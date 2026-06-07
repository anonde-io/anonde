# bench/corpora/synth_finance_en — full-F1 bench against synthetic
# English financial text across four document types (invoice, bank
# statement, KYC/onboarding record, transaction confirmation).
#
# Phase 3 of the multilingual bench expansion. This directory is the
# CANONICAL home of the shared finance generator (generate.py +
# generators.py + templates.py); the sibling corpora
# synth_finance_{de,es,fr,it} are thin Makefile wrappers that invoke
# this generator with a different LANGUAGE (mirrors the ai4privacy_*
# shared-loader pattern).
#
# Gold spans are produced by the generator itself (slot-based), so this
# bench gives proper strict / partial / type-only F1. Unlike
# bench/corpora/finance_de, gold types are the CANONICAL label_map
# vocabulary (PERSON, IBAN, ID, ADDRESS, ORGANIZATION, DATE, EMAIL,
# PHONE) — IBAN is therefore scored as canonical IBAN.

HERE   := $(dir $(abspath $(lastword $(MAKEFILE_LIST))))
ROOT   := $(HERE)../../..
DATA   := $(HERE)data
CORPUS := $(DATA)/corpus.jsonl
ANONDE_OUT := $(DATA)/anonde.jsonl
REPORT := $(HERE)REPORT.md
CSV    := $(HERE)results.csv

# Shared generator — single source of truth for every synth_finance_*
# corpus. The de/es/fr/it wrappers point GENERATOR at this same file.
GENERATOR := $(HERE)generate.py

ANONDE_BACKEND   ?= patterns-only
LANGUAGE         ?= en
PER_DOCTYPE      ?= 30
SEED             ?= 20260512
RECONCILER       ?= none
AUDITOR          ?= none
OLLAMA_MODEL     ?= llama3.2:3b
AUDITOR_MODEL    ?= llama3.1:8b
PYTHON           ?= python3
ANONDE_MODEL     ?=
ANONDE_ONNX_FILE ?=
NER_SCORE_FLOOR  ?= 0
# GLiNER label set for NER runs: chat|clinical|finance|legal. Threaded to
# the runner via --label-set. Default finance (this is a financial corpus);
# ignored on the patterns-only backend.
LABEL_SET        ?= finance

GO_TAGS := $(if $(filter hugot gliner,$(ANONDE_BACKEND)),-tags hugot)

.PHONY: all data anonde report clean

all: anonde report

$(CORPUS):
	mkdir -p $(DATA)
	$(PYTHON) $(GENERATOR) --out $@ --language $(LANGUAGE) \
		--per-doctype $(PER_DOCTYPE) --seed $(SEED)

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

# Re-uses compare.py + label_map.yaml from bench/scoring. The generator
# emits canonical label_map types directly (PERSON, IBAN, ID, ADDRESS,
# ORGANIZATION, DATE, EMAIL, PHONE), so the gold side is a pass-through.
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
