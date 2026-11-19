# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).
While the version is below `1.0.0`, minor releases may include breaking changes;
each such change is called out under a **Changed** or **Removed** heading.

## [Unreleased]

Nothing yet. Post-v0.2.0 work-in-progress lands here.

## [0.2.0] - 2026-11-19

Consolidates the NER lineup to one image, makes the NER path fail closed
instead of silently degrading to patterns-only, and hardens the vault,
reveal, and proxy paths. Two image variants are published at `0.2.0`:
`ghcr.io/anonde-io/anonde` (patterns-only, ~12 MB) and
`ghcr.io/anonde-io/anonde-ner` (BASE GLiNER, ~770 MB), multi-arch
(`linux/amd64` + `linux/arm64`). `anonde-ner-stack` is retired; its last
version is `0.1.1`.

### Upgrading from 0.1.x

- **`anonde-ner-stack` users** move to `ghcr.io/anonde-io/anonde-ner`. To
  run the LARGE GLiNER model on its own, start the NER image with:
  ```
  ANALYZER_BACKEND=gliner-flat
  GLINER_MODEL=knowledgator/gliner-pii-large-v1.0
  GLINER_ONNX_FILE=model.onnx
  ```
  `GLINER_ONNX_FILE` is required because the image defaults it to the
  BASE layout (`onnx/model.onnx`), while the LARGE export sits at the repo
  root. LARGE is not baked into `anonde-ner`, so the first boot downloads
  it from Hugging Face: the container needs outbound access, and pointing
  `GLINER_MODELS_DIR` at the data volume (e.g. `/var/lib/anonde/models`)
  keeps the download across restarts. If boot verification times out
  during that download, raise `ANONDE_NER_VERIFY_TIMEOUT` (default `5m`).
  Plan for roughly 1.4 GB of RAM per LARGE instance (see the pool-sizing
  notes in `docs/DEPLOYMENT.md`). The stack's `ANONDE_GLINER_FLAT_MODEL`,
  `ANONDE_GLINER_FLAT_ONNX_FILE`, `ANONDE_GLINER_FLAT_THRESHOLD`, and
  `ANONDE_GLINER_FLAT_POOL_SIZE` map to `GLINER_MODEL`,
  `GLINER_ONNX_FILE`, `GLINER_THRESHOLD`, and `GLINER_POOL_SIZE`. Running
  BASE and LARGE together in one process is no longer supported.
- **Source builds** use `-tags ner` instead of `-tags hugot`
  (`CGO_ENABLED=1 go build -tags ner ./...`).
- **Removed backends refuse to start.** `ANALYZER_BACKEND=hugot`,
  `ollama`, or `gliner-stack` now exits at boot with
  `unsupported ANALYZER_BACKEND`. `ANONDE_NER_STACK` and
  `ANONDE_NER_STACK_PARALLEL` are no longer read, so a deploy that still
  sets them runs the single `GLINER_MODEL`. `HUGOT_MODEL`,
  `HUGOT_MODELS_DIR`, `OLLAMA_ENDPOINT`, and `OLLAMA_MODEL` are ignored.
- **An explicit `score_threshold` of `0` is rejected** with
  `InvalidArgument`. Leave `score_threshold_set` unset to get the server
  default.
- **NER failures are errors now** (see **Changed**). A NER deploy whose
  model cannot load exits at boot unless `ANONDE_ALLOW_NER_FALLBACK=1`;
  a request whose NER recognizer fails returns an error; and a single
  analyzed text (a text body, or one JSON string value or log line) that
  needs more than 64 GLiNER chunks (roughly 64 KB) is rejected instead
  of being only partly NER-covered. Split large inputs, or pass
  `disable_ner: true` for patterns-only coverage.
- **Health checks:** the port now opens before the model loads, and
  `/healthz` returns `503` until it is ready. Gate traffic on
  `/healthz`, not on the port accepting connections.
- **Identifiers:** `tenant_id` and `id` longer than 4096 bytes, or
  containing a NUL byte, are rejected.
- **Structured-format offsets:** `start` / `end` on findings and tokens
  for `json` / `ndjson` / `logs` content are now document-relative
  (previously each leaf or line restarted at `0`).
- **New default detections:** the `SECRET`, well-known `ORGANIZATION`,
  and `US_NPI` / `US_DEA` / `US_MBI` recognizers are on by default, so
  text that passed through 0.1.x untouched may now be redacted. Use the
  `entities` allow-list to scope detection.
- **Go library callers:** `anonymizer.AnonymizerConfig` is now a struct;
  wrap an existing operator map with `anonymizer.Config(ops)`. Removed:
  `anonde.DefaultAnalyzerEngineWithHugot`, `...WithHugotConfig`,
  `...WithOllama`, `...WithGLiNEREnsemble`, and in `recognizers`:
  `HugotNERConfig`, `NewHugotNERRecognizer`, `MapHugotLabel`,
  `NewOllamaNERRecognizer`, `NewEnsembleGLiNERRecognizer`,
  `EnsembleFromEnv`. `recognizers.NewENPersonRecognizer` now returns
  `analyzer.EntityRecognizer` (was `*PatternRecognizer`).
- **Downgrade caveat:** with `STORE_BACKEND=bbolt` and
  `ANONDE_VAULT_KEY` set, store records written by 0.2.0 are encrypted
  at rest and cannot be read by 0.1.x. Back up the database file before
  upgrading if you may need to roll back.

### Added

- **`SECRET` recognizer** for machine credentials. Provider rules
  (derived from gitleaks, MIT; attributed in `NOTICE`) match tokens with
  a distinctive prefix such as `ghp_`, `AKIA`, `sk_live_`, or `AIza`; a
  generic layer flags a high-entropy value only when it is assigned to a
  secret keyword (`api_key = "..."`). Free-floating hashes, UUIDs, and
  base64 blobs are not flagged. On by default.
- **US healthcare identifiers:** `US_NPI` (Luhn-validated and
  context-required, so a bare 10-digit number does not match), `US_DEA`
  (checksum-validated), and `US_MBI` (strict CMS format). On by default.
- **Well-known organization gazetteer.** Tags AI vendors and major tech
  brands (OpenAI, Anthropic, Hugging Face, Mistral AI, ...) as
  `ORGANIZATION`, which GLiNER misses on coined brand names. Ambiguous
  names (Claude, Gemini, Llama, Meta, Cursor, ...) fire only near an
  AI/tech cue. A GLiNER `ORGANIZATION` span still takes precedence. On
  by default.
- **Opt-in GLiNER precision knobs.** `GLINER_STRICT=1` applies per-class
  floors (PERSON `0.50`, ORG / LOCATION / NRP `0.55`) plus a structural
  shape filter that drops fuzzy-type spans shaped like UUIDs, locales,
  semver strings, model slugs, hex / base64 blobs, SCREAMING_SNAKE
  constants, or dotted paths. `GLINER_SPAN_FILTER=1` enables only the
  shape filter; `GLINER_STOPLIST` appends terms to its stoplist.
  `GLINER_ORG_THRESHOLD`, `GLINER_LOCATION_THRESHOLD`, and
  `GLINER_NRP_THRESHOLD` join `GLINER_PERSON_THRESHOLD`; each is used
  as-is, so it can raise a class above its default floor. All off by
  default.
- **Leak-policy primitives (Go library).** `AnonymizerConfig` gains
  `DetectOnlyTypes` (detect a type but leave it verbatim, with no vault
  entry) and `AllowList` (pass known-safe surfaces through verbatim),
  backed by a new `operators.Keep`. `recognizers.NewDenyListRecognizer`
  always flags caller-supplied terms. Library-only in this release.

### Changed

- **The NER path fails closed instead of silently falling back to
  patterns-only.** In 0.1.x a dead NER backend degraded to patterns-only
  with no error, so PERSON / ORGANIZATION / LOCATION leaked while the
  server still reported a NER backend.
  - At boot, if `ANALYZER_BACKEND` selects a NER backend and the model
    cannot load (or the binary was built without `-tags ner`), the
    server exits non-zero. `ANONDE_ALLOW_NER_FALLBACK=1` deliberately
    degrades to patterns-only with an `ERROR` log instead.
    `ANONDE_NER_VERIFY_TIMEOUT` (default `5m`) bounds the check.
  - Per request, a NER recognizer error (tokenizer, ONNX inference,
    pooled-model load) fails the request instead of returning a
    patterns-only result. A lone pattern-recognizer error is still
    tolerated.
  - An input that needs more than 64 GLiNER sliding-window chunks
    (roughly 64 KB of text at the default chunk size) now errors.
    Previously only the head was NER-covered and the tail silently
    dropped to patterns-only. Library callers can raise
    `GLiNERConfig.MaxChunks` (negative disables the cap).
- **The listener binds before the model finishes loading.** `/healthz`
  returns `503 {"status":"starting"}` while NER verification and
  `WARMUP_ON_START` run, then `200`. A verification failure still exits
  non-zero. Previously the port stayed closed until both finished, which
  a proxy in front reads as an unreachable host. Other endpoints accept
  connections during this window and wait for the model.
- **GLiNER post-filters are on by default.** Every NER backend now drops
  currency amounts mislabelled as `ID` / `POSTAL_CODE` and whole-surface
  tokens that are never names. `GLINER_LABEL_SET=legal` also applies a
  legal precision profile (role stoplist, statute / exhibit suppressor,
  shape filter), and `GLINER_LABEL_SET=clinical` a clinical profile
  (multilingual role / field / department stoplist, department-context
  gate). The default `chat` set gets only the money and non-name guards;
  the broader shape filter stays opt-in.
- **Conflict resolution keeps validated IDs and full addresses.** The
  NER-wins rule for PERSON / ORG / LOCATION and similar types no longer
  evicts a checksum-validated structured ID (e.g. German Steuer-ID, UK
  NHS number, PESEL, Aadhaar, US NPI) or a pattern `POSTAL_CODE` /
  `STREET_ADDRESS` span that fully contains the NER span. Previously
  this could drop postcode digits.
- **PERSON spans drop a leading title or role word.** GLiNER sometimes
  glues a word such as `Mr`, `Dr`, `Customer`, or `Client` onto a name
  ("Customer john doe"); the span now starts at the name. The name is
  still redacted; the title word stays in the output. Multi-word names
  are untouched.
- **Fewer false positives from the heuristic PERSON / ORG recognizers.**
  Structured tokens (UUIDs, hex / base64 blobs, snake_case keys, model
  slugs, dotted paths) are no longer emitted as PERSON / ORG; a lone
  capitalised token on a JSON key, `=` assignment, user-agent version
  path, or English contraction is suppressed unless a name cue sits
  nearby; social-handle and username context cues must come from
  outside the candidate; the English PERSON context boost is limited to
  the same sentence; and `it_driver_license` rejects pure-hex surfaces.
- **Structured-format offsets are document-relative.** For `json`,
  `ndjson`, and `logs`, `Finding` and `TokenRef` `start` / `end` now
  locate the value in the submitted document instead of restarting at
  `0` for each leaf or line. Offsets are exact for unescaped JSON and
  lines without ANSI codes, and approximate past JSON escapes, ANSI
  sequences, or repaired UTF-8.
- **OCR subprocesses are bounded.** `pdftoppm` / `tesseract` now stop
  when the request is cancelled, and `ANONDE_OCR_TIMEOUT` (default `5m`;
  `0` / `off` disables) backstops requests that carry no deadline.
- **Telemetry heartbeat adds two booleans,** `activated_anonymize` and
  `activated_reveal`: whether any create or reveal request has succeeded
  since the process started. No content is sent; the field list is in
  `docs/TELEMETRY.md`. Disable with `ANONDE_TELEMETRY=off` or
  `ANONDE_OFFLINE=1`.
- **The NER build tag `hugot` is renamed to `ner`.** The tag only ever
  gated GLiNER. The published `anonde-ner` image is unaffected. The
  `anonde_build_info` metric label for NER builds is now `ner` (was
  `hugot`).
- **An explicit `score_threshold` of `0` is now rejected.** When
  `score_threshold_set = true`, `score_threshold` must be `> 0`; an
  explicit `0` (or negative) returns `InvalidArgument`. It was
  documented as "include everything" but silently used the service
  default. Applies to REST, Connect, and gRPC for create, synthesize,
  and PDF redaction.
- **Lower per-request overhead.** Pattern recognizers dispatch through a
  bounded worker pool instead of one goroutine each; the anonymizer
  rewrites in a single forward pass; line-based formats stream instead
  of splitting the whole input up front; PDF text extraction is no
  longer quadratic in page count; and the bbolt sweeper deletes in
  bounded batches. Output is unchanged.

### Removed

- **`anonde-ner-stack` image, the `gliner-stack` backend, and the
  `ANONDE_NER_STACK` ensemble.** The BASE + LARGE image (~2.1 GB) is no
  longer built, pushed, or documented; `0.1.1` is its last version. The
  LARGE model stays reachable on its own via `ANALYZER_BACKEND=gliner-flat`
  (see **Upgrading from 0.1.x**), but running both models together in
  one process is gone. Also removed: `Dockerfile.anonde-ner-stack`,
  `make docker-build-ner-stack`, the `ner-stack` compose profile, and
  the `ANONDE_GLINER_FLAT_*` and `ANONDE_NER_STACK_PARALLEL` env vars.
- **hugot / XLM-R transformer NER backend** (`ANALYZER_BACKEND=hugot`
  and the `HUGOT_MODEL` / `HUGOT_MODELS_DIR` env vars). GLiNER
  outperformed it on every benchmarked corpus and was already the
  production default. The hugot *library* stays a dependency, since
  GLiNER reuses its model downloader and cache layout.
- **Ollama NER backend** (`ANALYZER_BACKEND=ollama` and the
  `OLLAMA_ENDPOINT` / `OLLAMA_MODEL` env vars). It shipped in neither
  image, had no leak-rate benchmark, and depended on an external daemon.
  Using anonde as a privacy proxy *in front of* a local Ollama
  (`ANONDE_OPENAI_BASE_URL`) is a separate feature and is unaffected.

### Fixed

- **A narrower NER span no longer evicts a wider same-type pattern
  span.** The pattern span's overhang was left unredacted.
- **`disable_ner` skips pooled GLiNER.** With `GLINER_POOL_SIZE >= 2`,
  `disable_ner: true` still ran the model pool.
- **Duplicate JSON keys resolve last-wins before any side effect.** A
  shadowed value (`{"a":"alice@x","a":"safe"}`) no longer mints a vault
  entry or appears in findings, and an invalid document triggers none.
- **Enabling `ANONDE_VAULT_KEY` on an existing keyless bbolt vault** no
  longer makes every existing mapping unreadable. New entries carry a
  version tag; legacy plaintext and legacy encrypted entries both stay
  readable.
- **`DELETE` surfaces store errors** instead of reporting success when
  the record could not be read.
- **Vault / store entry gauges exclude expired entries** on both the
  memory and bbolt backends.
- **`WARMUP_ON_START` reports a timeout** as a `WARNING` instead of
  logging success when `ANONDE_WARMUP_TIMEOUT` expires.
- **Native memory leak on analyzer rebuild** when any `ANONDE_ORT_*`
  knob is set (ONNX Runtime session options were never released).

### Security

- **PDF reveal enforces the detokenize policy.**
  `GET /v1/anonymizations/{id}/reveal-pdf` (and its gRPC / Connect
  equivalents) returned the original PDF without consulting the policy
  authorizer that gates text reveal. It is now checked before the record
  lookup; a denied caller gets `403`.
- **Text reveal checks policy before looking up the record.** A denied
  caller could previously tell an existing id from a missing one, and
  records with no tokens skipped the policy check entirely.
- **Bounded identifiers close a bbolt key collision.** A `tenant_id`
  over 64 KiB could forge another tenant's on-disk key. `tenant_id` and
  `id` are now capped at 4096 bytes with NUL rejected on every service
  path, including the `SaveRecord` / `GetRecord` escape hatch. No
  on-disk format change.
- **In-memory backend tenant isolation.** Keys joined tenant and id with
  `:`, so `a:b` + `c` and `a` + `b:c` collided across tenants. Keys are
  now length-prefixed.
- **bbolt at-rest encryption covers the store, not just the vault.**
  With `ANONDE_VAULT_KEY` set, store records (including PDF originals)
  are now AES-256-GCM sealed too. Existing plaintext records stay
  readable.
- **No token reuse across restarts.** The token counter resets on
  restart, so with a persistent vault a re-minted token could overwrite
  a live mapping and an older document would reveal a newer one's
  cleartext. Minting now skips occupied tokens, and the vault refuses to
  overwrite a live token with different cleartext.
- **The vault cache honours `ANONDE_VAULT_TTL`.** The bbolt LRU cache
  could serve cleartext after the underlying entry expired.
- **Failed or replaced ingests no longer strand cleartext.** Vault
  mappings minted before an ingest failed, or belonging to a record
  replaced by re-ingesting the same id, stayed in the vault where
  `DELETE` could not reach them. They are now rolled back or removed.
- **Synthesis no longer keeps letters for types without a generator.**
  `SECRET`, `PROFESSION`, `NRP`, custom deny-list types, and
  alphanumeric licence / regional IDs kept their letters verbatim in
  synthesized output. Letters and digits are now scrambled
  (Unicode-aware), preserving character class, case, and separators.
- **The OpenAI-compatible proxy caps upstream response size.** The
  upstream body was buffered without limit; it is now capped at 32 MiB
  (`ANONDE_PROXY_MAX_RESPONSE_BYTES`), and an oversize response fails
  with `502` instead of being truncated.

### Known issues

- **Usernames labelled in languages other than English can be missed.**
  The username recognizer only fires next to English context words such
  as "username" or "account", so a value after a label like
  "Nombre de Usuario:" can pass through unredacted. A fix is planned
  for `0.2.1`.

Maintenance: Go module and GitHub Actions dependency bumps; CI fixes
(fail-closed GLiNER model prefetch and cache repair in the benchmark
workflows, runner-label fix); `NOTICE` attribution for redistributed
binaries, models, and linked dependencies, with `LICENSE` and `NOTICE`
now shipped at `/` in both images; benchmark harness and report updates
(published methodology, precision scorecard); documentation fixes.

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

[Unreleased]: https://github.com/anonde-io/anonde/compare/v0.2.0...HEAD
[0.2.0]: https://github.com/anonde-io/anonde/compare/v0.1.1...v0.2.0
[0.1.1]: https://github.com/anonde-io/anonde/compare/v0.1.0...v0.1.1
[0.1.0]: https://github.com/anonde-io/anonde/releases/tag/v0.1.0
