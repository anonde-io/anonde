# bench/corpora/ggponc_de — precision probe against the German Guideline
# Program in Oncology corpus. Unlike the other benches, this one needs the
# user to register for GGPONC 2.0 access and drop the extracted files
# under data/raw/ first.

HERE   := $(dir $(abspath $(lastword $(MAKEFILE_LIST))))
ROOT   := $(HERE)../../..
DATA   := $(HERE)data
RAW    := $(DATA)/raw
CORPUS := $(DATA)/corpus.jsonl
ANONDE_OUT := $(DATA)/anonde.jsonl
REPORT := $(HERE)REPORT.md
CSV    := $(HERE)results.csv

ANONDE_BACKEND   ?= patterns-only
LANGUAGE         ?= de
PYTHON           ?= python3
ANONDE_MODEL     ?=
ANONDE_ONNX_FILE ?=
NER_SCORE_FLOOR  ?= 0

GO_TAGS := $(if $(filter hugot gliner,$(ANONDE_BACKEND)),-tags hugot)

.PHONY: all check-raw data anonde report clean

all: anonde report

check-raw:
	@if [ ! -d $(RAW) ] || [ -z "$$(ls -A $(RAW) 2>/dev/null)" ]; then \
	  echo ""; \
	  echo "GGPONC corpus not found at $(RAW)/"; \
	  echo ""; \
	  echo "  1. Register at https://www.leitlinienprogramm-onkologie.de/projekte/ggponc-english"; \
	  echo "  2. Extract the archive into $(RAW)/"; \
	  echo "  3. Re-run make"; \
	  echo ""; \
	  exit 1; \
	fi

$(CORPUS): check-raw
	mkdir -p $(DATA)
	$(PYTHON) $(HERE)loader.py --in $(RAW) --out $@

data: $(CORPUS)

.PHONY: anonde
anonde: $(CORPUS)
	cd $(ROOT) && go run $(GO_TAGS) ./bench/runners/anonde.go \
		--in $(CORPUS) --out $(ANONDE_OUT) \
		--backend $(ANONDE_BACKEND) --language $(LANGUAGE) \
		--model "$(ANONDE_MODEL)" --onnx-file "$(ANONDE_ONNX_FILE)" \
		--ner-score-floor $(NER_SCORE_FLOOR) \
		--reconciler none --auditor none

# Re-uses the precision-probe analyser from the wiki_de corpus.
.PHONY: report
report:
	$(PYTHON) $(HERE)../wiki_de/analyze.py \
		--corpus $(CORPUS) \
		--anonde $(ANONDE_OUT) \
		--out $(REPORT) \
		--csv $(CSV) \
		--title "GGPONC 2.0 precision probe" \
		--source "German oncology guideline documents"

clean:
	rm -f $(CORPUS) $(ANONDE_OUT) $(REPORT) $(CSV)
