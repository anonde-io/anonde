package recognizers

// NewAUTFNRecognizer detects Australian Tax File Numbers (8 or 9 digits).
// We only validate the 9-digit form via checksum; 8-digit (legacy) hits land
// at a low score.
func NewAUTFNRecognizer() *validatedRecognizer {
	r := NewValidatedRecognizer(
		"AUTFNRecognizer",
		"AU_TFN",
		[]string{"en"},
		[][2]any{
			{`\b\d{3}\s?\d{3}\s?\d{3}\b`, 0.4},
			{`\b\d{3}\s?\d{2}\s?\d{3}\b`, 0.3},
		},
		[]string{"tfn", "tax file number"},
	)
	r.Normalize = stripSeparators
	r.Validate = func(s string) (bool, float64) {
		if validateAUTFN(s) {
			return true, 1.0
		}
		return false, 0
	}
	return r
}
