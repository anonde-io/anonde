package recognizers

import "regexp"

var macRE = regexp.MustCompile(
	`(?i)\b(?:[0-9a-f]{2}[:\-]){5}[0-9a-f]{2}\b`,
)

// NewMACAddressRecognizer detects MAC_ADDRESS entities.
func NewMACAddressRecognizer() *PatternRecognizer {
	return NewPatternRecognizer(
		"MACAddressRecognizer",
		[]string{"MAC_ADDRESS"},
		[]string{"*"},
		[]namedPattern{{re: macRE, score: 0.9}},
	)
}
