package recognizers

// NewITVATCodeRecognizer detects Italian VAT numbers (Partita IVA, 11 digits).
//
// We only match with the "IT" prefix or near explicit context (handled by
// the engine's context enhancer). A bare 11-digit pattern here would collide
// with PL_PESEL — both validators happen to accept some of the same
// strings — so we keep the regex strict and rely on context to win
// disambiguation.
func NewITVATCodeRecognizer() *validatedRecognizer {
	r := NewValidatedRecognizer(
		"ITVATCodeRecognizer",
		"IT_VAT_CODE",
		[]string{"en", "it"},
		[][2]any{
			{`\bIT\s?\d{11}\b`, 0.6},
		},
		[]string{"partita iva", "p.iva", "p iva", "vat", "vat number", "codice iva"},
	)
	r.Normalize = func(s string) string {
		s = stripSeparators(s)
		// Drop the optional country prefix.
		if len(s) == 13 && (s[0] == 'I' || s[0] == 'i') && (s[1] == 'T' || s[1] == 't') {
			return s[2:]
		}
		return s
	}
	r.Validate = func(s string) (bool, float64) {
		if validateITVATCode(s) {
			return true, 1.0
		}
		return false, 0
	}
	return r
}
