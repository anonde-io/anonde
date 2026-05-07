package recognizers

// NewINAadhaarRecognizer detects Indian Aadhaar numbers (12 digits, Verhoeff).
// Per UIDAI spec, Aadhaar can never start with 0 or 1.
func NewINAadhaarRecognizer() *validatedRecognizer {
	r := NewValidatedRecognizer(
		"INAadhaarRecognizer",
		"IN_AADHAAR",
		[]string{"en"},
		[][2]any{
			// Canonical 4-4-4 grouping.
			{`\b[2-9]\d{3}\s?\d{4}\s?\d{4}\b`, 0.6},
		},
		[]string{"aadhaar", "aadhar", "uid", "uidai"},
	)
	r.Normalize = stripSeparators
	r.Validate = func(s string) (bool, float64) {
		if len(s) != 12 {
			return false, 0
		}
		if s[0] == '0' || s[0] == '1' {
			return false, 0
		}
		if validateVerhoeff(s) {
			return true, 1.0
		}
		return false, 0
	}
	return r
}
