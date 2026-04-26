package recognizers

import "regexp"

// US driver's license formats vary by state; this covers common patterns.
var usDriverLicenseRE = regexp.MustCompile(
	`\b[A-Z]\d{7}\b|` + // e.g. California, Texas
		`\b[A-Z]{2}\d{6}\b|` + // e.g. Virginia
		`\b\d{9}\b`, // numeric-only states
)

// NewUSDriverLicenseRecognizer detects US_DRIVER_LICENSE entities.
func NewUSDriverLicenseRecognizer() *PatternRecognizer {
	return NewPatternRecognizer(
		"USDriverLicenseRecognizer",
		[]string{"US_DRIVER_LICENSE"},
		[]string{"en"},
		[]namedPattern{{re: usDriverLicenseRE, score: 0.6}},
	)
}
