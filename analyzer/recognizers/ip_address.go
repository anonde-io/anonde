package recognizers

import "regexp"

var (
	ipv4RE = regexp.MustCompile(
		`\b(?:(?:25[0-5]|2[0-4]\d|[01]?\d\d?)\.){3}(?:25[0-5]|2[0-4]\d|[01]?\d\d?)\b`,
	)
	ipv6RE = regexp.MustCompile(
		`(?i)\b(?:[0-9a-f]{1,4}:){7}[0-9a-f]{1,4}\b|` +
			`(?:[0-9a-f]{1,4}:){1,7}:|` +
			`(?:[0-9a-f]{1,4}:){1,6}:[0-9a-f]{1,4}|` +
			`::(?:[0-9a-f]{1,4}:){0,5}[0-9a-f]{1,4}`,
	)
)

// NewIPAddressRecognizer detects IP_ADDRESS (both v4 and v6).
func NewIPAddressRecognizer() *PatternRecognizer {
	return NewPatternRecognizer(
		"IPAddressRecognizer",
		[]string{"IP_ADDRESS"},
		[]string{"*"},
		[]namedPattern{
			{re: ipv4RE, score: 0.95},
			{re: ipv6RE, score: 0.95},
		},
	)
}
