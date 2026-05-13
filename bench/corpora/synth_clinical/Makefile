# bench/corpora/synth_clinical — full-F1 bench against synthetic German
# clinical text across four sublanguages (ED triage, OP report, radiology,
# rehab).
#
# Gold spans are produced by the generator itself (slot-based), so this
# bench gives proper strict / partial / type-only F1 — not just a
# precision proxy like wiki_de or pmc_de.

HERE   := $(dir $(abspath $(lastword $(MAKEFILE_LIST))))
ROOT   := $(HERE)../../..
DATA   := $(HERE)data
CORPUS := $(DATA)/corpus.jsonl
ANONDE_OUT := $(DATA)/anonde.jsonl
REPORT := $(HERE)REPORT.md
CSV    := $(HERE)results.csv

ANONDE_BACKEND   ?= patterns-only
LANGUAGE         ?= de
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
	$(PYTHON) $(HERE)generate.py --out $@ --per-sublanguage $(PER_SUBLANGUAGE) --seed $(SEED)

data: $(CORPUS)

# Re-uses the unified runner_anonde from bench/runners (single source of truth).
.PHONY: anonde
anonde: $(CORPUS)
	cd $(ROOT) && go run $(GO_TAGS) ./bench/runners/anonde.go \
		--in $(CORPUS) --out $(ANONDE_OUT) \
		--backend $(ANONDE_BACKEND) --language $(LANGUAGE) \
		--model "$(ANONDE_MODEL)" --onnx-file "$(ANONDE_ONNX_FILE)" \
		--ner-score-floor $(NER_SCORE_FLOOR) \
		--reconciler $(RECONCILER) --ollama-model $(OLLAMA_MODEL) \
		--auditor $(AUDITOR) --auditor-model $(AUDITOR_MODEL)

# Re-uses compare.py + label_map.yaml from bench/scoring — gold labels
# match GraSCCo PHI conventions (NAME_PATIENT, LOCATION_HOSPITAL, etc.).
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
