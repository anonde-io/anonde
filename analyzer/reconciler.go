package analyzer

import "context"

// Reconciler post-processes the candidate spans produced by the recognizers
// and returns a possibly-modified set. The typical implementation gates an
// LLM call on borderline-confidence candidates and decides whether to keep
// or drop each one.
//
// Contract:
//   - The reconciler MUST NOT raise the leak rate vs not running at all.
//     On any error or timeout it MUST keep the candidate (fail-open).
//   - The reconciler MAY drop candidates (typical FP-killer use case) and
//     MAY adjust scores; it MUST NOT invent new spans.
//   - Order of the returned slice is not required to be preserved.
//
// A nil Reconciler on AnalyzerEngine means "no reconciliation" — the
// engine skips the call entirely.
type Reconciler interface {
	Reconcile(ctx context.Context, text string, candidates []RecognizerResult) ([]RecognizerResult, error)
}
