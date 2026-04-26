package recognizers

import "regexp"

var phoneRE = regexp.MustCompile(
	`(?:(?:\+|00)[1-9]\d{0,3}[\s\-.]?)?` + // country code
		`(?:\(?\d{1,4}\)?[\s\-.]?)?` + // area code
		`\d{1,4}[\s\-.]?\d{1,4}[\s\-.]?\d{1,9}`, // subscriber number
)

// NewPhoneRecognizer detects PHONE_NUMBER entities.
func NewPhoneRecognizer() *PatternRecognizer {
	return NewPatternRecognizer(
		"PhoneRecognizer",
		[]string{"PHONE_NUMBER"},
		[]string{"*"},
		[]namedPattern{{re: phoneRE, score: 0.75}},
	)
}
