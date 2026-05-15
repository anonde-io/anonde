# bench/corpora/germeval_14 — GermEval 2014 German NER (open license).
#
# Companion to wikiann_de but with denser annotations (~3 entities per
# sentence vs ~1) and slightly cleaner gold (expert annotation, not
# anchor-link induced). Open license (CC-BY 4.0) — no DUA, no auth.
#
# Why we include it: gives us the standard German NER baseline number
# every German NER paper publishes, comparable to CoNLL-2003 EN
# numbers. WikiAnn complements as a precision probe; this is the
# strict-F1 anchor.
#
# Targets mirror wikiann_de's layout so the top-level bench/Makefile
# can call `data-germeval_14` and `corpus-germeval_14` uniformly.

HERE   := $(dir $(abspath $(lastword $(MAKEFILE_LIST))))
DATA   := $(HERE)data
CORPUS := $(DATA)/corpus.jsonl

# How many sentences to sample. 300 matches wikiann_de and keeps the
# total bench wall-clock under ~3 min/cell for the gliner backend.
N_SENTENCES ?= 300
SEED        ?= 20260515
PYTHON      ?= python3

.PHONY: all data clean

all: data

$(CORPUS):
	mkdir -p $(DATA)
	$(PYTHON) $(HERE)loader.py --out $(CORPUS) --n $(N_SENTENCES) --seed $(SEED)

data: $(CORPUS)

clean:
	rm -f $(CORPUS)
