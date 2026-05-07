package recognizers

// NewAUABNRecognizer detects Australian Business Numbers (11 digits).
func NewAUABNRecognizer() *validatedRecognizer {
	r := NewValidatedRecognizer(
		"AUABNRecognizer",
		"AU_ABN",
		[]string{"en"},
		[][2]any{
			// Canonical 2-3-3-3 grouping.
			{`\b\d{2}\s?\d{3}\s?\d{3}\s?\d{3}\b`, 0.6},
		},
		[]string{"abn", "australian business number"},
	)
	r.Normalize = stripSeparators
	r.Validate = func(s string) (bool, float64) {
		if validateAUABN(s) {
			return true, 1.0
		}
		return false, 0
	}
	return r
}
