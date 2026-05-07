package recognizers

import "regexp"

// phoneRE matches three concrete phone formats. The previous catch-all
// regex matched any 6+ digit sequence including credit cards and random
// numbers, which dropped precision into single digits on real corpora.
//
// Patterns (each with a clear start anchor):
//
//	1) International:    +<country>[ -.]<digits>...   e.g. "+1-800-555-0199"
//	2) US-style numeric: NNN-NNN-NNNN                 e.g. "212-555-0143"
//	3) Parenthesized:    (NNN) NNN-NNNN               e.g. "(415) 555-2671"
var phoneRE = regexp.MustCompile(
	`\+\d{1,3}[-.\s]?\d{1,4}[-.\s]?\d{1,4}[-.\s]?\d{1,9}` +
		`|\b\d{3}[-.\s]\d{3}[-.\s]\d{4}\b` +
		`|\(\d{2,4}\)\s?\d{3,4}[-.\s]?\d{3,4}`,
)

// NewPhoneRecognizer detects PHONE_NUMBER entities.
func NewPhoneRecognizer() *PatternRecognizer {
	return NewPatternRecognizerWithContext(
		"PhoneRecognizer",
		[]string{"PHONE_NUMBER"},
		[]string{"*"},
		[]namedPattern{{re: phoneRE, score: 0.75}},
		[]string{"phone", "telephone", "tel", "mobile", "cell", "fax", "call", "contact", "number"},
	)
}
