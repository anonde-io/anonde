package recognizers

import "regexp"

var usPassportRE = regexp.MustCompile(`\b[A-Z]\d{8}\b`)

// NewUSPassportRecognizer detects US_PASSPORT entities.
func NewUSPassportRecognizer() *PatternRecognizer {
	return NewPatternRecognizerWithContext(
		"USPassportRecognizer",
		[]string{"US_PASSPORT"},
		[]string{"en"},
		[]namedPattern{{re: usPassportRE, score: 0.5}},
		[]string{"passport", "us passport", "passport number", "travel document"},
	)
}
