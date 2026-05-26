package recognizers

// NewUKNHSRecognizer detects UK National Health Service (NHS) numbers.
// 10-digit identifier with mod-11 weighted checksum.
func NewUKNHSRecognizer() *validatedRecognizer {
	r := NewValidatedRecognizer(
		"UKNHSRecognizer",
		"UK_NHS",
		[]string{"en"},
		[][2]any{
			// Strong: standard 3-3-4 grouping with separators ("123 456 7890" or "123-456-7890").
			{`\b\d{3}[ -]\d{3}[ -]\d{4}\b`, 0.7},
			// Medium: bare 10 digits; common in dumps but more false-positive-prone.
			{`\b\d{10}\b`, 0.3},
		},
		[]string{"nhs", "national health", "patient id", "patient number", "health number"},
	)
	r.Normalize = stripSeparators
	r.Validate = func(s string) (bool, float64) {
		if validateUKNHS(s) {
			return true, 1.0
		}
		return false, 0
	}
	return r
}
