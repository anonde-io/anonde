package recognizers

import "strings"

// NewESNIFRecognizer detects Spanish NIF / DNI (8 digits + control letter).
func NewESNIFRecognizer() *validatedRecognizer {
	r := NewValidatedRecognizer(
		"ESNIFRecognizer",
		"ES_NIF",
		[]string{"en", "es"},
		[][2]any{
			{`\b\d{8}[A-Za-z]\b`, 0.5},
		},
		[]string{"nif", "dni", "documento nacional de identidad", "national id"},
	)
	r.Normalize = func(s string) string { return strings.ToUpper(stripSeparators(s)) }
	r.Validate = func(s string) (bool, float64) {
		if validateESNIF(s) {
			return true, 1.0
		}
		return false, 0
	}
	return r
}
