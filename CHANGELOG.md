# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).
While the version is below `1.0.0`, minor releases may include breaking changes;
each such change is called out under a **Changed** or **Removed** heading.

## [Unreleased]

Nothing yet. Post-`v0.1.0` work-in-progress lands here.

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

[Unreleased]: https://github.com/anonde-io/anonde/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/anonde-io/anonde/releases/tag/v0.1.0
