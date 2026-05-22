# bench/corpora/synth_logs — full-F1 bench against synthetic enterprise
# log text across four log types (auth logs, application error logs,
# HTTP access logs, audit trails).
#
# Phase 3 of the multilingual bench expansion. Gold spans are produced
# by the generator itself (slot-based), so this bench gives proper
# strict / partial / type-only F1.
#
# The log scaffolding is English; embedded PII (names, addresses) is
# sampled across EN/DE/ES/FR/IT locales — realistic for a global SaaS.
# The corpus is classified as ENGLISH in bench/Makefile (the scaffolding
# language; there is no single dominant PII language).
#
# SECRET handling: API keys / JWTs / bearer tokens / OAuth secrets are
# gold-tagged `SECRET`, which maps to `~` (dropped from scoring) in
# bench/scoring/label_map.yaml — anonde ships no secret recognizer yet.
# The spans remain in the gold JSONL so a future phase can score them.

HERE   := $(dir $(abspath $(lastword $(MAKEFILE_LIST))))
ROOT   := $(HERE)../../..
DATA   := $(HERE)data
CORPUS := $(DATA)/corpus.jsonl
ANONDE_OUT := $(DATA)/anonde.jsonl
REPORT := $(HERE)REPORT.md
CSV    := $(HERE)results.csv

ANONDE_BACKEND   ?= patterns-only
LANGUAGE         ?= en
PER_LOGTYPE      ?= 30
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
	$(PYTHON) $(HERE)generate.py --out $@ --per-logtype $(PER_LOGTYPE) --seed $(SEED)

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

# Re-uses compare.py + label_map.yaml from bench/scoring. The generator
# emits canonical label_map types directly (EMAIL, PERSON, URL, PHONE,
# ID) plus SECRET (which label_map maps to `~`, dropping it from scoring).
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
