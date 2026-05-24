---
name: Bug report
about: Something is broken or behaves differently than the docs say.
title: ''
labels: bug
assignees: ''
---

<!--
Before filing:

- Recall misses (PII the analyzer should have caught but didn't) are not bugs
  unless triggerable by crafted input. Track them via the benchmark suite or
  open a separate "recognizer gap" issue with a sample document.
- Security issues: do NOT file here. See SECURITY.md for the private channel.
-->

## What happened

A short description of the actual behavior.

## What you expected

What you thought would happen, and where you got that expectation
(README, docs/, a specific recognizer, etc.).

## Reproduction

Minimal steps. A `curl` invocation, a Go snippet, or a short corpus
sample is ideal. Redact any real PII before sharing.

```bash
# example
curl -sS -X POST http://localhost:8081/v1/anonymizations \
  -H "Content-Type: application/json" \
  -d '{"tenant_id":"repro","content":"..."}'
```

Observed response / log output:

```
```

## Environment

| | |
|---|---|
| anonde version / commit | e.g. `v0.1.0` or `git rev-parse HEAD` |
| Build variant | patterns-only / `-tags hugot` / Docker image tag |
| OS + arch | e.g. macOS 14.5 arm64, Ubuntu 22.04 amd64 |
| Go version (if building from source) | `go version` output |
| Analyzer backend | `ANALYZER_BACKEND` value (patterns / gliner / hugot / ollama) |
| Language sent | `de`, `en`, multilingual, … |

## Additional context

Anything else relevant — config, env vars, related issues, suspected
recognizer or operator.
