package recognizers

import "strings"

// NewFIPersonalIdentityCodeRecognizer detects Finnish HETU (henkilötunnus).
// Format: DDMMYY + century separator (+/-/A/Y/etc.) + 3 digits + check char (11 chars total).
func NewFIPersonalIdentityCodeRecognizer() *validatedRecognizer {
	r := NewValidatedRecognizer(
		"FIPersonalIdentityCodeRecognizer",
		"FI_PERSONAL_IDENTITY_CODE",
		[]string{"en", "fi"},
		[][2]any{
			{`\b\d{6}[+\-AaYyXxWwVvUuBbCcDdEeFf]\d{3}[A-Za-z0-9]\b`, 0.6},
		},
		[]string{"hetu", "henkilotunnus", "henkilötunnus", "personal identity code", "personal id"},
	)
	r.Normalize = strings.ToUpper
	r.Validate = func(s string) (bool, float64) {
		if validateFIHETU(s) {
			return true, 1.0
		}
		return false, 0
	}
	return r
}
