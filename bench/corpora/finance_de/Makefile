# bench/corpora/finance_de — full-F1 bench against synthetic German
# financial text across five document types (Kontoauszug,
# Überweisungsauftrag, Kreditantrag, Depot-Auszug, KYC-Anfrage).
#
# Gold spans are produced by the generator itself (slot-based), so this
# bench gives proper strict / partial / type-only F1 — like its sibling
# bench/corpora/synth_clinical — but exercises a non-clinical sublanguage:
# the answer to "does anonde generalise beyond clinical text?".

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
		--reconciler $(RECONCILER) --ollama-model $(OLLAMA_MODEL) \
		--auditor $(AUDITOR) --auditor-model $(AUDITOR_MODEL)

# Re-uses compare.py + label_map.yaml from bench/scoring — gold labels
# reuse the GraSCCo PHI vocabulary (NAME_PATIENT, CONTACT_PHONE, …) since
# those are the only keys the gold section of label_map.yaml knows. See
# this corpus's README for the IBAN/Steuer-ID-vs-ID labelling trade-off.
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
