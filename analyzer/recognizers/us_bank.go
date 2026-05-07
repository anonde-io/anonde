package recognizers

import "regexp"

// US bank account numbers are typically 8-17 digits.
var usBankRE = regexp.MustCompile(`\b\d{8,17}\b`)

// NewUSBankRecognizer detects US_BANK_NUMBER entities (low confidence, context-dependent).
func NewUSBankRecognizer() *PatternRecognizer {
	return NewPatternRecognizerWithContext(
		"USBankRecognizer",
		[]string{"US_BANK_NUMBER"},
		[]string{"en"},
		[]namedPattern{{re: usBankRE, score: 0.3}},
		[]string{"bank", "account", "checking", "savings", "routing", "ach", "wire"},
	)
}
