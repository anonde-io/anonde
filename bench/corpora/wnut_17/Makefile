# bench/corpora/wnut_17 — WNUT 2017 English emerging-entities NER.
#
# WNUT-17 (Derczynski et al., 2017) is the standard English NER
# benchmark for noisy user-generated text. Companion to CoNLL-2003 EN
# (clean newswire) — together they let us cite both a clean and a
# noisy English NER number. Open license (CC-BY).
#
# Targets mirror wikiann_de's layout so the top-level bench/Makefile
# can call `data-wnut_17` and `corpus-wnut_17` uniformly.

HERE   := $(dir $(abspath $(lastword $(MAKEFILE_LIST))))
DATA   := $(HERE)data
CORPUS := $(DATA)/corpus.jsonl

# How many sentences to sample. 300 matches wikiann_de / conll2003_en
# and keeps the total bench wall-clock under ~3 min/cell for the
# gliner backend. WNUT-17 test is small (~1287 sentences) so 300 is
# ~23% of the test split.
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
