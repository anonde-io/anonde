package recognizers

import "strings"

// NewSGNRICRecognizer detects Singapore NRIC and FIN numbers.
// Format: 1 letter (S/T/F/G/M) + 7 digits + check letter.
func NewSGNRICRecognizer() *validatedRecognizer {
	r := NewValidatedRecognizer(
		"SGNRICRecognizer",
		"SG_NRIC_FIN",
		[]string{"en"},
		[][2]any{
			{`\b[STFGMstfgm]\d{7}[A-Za-z]\b`, 0.6},
		},
		[]string{"nric", "fin", "singapore id", "ic number", "identity card"},
	)
	r.Normalize = strings.ToUpper
	r.Validate = func(s string) (bool, float64) {
		if validateSGNRIC(s) {
			return true, 1.0
		}
		return false, 0
	}
	return r
}
