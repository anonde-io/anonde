package recognizers

import "strings"

// NewSGUENRecognizer detects Singapore Unique Entity Numbers.
// Three formats:
//   - Local companies: 9 digits + 1 letter (e.g., 200012345A)
//   - Businesses: 8 digits + 1 letter (e.g., 12345678A)
//   - Other entities: T/S/R + 2 digits + 2 letters + 4 digits + letter
//
// Without an official checksum spec, we rely on format match only.
func NewSGUENRecognizer() *validatedRecognizer {
	r := NewValidatedRecognizer(
		"SGUENRecognizer",
		"SG_UEN",
		[]string{"en"},
		[][2]any{
			{`\b\d{9}[A-Za-z]\b`, 0.4},                         // local companies
			{`\b\d{8}[A-Za-z]\b`, 0.3},                         // businesses
			{`\b[TtSsRr]\d{2}[A-Za-z]{2}\d{4}[A-Za-z]\b`, 0.5}, // other entities
		},
		[]string{"uen", "unique entity number", "company registration"},
	)
	r.Normalize = strings.ToUpper
	return r
}
