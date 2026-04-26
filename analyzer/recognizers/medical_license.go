package recognizers

import "regexp"

// US medical license: state abbreviation + optional hyphen + 5-10 alphanumeric.
var medLicenseRE = regexp.MustCompile(
	`\b[A-Z]{2}-?\d{5,10}\b`,
)

// NewMedicalLicenseRecognizer detects MEDICAL_LICENSE entities.
func NewMedicalLicenseRecognizer() *PatternRecognizer {
	return NewPatternRecognizer(
		"MedicalLicenseRecognizer",
		[]string{"MEDICAL_LICENSE"},
		[]string{"en"},
		[]namedPattern{{re: medLicenseRE, score: 0.5}},
	)
}
