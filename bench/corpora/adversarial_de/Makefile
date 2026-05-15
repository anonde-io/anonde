# bench/corpora/adversarial_de — derive an adversarial / OOD probe from
# the synth_clinical corpus by applying six classes of realistic input
# perturbations (typo inside PII, umlaut transliteration, case scramble,
# ANSI escape insertion, EN/DE code-switching, NBSP substitution).
#
# Why we include it: every other gold German corpus in the bench
# (synth_clinical, finance_de, legal_de, openmed) ships *clean* text.
# Production traffic isn't clean. This corpus measures whether anonde's
# recall holds up under realistic input noise without retraining anything.
#
# Targets mirror synth_clinical's layout so the top-level bench/Makefile
# can call `data-adversarial_de` and `corpus-adversarial_de` uniformly.
# Depends on synth_clinical's corpus.jsonl as the source for perturbation.

HERE   := $(dir $(abspath $(lastword $(MAKEFILE_LIST))))
DATA   := $(HERE)data
CORPUS := $(DATA)/corpus.jsonl

# Sibling corpus we derive from. Order-only prereq so make doesn't
# regenerate ours every time synth_clinical's mtime changes — the
# perturbation script is seeded deterministically, so re-running on the
# same input yields identical output.
SOURCE_CORPUS := $(abspath $(HERE)../synth_clinical/data/corpus.jsonl)

# Number of input docs to sample × 6 perturbations = output doc count.
# 50 × 6 = 300 docs, matching wikiann_de's bench scale and keeping cell
# wall-clock under ~3 min for the gliner backend.
N_INPUTS ?= 50
SEED     ?= 20260515
PYTHON   ?= python3

.PHONY: all data clean

all: data

data: $(CORPUS)

$(CORPUS): | $(SOURCE_CORPUS)
	mkdir -p $(DATA)
	$(PYTHON) $(HERE)loader.py \
	  --in  $(SOURCE_CORPUS) \
	  --out $(CORPUS) \
	  --n   $(N_INPUTS) \
	  --seed $(SEED)

# If the source corpus isn't there yet, build it. This delegates back to
# the parent corpus's Makefile so we don't duplicate generator logic.
$(SOURCE_CORPUS):
	$(MAKE) -C $(abspath $(HERE)../synth_clinical) data

clean:
	rm -f $(CORPUS)
