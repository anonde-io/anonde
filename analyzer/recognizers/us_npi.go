package recognizers

// NewUSNPIRecognizer detects US National Provider Identifiers (NPI): the
// 10-digit identifier CMS/NPPES issues to healthcare providers. The 10th
// digit is a Luhn check over the "80840" issuer prefix + first 9 digits
// (validateUSNPI), so a random 10-digit number is rejected. But Luhn alone
// still passes ~1-in-10 random 10-digit numbers, so — unlike credit_card /
// uk_nhs whose surfaces are more constrained — NPI additionally REQUIRES a
// context keyword nearby (RequireContext): a bare Luhn-valid 10-digit number
// with no "npi"/"provider" context is dropped, keeping it high-precision on
// general traffic instead of over-redacting arbitrary 10-digit IDs.
//
// Fills the US-healthcare gap Presidio tracks in issue #2136; HIPAA
// Safe-Harbor-aligned.
func NewUSNPIRecognizer() *validatedRecognizer {
	r := NewValidatedRecognizer(
		"USNPIRecognizer",
		"US_NPI",
		[]string{"en"},
		[][2]any{
			// Bare 10 digits; the Luhn+80840 checksum is the real gate.
			{`\b\d{10}\b`, 0.3},
		},
		[]string{"npi", "national provider identifier", "provider id", "provider number"},
	)
	r.Normalize = stripSeparators
	r.Validate = func(s string) (bool, float64) {
		if validateUSNPI(s) {
			return true, 1.0
		}
		return false, 0
	}
	// Luhn on a bare 10-digit surface is too permissive to run context-free;
	// require an "npi"/"provider" keyword nearby (see the type doc on
	// validatedRecognizer.RequireContext).
	r.RequireContext = true
	return r
}
