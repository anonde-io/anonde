# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).
While the version is below `1.0.0`, minor releases may include breaking changes;
each such change is called out under a **Changed** or **Removed** heading.

## [Unreleased]

### Changed

- **An explicit `score_threshold` of `0` is now rejected.** When a request
  sets `score_threshold_set = true`, `score_threshold` must be `> 0`;
  an explicit `0` (or negative) returns `InvalidArgument` instead of
  silently falling through to the service default. Previously the field
  was documented as "`0` = include everything" but the service treated
  it as "use default", so an explicit `0` was silently ignored. Clients
  that want the server default should leave `score_threshold_set` unset
  (or `false`) rather than sending `0`. Affects the REST, Connect, and
  gRPC transports for create/anonymize, synthesize, and PDF redaction.
- **The NER build tag `hugot` is renamed to `ner`.** Self-hosters who
  build the NER variant from source now use `go build -tags ner ./...`
  (was `-tags hugot`); the published `anonde-ner` / `anonde-ner-stack`
  images are unaffected. The old name was misleading once the hugot
  transformer backend was removed (see below) — the tag only ever gated
  GLiNER. The `anonde_build_info` metric label for NER builds is now
  `ner` (was `hugot`).

### Removed

- **hugot / XLM-R transformer NER backend** (`ANALYZER_BACKEND=hugot`
  and the `HUGOT_MODEL` / `HUGOT_MODELS_DIR` env vars). GLiNER strictly
  outperformed it on every benched corpus and was the production default;
  the hugot recognizer carried a second ONNX code path for no recall
  benefit. The hugot *library* stays a dependency — GLiNER reuses its
  model downloader and on-disk cache layout. The public library entry
  points `anonde.DefaultAnalyzerEngineWithHugot` /
  `...WithHugotConfig` and the `recognizers.HugotNERConfig` type are
  removed; use the GLiNER constructors instead.
- **Ollama NER backend** (`ANALYZER_BACKEND=ollama`) and the
  `OLLAMA_ENDPOINT` / `OLLAMA_MODEL` env vars. The LLM-over-HTTP backend
  was never shipped in either image, carried no leak-rate bench numbers,
  and depended on an external daemon — off-brand for a deterministic
  local-redaction tool and pure installation-surface clutter. The public
  library entry point `anonde.DefaultAnalyzerEngineWithOllama` is removed.
  Using anonde as a privacy proxy *in front of* a local Ollama
  (`ANONDE_OPENAI_BASE_URL`) is unaffected — that is a separate feature.

## [0.1.1] - 2026-06-13

Maintenance release. Re-publishes the three image variants
(`anonde`, `anonde-ner`, `anonde-ner-stack`) at `0.1.1`.

### Added

- **Selectable GLiNER label sets** (`GLINER_LABEL_SET`) — the NER image
  ships four curated open-set label sets and selects one at inference
  time: `chat` (default), `clinical`, `finance`, and `legal`. All four
  map onto the same canonical entity types the pattern recognizers emit,
  so anonymizer operators and reveal/detokenize behave identically
  regardless of which set is active; an unrecognised value falls back to
  `chat`. Go-library callers set `GLiNERConfig.Labels` / `LabelToEntity`
  directly (e.g. `recognizers.FinancePIILabels`). See the README for the
  per-set coverage table.

### Changed

- **Default NER label set is now `chat`** (was the full clinical set in
  `0.1.0`). `chat` drops `age`, `profession`, `job title`,
  `date` / `date of birth`, and the clinical / German-insurance labels
  because they over-redact ordinary conversational text ("18 years of
  experience" → AGE, "tech" → PROFESSION). Deployments that need the old
  behavior set `GLINER_LABEL_SET=clinical`.

## [0.1.0] - 2026-05-28

First tagged release. Three image variants published to ghcr.io:
`ghcr.io/anonde-io/anonde:0.1.0` (patterns-only, ~12 MB),
`ghcr.io/anonde-io/anonde-ner:0.1.0` (BASE GLiNER, ~770 MB), and
`ghcr.io/anonde-io/anonde-ner-stack:0.1.0` (BASE + LARGE GLiNER, ~2.1 GB).
Multi-arch (`linux/amd64` + `linux/arm64`).

### Added

- **PII analyzer** — 52 region-aware pattern recognizers covering 12+
  jurisdictions (international IDs, US, UK, Germany, Italy, Spain,
  Australia, India, Poland, Singapore, Finland, Korea), with parallel
  dispatch and score-based conflict resolution.
- **Optional in-process NER** behind the `-tags hugot` build — GLiNER and
  hugot ONNX recognizers. Requires CGO and a reachable `libonnxruntime.so`.
  NER beats patterns for PERSON/ORG/LOC/AGE/PROFESSION/NRP on conflict.
- **Anonymizer operators** — Replace, Redact, Mask, Hash, Encrypt, and
  Synthesize, with adjacent-span merging.
- **Reversible token vault** — in-memory and bbolt backends behind one
  interface. Tokens are stable per `(tenant, doc)` and reversible only
  through the reveal path.
- **HTTP server** — three transports on one port: REST/JSON (grpc-gateway),
  Connect (Connect/JSON, Connect/Protobuf, gRPC-Web), and native gRPC.
  Wire JSON is snake_case; camelCase inputs are also accepted.
- **Reveal / detokenize** gated by `actor` + `purpose` audit metadata —
  the only path that returns cleartext.
- **OpenAI-compatible proxy** at `POST /v1/chat/completions` —
  anonymizes the prompt, forwards to the upstream provider, and
  de-anonymizes the response in OpenAI shape. v0.1 proxies OpenAI only,
  non-streaming only.
- **Content formats** — text, JSON, NDJSON, logs, and PDF, with
  format negotiation. Scanned (image-only) PDFs are OCR'd via
  `pdftoppm` + `tesseract` when both are on `PATH`; bundled in the
  NER image variants, no-op fallback in the patterns-only image.
  Tunable via `ANONDE_OCR_*` env vars.
- **Docker images** — three multi-arch (`linux/amd64` + `linux/arm64`)
  variants: `anonde` (patterns-only, ~12 MB, pure Go),
  `anonde-ner` (BASE GLiNER, ~770 MB, CGO + bundled libonnxruntime),
  and `anonde-ner-stack` (BASE + LARGE GLiNER, ~2.1 GB).
- **Public benchmark matrix** — leak-rate and F1 scoring across
  gold-annotated clinical, finance, legal, and general-PII corpora,
  re-run on every relevant PR with a guard rail against silent NER
  fallback.

### Security

- NER models are baked into the NER image; there is no outbound
  HuggingFace traffic at request time.
- All anonymization and de-anonymization runs locally — no third-party
  calls except the upstream provider when the OpenAI-compatible proxy
  is explicitly configured.

### Planned (post-v0.1.0)

- Secret recognizers — API keys, tokens, credentials.
- Streaming SSE support for the OpenAI-compatible proxy (`stream: true`).
- Anthropic and Gemini upstreams for the proxy, selected by model prefix.

[Unreleased]: https://github.com/anonde-io/anonde/compare/v0.1.1...HEAD
[0.1.1]: https://github.com/anonde-io/anonde/compare/v0.1.0...v0.1.1
[0.1.0]: https://github.com/anonde-io/anonde/releases/tag/v0.1.0
