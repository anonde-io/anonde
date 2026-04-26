package operators

// Redact removes the PII span entirely.
type Redact struct{}

func (r *Redact) Name() string                                   { return "redact" }
func (r *Redact) Anonymize(_ string, _ string) (string, error)   { return "", nil }
