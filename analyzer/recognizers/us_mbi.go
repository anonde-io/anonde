package recognizers

// NewUSMBIRecognizer detects US Medicare Beneficiary Identifiers (MBI): the
// 11-character token that replaced the SSN-based HICN on Medicare cards.
//
// The regex encodes the strict CMS positional format directly — the
// allowed-letter class [AC-HJKMNP-RT-Y] is A–Z minus the six letters CMS
// excludes (S,L,O,I,B,Z), and alphanumeric positions add 0-9. Because
// positions 2/5/8/9 must be letters, this can never match a bare 11-digit
// number (so no PL_PESEL / generic-numeric collision). The optional 4-3-4
// hyphens of the display form ("1EG4-TE5-MK73") are tolerated, stripped by
// Normalize, then validateUSMBI re-checks the strict format. There is no
// arithmetic checksum — the character-class format IS the precision guard.
//
// Fills the US-healthcare gap Presidio tracks in issue #2136; HIPAA
// Safe-Harbor-aligned.
func NewUSMBIRecognizer() *validatedRecognizer {
	r := NewValidatedRecognizer(
		"USMBIRecognizer",
		"US_MBI",
		[]string{"en"},
		[][2]any{
			{`\b[1-9][AC-HJKMNP-RT-Y][0-9AC-HJKMNP-RT-Y]\d-?` +
				`[AC-HJKMNP-RT-Y][0-9AC-HJKMNP-RT-Y]\d-?` +
				`[AC-HJKMNP-RT-Y][AC-HJKMNP-RT-Y]\d\d\b`, 0.6},
		},
		[]string{"mbi", "medicare", "medicare beneficiary", "medicare number"},
	)
	r.Normalize = stripSeparators
	r.Validate = func(s string) (bool, float64) {
		if validateUSMBI(s) {
			return true, 1.0
		}
		return false, 0
	}
	return r
}
