package recognizers

import "strings"

// NewITPassportRecognizer detects Italian passport numbers.
// Two letters (commonly YA-YB) + 7 digits.
func NewITPassportRecognizer() *validatedRecognizer {
	r := NewValidatedRecognizer(
		"ITPassportRecognizer",
		"IT_PASSPORT",
		[]string{"en", "it"},
		[][2]any{
			{`\b[A-Za-z]{2}\d{7}\b`, 0.4},
		},
		[]string{"passaporto", "passport", "passport number", "numero passaporto"},
	)
	r.Normalize = strings.ToUpper
	return r
}
