# bench/corpora/conll2003_de — German CoNLL-2003 (Frankfurter Rundschau)
# as a precision probe.
#
# *Status: gated.* The German split of CoNLL-2003 (Frankfurter Rundschau
# 1992) is distributed by the LDC under a research-only license that
# requires registration; there is no public HF mirror at time of writing.
# `loader.py` tries a list of community-mirror candidates and exits
# gracefully (code 2) with a clear message if none resolve. See README.
#
# When a mirror does become available, this Makefile already targets it:
# the `data` recipe runs the loader, which transparently picks the first
# working candidate. No top-level Makefile change needed.
#
# Targets mirror wikiann_de's layout so the top-level bench/Makefile
# can call `data-conll2003_de` and `corpus-conll2003_de` uniformly.

HERE   := $(dir $(abspath $(lastword $(MAKEFILE_LIST))))
DATA   := $(HERE)data
CORPUS := $(DATA)/corpus.jsonl

# How many sentences to sample. 300 keeps bench wall-clock low and
# matches wikiann_de's sample size for cross-corpus comparability.
N_SENTENCES ?= 300
# Deterministic seed so anonde-gliner vs anonde-patterns score on the
# same 300 docs.
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
