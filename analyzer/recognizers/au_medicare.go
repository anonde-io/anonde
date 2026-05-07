package recognizers

// NewAUMedicareRecognizer detects Australian Medicare card numbers (10 digits,
// optional 1-digit IRN). 4-5-1 grouping is the canonical printed form.
func NewAUMedicareRecognizer() *validatedRecognizer {
	r := NewValidatedRecognizer(
		"AUMedicareRecognizer",
		"AU_MEDICARE",
		[]string{"en"},
		[][2]any{
			{`\b[2-6]\d{3}\s?\d{5}\s?\d\b`, 0.6},
		},
		[]string{"medicare", "medicare number", "medicare card"},
	)
	r.Normalize = stripSeparators
	r.Validate = func(s string) (bool, float64) {
		// validateAUMedicare expects exactly 10 digits.
		if len(s) > 10 {
			s = s[:10]
		}
		if validateAUMedicare(s) {
			return true, 1.0
		}
		return false, 0
	}
	return r
}
