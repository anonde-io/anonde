<!--
Thanks for sending a PR. A few notes before you submit:

- Keep the change focused. One concern per PR — easier to review, easier
  to revert.
- The repo is Apache 2.0. By opening a PR you're licensing the change
  under the same terms (Apache 2.0 §5).
- For changes that affect detection quality (new / modified recognizer,
  NER backend, conflict resolver), include a bench note — see below.
-->

## What changed

A short description of the change. What's new or different, and where
in the code it lives.

## Why

The motivation. Link the issue this fixes (`Fixes #N`) or the use case
it enables.

## Tests

- [ ] `go test ./...` passes locally.
- [ ] `go test -tags ner ./...` passes (only required if the change
      touches the analyzer / NER recognizers / build tags).
- [ ] New unit tests cover the change, or an existing test was extended.

If the change is documentation- or comment-only, say so here and skip
the rest.

## Bench impact (if any)

Skip if not applicable (docs, refactor with no behavior change, etc.).
Otherwise:

- Corpora touched: e.g. `openmed`, `ai4privacy_en`, `wikiann_de`, …
- Engines re-run: e.g. `anonde-ner`, `anonde-patterns`.
- Result delta vs. `main`: leak rate, F1, latency — whichever the change
  is meant to move. Include before / after numbers from the matrix.
- Command used: `make -C bench corpus-<name>` or similar.

## Risk / breakage

Anything reviewers should look at twice — API surface changes, default
behavior changes, dependency bumps, new env vars, new build flags.

## Checklist

- [ ] Docs updated if user-visible behavior or config changed.
- [ ] `CHANGELOG.md` entry added under `## [Unreleased]` if the change is
      user-visible.
