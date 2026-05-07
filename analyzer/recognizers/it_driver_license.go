package recognizers

import "strings"

// NewITDriverLicenseRecognizer detects Italian driver license numbers.
// Format: 1 letter + "A" + 7 alphanumerics + 1 letter (10 chars total).
func NewITDriverLicenseRecognizer() *validatedRecognizer {
	r := NewValidatedRecognizer(
		"ITDriverLicenseRecognizer",
		"IT_DRIVER_LICENSE",
		[]string{"en", "it"},
		[][2]any{
			{`\b[A-Za-z][Aa][A-Za-z0-9]{7}[A-Za-z]\b`, 0.4},
		},
		[]string{"patente", "patente di guida", "driver license", "driver licence", "driving licence"},
	)
	r.Normalize = func(s string) string { return strings.ToUpper(stripSeparators(s)) }
	return r
}
