package analyzer

import "context"

// Auditor is an optional final-pass detector that looks for PII the
// regex+NER stack missed. It runs AFTER the main analyzer pipeline
// (recognizers + reconciler + threshold + conflicts) and appends its
// findings to the result set.
//
// Distinct from Reconciler:
//   - Reconciler decides KEEP/DROP on already-found candidates.
//   - Auditor finds NEW candidates the rest of the stack missed.
//
// Contract:
//   - The auditor MUST NOT modify existing findings; it only appends.
//   - On any error or timeout it MUST return an empty slice (fail-open
//     in the recall direction is acceptable here — the worst case is
//     that the auditor adds nothing, which is exactly the no-auditor
//     baseline).
//   - Emitted spans MUST have valid byte offsets in text (the caller
//     can append them directly to RecognizerResult lists).
//
// Typical implementation: prompt a local LLM with the original text +
// the list of already-found spans, ask for any remaining PII, substring-
// match its replies back to spans, emit.
type Auditor interface {
	Audit(ctx context.Context, text string, known []RecognizerResult) ([]RecognizerResult, error)
}
