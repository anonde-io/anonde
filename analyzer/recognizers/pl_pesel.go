package recognizers

// NewPLPESELRecognizer detects Polish PESEL numbers (11-digit national ID).
func NewPLPESELRecognizer() *validatedRecognizer {
	r := NewValidatedRecognizer(
		"PLPESELRecognizer",
		"PL_PESEL",
		[]string{"en", "pl"},
		[][2]any{
			{`\b\d{11}\b`, 0.3},
		},
		[]string{"pesel", "numer pesel", "national id"},
	)
	r.Normalize = stripSeparators
	r.Validate = func(s string) (bool, float64) {
		if validatePLPESEL(s) {
			return true, 1.0
		}
		return false, 0
	}
	return r
}
