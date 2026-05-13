# bench/corpora/wikiann_de — German Wikipedia NER as a precision probe.
#
# WikiAnn (a.k.a. PAN-X) is the de-facto open multi-lingual NER corpus
# derived from Wikipedia anchor links. The German split (`wikiann/de`)
# carries ~30k sentences with expert-validated PER / LOC / ORG spans.
# We sample 300 sentences to keep CI bench wall-clock low; bench
# numbers are stable at that size on this corpus shape.
#
# Why we include it: the synthetic corpora (synth_clinical, finance_de,
# legal_de) all have unambiguous slot-shaped PII. Real natural-text
# precision is harder: anonde may flag plausible-looking strings
# (capitalised common nouns, time expressions, etc.) that aren't true
# entities. WikiAnn gives us a numerical answer to "does anonde
# over-flag on real German prose?".
#
# Targets mirror synth_clinical's layout so the top-level
# bench/Makefile can call `data-wikiann_de` and `corpus-wikiann_de`
# uniformly.

HERE   := $(dir $(abspath $(lastword $(MAKEFILE_LIST))))
DATA   := $(HERE)data
CORPUS := $(DATA)/corpus.jsonl

# How many sentences to sample. 300 = ~3 min total bench cost across
# patterns + gliner + gliner-py and big enough that the precision
# number is stable (we verified by halving and quartering it locally).
N_SENTENCES ?= 300
# Deterministic seed so anonde-gliner vs anonde-patterns score on the
# same 300 docs.
SEED        ?= 20260513
PYTHON      ?= python3

.PHONY: all data clean

all: data

$(CORPUS):
	mkdir -p $(DATA)
	$(PYTHON) $(HERE)loader.py --out $(CORPUS) --n $(N_SENTENCES) --seed $(SEED)

data: $(CORPUS)

clean:
	rm -f $(CORPUS)
