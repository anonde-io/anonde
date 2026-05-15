# bench/corpora/pharmaconer_es — Spanish clinical NER as a precision probe.
#
# PharmaCoNER is the IberLEF 2019 shared-task corpus: 1000 manually
# annotated Spanish clinical case reports with gold spans for
# pharmacological substances, chemical compounds and proteins. The
# annotation has NO PERSON / LOCATION / ORGANIZATION layer because the
# source documents are de-identified before publication.
#
# Why we include it: anonde's bench was DE + EN heavy. PharmaCoNER fills
# the Spanish gap with **real clinical prose** (not synthetic) and
# answers a different question than the recall benches: "in a
# language anonde wasn't tuned on, with chemical names that look
# uppercase-ish, does anonde over-fire?". Concretely a precision probe
# in the same family as wiki_de and pmc_de.
#
# Targets mirror wikiann_de's layout so the top-level bench/Makefile can
# call `data-pharmaconer_es` and `corpus-pharmaconer_es` uniformly. The
# parent registers this corpus by adding it to its corpus list — this
# Makefile must not be modified to do so.

HERE   := $(dir $(abspath $(lastword $(MAKEFILE_LIST))))
DATA   := $(HERE)data
CORPUS := $(DATA)/corpus.jsonl

# How many docs to sample. PharmaCoNER docs are paragraph-length
# clinical case reports (~1.5-2 kB each), so 200 docs ~= 350 kB total
# text — a few minutes total bench wall-clock across patterns + gliner
# + gliner-py. Deterministic via SEED so every engine scores the same
# slice.
N_DOCS ?= 200
SEED   ?= 20260515
PYTHON ?= python3

.PHONY: all data clean

all: data

$(CORPUS):
	mkdir -p $(DATA)
	$(PYTHON) $(HERE)loader.py --out $(CORPUS) --n $(N_DOCS) --seed $(SEED)

data: $(CORPUS)

clean:
	rm -f $(CORPUS)
