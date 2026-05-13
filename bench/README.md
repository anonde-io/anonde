# bench/

The anonde bench harness — a single canonical tree replacing the old
`bench/`, `benchmark/`, `vs/`, and `bench/parity/` layouts.

```
bench/
├── Makefile                 # top-level entry: `make matrix`, `make corpus-NAME`, `make help`
├── README.md                # this file
├── corpora/                 # one directory per dataset (gold + per-engine outputs)
│   ├── openmed/             # German PHI: GraSCCo (the German clinical anchor)
│   ├── ggponc_de/           # German oncology guidelines (precision probe)
│   ├── pmc_de/              # German clinical case reports from PMC (precision probe)
│   ├── synth_clinical/      # synthetic German clinical (slot-based gold; full F1)
│   ├── wiki_de/             # German medical Wikipedia (precision probe)
│   └── ai4privacy_en/       # English PII (ai4privacy/pii-masking-200k; Presidio parity)
├── runners/                 # every engine the matrix can score
│   ├── anonde.go            # unified Go runner — patterns / hugot / gliner
│   ├── presidio.py          # Microsoft Presidio (Python sidecar)
│   ├── gliner.py            # GLiNER PII (Python sidecar, reference for the Go-native build)
│   └── openai_pf.py         # OpenAI Privacy Filter (Python sidecar)
├── probes/                  # diagnostic — not part of the matrix
│   ├── hugot/probe.go       # "does this hugot model load and emit?"
│   ├── gliner/probe.go      # "what does the Go-native GLiNER recognizer return?"
│   └── diff_gliner/main.go  # candidate-vs-reference GLiNER output diff
├── scoring/                 # shared scoring + label normalisation
│   ├── compare.py           # per-corpus engine comparator (used by each corpus's `make report`)
│   ├── render_matrix.py     # ingests every per-cell findings JSONL, writes REPORT_MATRIX.md
│   ├── merge_findings.py    # union-merge two findings JSONLs (e.g. patterns ∪ sidecar NER)
│   └── label_map.yaml       # canonical entity vocabulary + per-engine label maps
└── microbench/              # Go vs Python micro-benchmarks (latency + throughput)
    ├── bench_test.go
    ├── gentext_test.go
    ├── bench_python.py
    └── compare/main.go
```

## The matrix

```sh
# from repo root:
make -C bench help          # list all targets
make -C bench data          # fetch / generate every corpus
make -C bench matrix        # the headline: all engines × all corpora
make -C bench matrix-de     # German subset only (skips ai4privacy_en)
make -C bench matrix-en     # English subset only (only ai4privacy_en)
make -C bench corpus-openmed   # one corpus, all engines
make -C bench clean         # remove generated data + reports
```

The matrix scores these cells:

| Engine | What it is | German? | English? |
|---|---|:---:|:---:|
| `anonde-patterns` | patterns-only floor | ✓ | ✓ |
| **`anonde-gliner`** | **production: patterns + GLiNER (`knowledgator/gliner-pii-base-v1.0`, threshold 0.40)** | ✓ | ✓ |
| `anonde-hugot` | patterns + hugot XLM-R PII (the prior default; regression detector) | ✓ | ✓ |
| `presidio` | Microsoft Presidio (external baseline competitor) | – | ✓ |
| `gliner-py` | Python sidecar GLiNER (parity check on the Go-native build) | ✓ | ✓ |

Per-cell output → `bench/corpora/<corpus>/data/anonde_<engine>.jsonl`.
Combined report → `bench/REPORT_MATRIX.md` (+ `bench/results_matrix.csv`).

## Running a single corpus directly

Every corpus directory still has its own Makefile that does the full
load → predict → score loop using the unified runner:

```sh
make -C bench/corpora/openmed all ANONDE_BACKEND=patterns-only   # fastest
make -C bench/corpora/openmed all ANONDE_BACKEND=gliner          # production stack
make -C bench/corpora/openmed all ANONDE_BACKEND=hugot ANONDE_MODEL=urchade/gliner_multi-v2.1
```

The per-corpus Makefiles also accept `RECONCILER=ollama`, `AUDITOR=ollama`,
`NER_SCORE_FLOOR=...`, etc. — see each corpus's source for the full set.

## Schema

Every runner emits the same JSONL shape (codepoint offsets, Python
convention) so all comparators just work:

```json
{"id":"doc-42","engine":"anonde-gliner",
 "findings":[{"start":12,"end":28,"type":"EMAIL_ADDRESS","score":1.0}],
 "duration_ms":8.4}
```
