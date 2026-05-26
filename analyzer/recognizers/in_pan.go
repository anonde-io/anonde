package recognizers

import "strings"

// NewINPANRecognizer detects Indian Permanent Account Numbers.
// Format: AAAAA9999A; 5 letters, 4 digits, 1 letter.
func NewINPANRecognizer() *validatedRecognizer {
	r := NewValidatedRecognizer(
		"INPANRecognizer",
		"IN_PAN",
		[]string{"en"},
		[][2]any{
			{`\b[A-Za-z]{5}\d{4}[A-Za-z]\b`, 0.6},
		},
		[]string{"pan", "pan number", "permanent account number"},
	)
	r.Normalize = strings.ToUpper
	r.Validate = func(s string) (bool, float64) {
		if validateINPAN(s) {
			return true, 1.0
		}
		return false, 0
	}
	return r
}
