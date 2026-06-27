package anonymizer

// Operator transforms a detected PII span.
type Operator interface {
	// Name returns the operator identifier.
	Name() string
	// Anonymize returns the replacement string for the given PII text and entity type.
	Anonymize(text, entityType string) (string, error)
}

// DetectOnly is an optional marker an Operator may implement to opt into
// "detect-but-don't-anonymize" behavior. When the operator selected for a span
// reports IsDetectOnly() == true, the engine leaves the span's bytes verbatim
// and emits NO AnonymizedItem for it — Anonymize is never invoked and no
// caller-side reverse-map entry is minted. The span is still DETECTED upstream
// (it remains in the recognizer results the caller passed in), so it stays on
// any leak list / detection metric the caller derives from those results.
//
// operators.Keep is the canonical implementation.
type DetectOnly interface {
	IsDetectOnly() bool
}
