package recognizers

import "regexp"

var urlRE = regexp.MustCompile(
	`(?i)\b(?:https?|ftp)://[^\s/$.?#].[^\s]*\b|` +
		`\b(?:www\.)[a-zA-Z0-9\-]+(?:\.[a-zA-Z]{2,})+(?:/[^\s]*)?\b`,
)

// NewURLRecognizer detects URL entities.
func NewURLRecognizer() *PatternRecognizer {
	return NewPatternRecognizer(
		"URLRecognizer",
		[]string{"URL"},
		[]string{"*"},
		[]namedPattern{{re: urlRE, score: 0.6}},
	)
}
