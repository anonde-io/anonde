package anonymizer

// Operator transforms a detected PII span.
type Operator interface {
	// Name returns the operator identifier.
	Name() string
	// Anonymize returns the replacement string for the given PII text and entity type.
	Anonymize(text, entityType string) (string, error)
}
