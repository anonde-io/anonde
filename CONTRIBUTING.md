# Contributing to anonde

Thanks for considering a contribution. anonde is open source under
Apache 2.0; everything below applies whether you're fixing a typo,
adding a recognizer, or rewiring a transport.

## Code of conduct

By participating you agree to the [Code of Conduct](CODE_OF_CONDUCT.md)
(Contributor Covenant 2.1). Conduct concerns go privately to
`conduct@anonde.io`; security vulnerabilities go to the channels in
[`SECURITY.md`](SECURITY.md) — **not** to public issues.

## Before you start

- **Search existing issues** first. If your change is non-trivial
  (anything beyond a typo or a self-contained bugfix), open an issue
  so we can agree on shape before you spend time on the PR.
- **One concern per PR.** Easier to review, easier to revert. Split
  refactors out of feature work.
- **No DCO, no CLA.** Apache 2.0 §5 already grants the project a
  license to any contribution you submit through the GitHub PR flow.
  Don't bother with `Signed-off-by:` trailers unless you want to.

## Dev setup

```bash
git clone https://github.com/anonde-io/anonde
cd anonde

# One-off: install the protoc plugins used by `make proto`.
# Lands them in $(go env GOPATH)/bin.
make tools

# Default build — pure Go, no CGO. Production-safe everywhere.
make build

# NER build — `-tags hugot` enables the GLiNER + hugot in-process
# recognizers. Needs CGO and a reachable libonnxruntime.so. The
# Dockerfile.anonde-ner image handles all of this for you; if you
# want it locally, see docs/DEPLOYMENT.md.
make build-ner
```

`make help` lists every target.

## Tests

```bash
make test                                    # full suite
make test-api                                # internal/api/... only
go test ./analyzer/recognizers/ -run TestX   # single test
make ci                                      # what CI runs: vet + tests
```

Tests are expected to pass on a default `make build` (no `-tags
hugot`). If your change touches the NER recognizers, run
`go test -tags hugot ./...` too — but flag in the PR description that
you couldn't (or did) run it, since the libonnxruntime requirement
isn't on every contributor's machine.

## Adding a recognizer

Pattern recognizers are the simplest path. Drop a file under
`analyzer/recognizers/` and register it in `anonde.go`. The shortest
real example in the tree is [`analyzer/recognizers/email.go`](analyzer/recognizers/email.go).

```go
// analyzer/recognizers/my_id.go
package recognizers

import "regexp"

var myIDRE = regexp.MustCompile(`\bMID-\d{6,10}\b`)

// NewMyIDRecognizer detects MY_ID entities.
func NewMyIDRecognizer() *PatternRecognizer {
	return NewPatternRecognizerWithContext(
		"MyIDRecognizer",
		[]string{"MY_ID"},
		[]string{"*"},                                // languages
		[]namedPattern{{re: myIDRE, score: 1.0}},
		[]string{"member id", "membership", "mid"},   // context boosts
	)
}
```

Then register in `anonde.go::patternRecognizers()` in the appropriate
region block, and add a unit test under `analyzer/recognizers/`
following any of the existing `*_test.go` files for shape.

If the entity name is new, also add it to
`bench/scoring/label_map.yaml` under the `anonde:` block so bench
runs map your label onto a canonical type.

Full pipeline + conflict-resolution rules (NER beats patterns for
PERSON/ORG/LOC/AGE/PROFESSION/NRP regardless of score) live in
[`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md). The full
52-recognizer table is in [`docs/RECOGNIZERS.md`](docs/RECOGNIZERS.md).

## PR conventions

- **Commit style.** Conventional commits are preferred:
  `feat(recognizers): add MID recognizer`, `fix(api): preserve
  tenant_id on reveal`, `docs: …`, `chore: …`. Scope is optional.
- **PR template.** The repo has a [PR template](.github/pull_request_template.md);
  it asks for what / why / tests / bench impact / risk. Filling it in
  shortens review.
- **Lint.** No project-specific linter beyond `go vet ./...` (run via
  `make vet` or `make ci`). Stick to `gofmt`'d code; CI rejects the
  rest.
- **Keep diffs focused.** Don't bundle drive-by refactors with a
  feature.

## Bench expectations

If your change touches detection (a new or modified recognizer, NER
backend, the conflict resolver, the analyzer pipeline), include a
bench note in the PR.

```bash
# Cheapest signal — one corpus, all engines (cached cells skip):
make -C bench corpus-openmed

# Full matrix (1–3 hours wall-clock):
make -C bench matrix

# Force a single cell to re-run:
rm bench/corpora/<corpus>/data/anonde_<engine>.jsonl
make -C bench corpus-<corpus>
```

What to include in the PR:

- Corpora you re-ran (e.g. `openmed`, `ai4privacy_en`).
- Before / after numbers from `bench/REPORT_MATRIX.md` for the rows
  you touched — leak rate, F1, or whichever metric the change is
  meant to move.
- If you couldn't run the bench (libonnxruntime / disk / time), say
  so. CI runs the canonical two corpora on every relevant PR, so
  silent regressions get caught — your reviewer just needs to know
  what to expect.

A 5 pp regression on `openmed` leak rate (or equivalent on the other
gold corpora) is a meaningful red flag and will block merge unless
there's a deliberate reason.

## Releases

Releases are tagged from `main` by maintainers. If your PR is
user-visible, add an entry under `## [Unreleased]` in
[`CHANGELOG.md`](CHANGELOG.md) under the appropriate Keep-a-Changelog
heading (Added / Changed / Deprecated / Removed / Fixed / Security).

## Questions

Open a [question issue](.github/ISSUE_TEMPLATE/question.md) or — for
anything that doesn't fit an issue template — just open a regular
issue. We'd rather see a half-formed question than not see it.
