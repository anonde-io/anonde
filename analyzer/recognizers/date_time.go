package recognizers

import "regexp"

var dateTimeRE = regexp.MustCompile(
	// ISO 8601
	`\b\d{4}-(?:0[1-9]|1[0-2])-(?:0[1-9]|[12]\d|3[01])(?:[T ]\d{2}:\d{2}(?::\d{2})?(?:Z|[+-]\d{2}:?\d{2})?)?\b|` +
		// MM/DD/YYYY and DD/MM/YYYY
		`\b(?:0?[1-9]|1[0-2])[/\-.](?:0?[1-9]|[12]\d|3[01])[/\-.]\d{2,4}\b|` +
		// Month name DD, YYYY
		`\b(?:Jan(?:uary)?|Feb(?:ruary)?|Mar(?:ch)?|Apr(?:il)?|May|Jun(?:e)?|Jul(?:y)?|Aug(?:ust)?|Sep(?:tember)?|Oct(?:ober)?|Nov(?:ember)?|Dec(?:ember)?)\s+\d{1,2},?\s+\d{4}\b|` +
		// DD Month YYYY
		`\b\d{1,2}\s+(?:Jan(?:uary)?|Feb(?:ruary)?|Mar(?:ch)?|Apr(?:il)?|May|Jun(?:e)?|Jul(?:y)?|Aug(?:ust)?|Sep(?:tember)?|Oct(?:ober)?|Nov(?:ember)?|Dec(?:ember)?)\s+\d{4}\b`,
)

// NewDateTimeRecognizer detects DATE_TIME entities.
func NewDateTimeRecognizer() *PatternRecognizer {
	return NewPatternRecognizer(
		"DateTimeRecognizer",
		[]string{"DATE_TIME"},
		[]string{"*"},
		[]namedPattern{{re: dateTimeRE, score: 0.85}},
	)
}
