package recognizers

// NewAUACNRecognizer detects Australian Company Numbers (9 digits).
func NewAUACNRecognizer() *validatedRecognizer {
	r := NewValidatedRecognizer(
		"AUACNRecognizer",
		"AU_ACN",
		[]string{"en"},
		[][2]any{
			// Canonical 3-3-3 grouping.
			{`\b\d{3}\s?\d{3}\s?\d{3}\b`, 0.4},
		},
		[]string{"acn", "australian company number"},
	)
	r.Normalize = stripSeparators
	r.Validate = func(s string) (bool, float64) {
		if validateAUACN(s) {
			return true, 1.0
		}
		return false, 0
	}
	return r
}
