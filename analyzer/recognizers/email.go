package recognizers

import "regexp"

var emailRE = regexp.MustCompile(`(?i)\b[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}\b`)

// NewEmailRecognizer detects EMAIL_ADDRESS entities.
func NewEmailRecognizer() *PatternRecognizer {
	return NewPatternRecognizer(
		"EmailRecognizer",
		[]string{"EMAIL_ADDRESS"},
		[]string{"*"},
		[]namedPattern{{re: emailRE, score: 1.0}},
	)
}
