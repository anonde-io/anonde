package operators

// Keep is a detect-but-don't-anonymize operator. A span assigned the Keep
// operator is DETECTED (it still flows through the analyzer and is visible to
// the caller in the recognizer results / detection list) but its text is left
// VERBATIM in the output: no replacement is written and no AnonymizedItem is
// emitted, so the caller's reverse map never records it.
//
// The motivating cases are entity types that are operationally a leak to
// surface on a dashboard, yet harmful to rewrite: URLs and timestamps corrupt
// downstream LLM prompts when tokenized, and generic IDs flood the reversible
// vault with low-value entries. Keep lets those types stay on the leak list
// and in detection metrics while skipping the body rewrite and the vault
// entry.
//
// The anonymizer engine recognizes Keep via the DetectOnly marker interface
// (IsDetectOnly) and short-circuits before invoking Anonymize, so the methods
// below exist only to satisfy the Operator interface; Anonymize returns the
// text unchanged as a defensive fallback.
type Keep struct{}

// Name returns the operator identifier.
func (k *Keep) Name() string { return "keep" }

// Anonymize returns the original text unchanged. The engine short-circuits
// Keep spans before reaching here, so this is a defensive no-op.
func (k *Keep) Anonymize(text, _ string) (string, error) { return text, nil }

// IsDetectOnly marks Keep as a detect-but-don't-anonymize operator. The
// anonymizer engine uses this marker to leave the span verbatim and skip
// emitting an AnonymizedItem (and therefore any caller-side reverse map entry).
func (k *Keep) IsDetectOnly() bool { return true }
