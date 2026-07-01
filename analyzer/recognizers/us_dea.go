package recognizers

// NewUSDEARecognizer detects US DEA registration numbers issued to prescribers
// of controlled substances: two letters (registrant-type letter + a
// letter-or-9) followed by seven digits, where the 7th digit is a checksum
// over the other six (validateUSDEA). The checksum gate is the precision
// guard; the surface alone (2 letters + 7 digits) would over-fire on ordinary
// alphanumeric product / order codes, so a checksum failure drops the finding.
//
// Fills the US-healthcare gap Presidio tracks in issue #2136; HIPAA
// Safe-Harbor-aligned.
func NewUSDEARecognizer() *validatedRecognizer {
	r := NewValidatedRecognizer(
		"USDEARecognizer",
		"US_DEA",
		[]string{"en"},
		[][2]any{
			// [A-Z][A-Z9][0-9]{7}: registrant-type letter, last-name-initial
			// (or 9), then 7 digits incl. the trailing check digit.
			{`\b[A-Z][A-Z9]\d{7}\b`, 0.4},
		},
		[]string{"dea", "dea number", "dea registration", "prescriber", "controlled substance"},
	)
	r.Validate = func(s string) (bool, float64) {
		if validateUSDEA(s) {
			return true, 1.0
		}
		return false, 0
	}
	return r
}
