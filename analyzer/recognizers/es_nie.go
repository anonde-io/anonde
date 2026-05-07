package recognizers

import "strings"

// NewESNIERecognizer detects Spanish NIE (Número de Identidad de Extranjero).
// X/Y/Z + 7 digits + control letter.
func NewESNIERecognizer() *validatedRecognizer {
	r := NewValidatedRecognizer(
		"ESNIERecognizer",
		"ES_NIE",
		[]string{"en", "es"},
		[][2]any{
			{`\b[XxYyZz]\d{7}[A-Za-z]\b`, 0.5},
		},
		[]string{"nie", "numero de identidad de extranjero", "foreigner id"},
	)
	r.Normalize = func(s string) string { return strings.ToUpper(stripSeparators(s)) }
	r.Validate = func(s string) (bool, float64) {
		if validateESNIE(s) {
			return true, 1.0
		}
		return false, 0
	}
	return r
}
