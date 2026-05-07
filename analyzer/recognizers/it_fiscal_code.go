package recognizers

import "strings"

// NewITFiscalCodeRecognizer detects Italian Codice Fiscale (16 chars).
func NewITFiscalCodeRecognizer() *validatedRecognizer {
	r := NewValidatedRecognizer(
		"ITFiscalCodeRecognizer",
		"IT_FISCAL_CODE",
		[]string{"en", "it"},
		[][2]any{
			{`\b[A-Za-z]{6}\d{2}[A-Za-z]\d{2}[A-Za-z]\d{3}[A-Za-z]\b`, 0.5},
		},
		[]string{"codice fiscale", "fiscal code", "cf", "tax code"},
	)
	r.Normalize = func(s string) string { return strings.ToUpper(stripSeparators(s)) }
	r.Validate = func(s string) (bool, float64) {
		if validateITFiscalCode(s) {
			return true, 1.0
		}
		return false, 0
	}
	return r
}
