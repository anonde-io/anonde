# bench/corpora/meddocan_es — real-gold Spanish clinical de-identification
# bench on the MEDDOCAN corpus (IberLEF 2019 shared task).
#
# Phase 4 of the multilingual bench expansion. MEDDOCAN is the canonical
# Spanish clinical PHI gold — 1,000 manually annotated clinical case
# reports, 22 PHI entity types. (pharmaconer_es is a chemical/drug
# corpus, NOT a PHI-recall corpus.)
#
# DATA SOURCE — public, unauthenticated. MEDDOCAN is openly distributed
# (CC-BY-4.0) as a Zenodo deposit; `make data` curls it. The loader is
# nonetheless forward-compatible: if the archive is absent (Zenodo
# unreachable, offline CI, or a future move behind registration) the
# loader exits 2 cleanly and the bench harness skips the cell — the same
# contract ggponc_de / conll2003_de use.
#
# Targets:
#   make data       # fetch + load MEDDOCAN test split -> data/corpus.jsonl
#   make anonde     # run anonde -> data/anonde.jsonl
#   make presidio   # run Presidio -> data/presidio.jsonl  (needs es spaCy model)
#   make report     # compare -> REPORT.md + results.csv
#   make all        # everything

HERE   := $(dir $(abspath $(lastword $(MAKEFILE_LIST))))
ROOT   := $(HERE)../../..
DATA   := $(HERE)data
RAW    := $(DATA)/raw
CORPUS := $(DATA)/corpus.jsonl
ANONDE_OUT   := $(DATA)/anonde.jsonl
PRESIDIO_OUT := $(DATA)/presidio.jsonl
REPORT := $(HERE)REPORT.md
CSV    := $(HERE)results.csv

MEDDOCAN_URL := https://zenodo.org/records/4279323/files/meddocan.zip
MEDDOCAN_ZIP := $(RAW)/meddocan.zip

ANONDE_BACKEND   ?= patterns-only
LANGUAGE         ?= es
SPLIT            ?= test
PYTHON           ?= python3
ANONDE_MODEL     ?=
ANONDE_ONNX_FILE ?=
NER_SCORE_FLOOR  ?= 0
# GLiNER label set for NER runs: chat|clinical|finance|legal. Threaded to
# the runner via --label-set. This corpus self-declares its domain
# (clinical/PHI corpus); ignored on the patterns-only backend.
LABEL_SET        ?= clinical
PRESIDIO_ENGINE  ?= default

GO_TAGS := $(if $(filter hugot gliner,$(ANONDE_BACKEND)),-tags hugot)

.PHONY: all data anonde presidio report clean

all: anonde report

# Fetch the MEDDOCAN archive from Zenodo. `curl -f` makes a non-200
# response fail the rule rather than writing an HTML error page; the
# loader then sees no zip and exits 2 cleanly. -C - resumes a partial
# download on re-run.
$(MEDDOCAN_ZIP):
	mkdir -p $(RAW)
	curl -fL -C - -o $@ $(MEDDOCAN_URL) || \
	  echo "meddocan_es: Zenodo fetch failed — loader will skip (exit 2)"

$(CORPUS): $(MEDDOCAN_ZIP)
	mkdir -p $(DATA)
	$(PYTHON) $(HERE)loader.py --in $(MEDDOCAN_ZIP) --out $@ --split $(SPLIT)

data: $(CORPUS)

# Re-uses the unified runner_anonde from bench/runners (single source of
# truth). No --fold-parity-labels: MEDDOCAN's gold keeps CALLE (street)
# as ADDRESS and TERRITORIO (city/region) as LOCATION — the loader
# pre-maps to those canonical types, so anonde's STREET_ADDRESS ->
# ADDRESS and city -> LOCATION already line up without folding.
.PHONY: anonde
anonde: $(CORPUS)
	cd $(ROOT) && go run $(GO_TAGS) ./bench/runners/anonde.go \
		--in $(CORPUS) --out $(ANONDE_OUT) \
		--backend $(ANONDE_BACKEND) --language $(LANGUAGE) \
		--model "$(ANONDE_MODEL)" --onnx-file "$(ANONDE_ONNX_FILE)" \
		--ner-score-floor $(NER_SCORE_FLOOR) \
		--label-set $(LABEL_SET)

.PHONY: presidio
presidio: $(CORPUS)
	$(PYTHON) $(ROOT)/bench/runners/presidio.py \
		--in $(CORPUS) --out $(PRESIDIO_OUT) \
		--language $(LANGUAGE) \
		--engine $(PRESIDIO_ENGINE)

# Strict / partial / type-only entity compare (uses bench/scoring/compare.py).
# Gold types are already canonical (the loader pre-maps MEDDOCAN's 22 PHI
# labels) — the gold: section of label_map.yaml documents the mapping.
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
	rm -f $(CORPUS) $(ANONDE_OUT) $(PRESIDIO_OUT) $(REPORT) $(CSV)
	rm -rf $(RAW)/_extracted
