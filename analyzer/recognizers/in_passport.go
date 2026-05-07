package recognizers

import "strings"

// NewINPassportRecognizer detects Indian passport numbers.
// 1 letter + 7 digits (newer 8-character format).
func NewINPassportRecognizer() *validatedRecognizer {
	r := NewValidatedRecognizer(
		"INPassportRecognizer",
		"IN_PASSPORT",
		[]string{"en"},
		[][2]any{
			{`\b[A-PR-WYa-pr-wy]\d{7}\b`, 0.5},
		},
		[]string{"passport", "passport number", "indian passport"},
	)
	r.Normalize = strings.ToUpper
	return r
}
