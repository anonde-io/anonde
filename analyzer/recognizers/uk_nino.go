package recognizers

import "strings"

// NewUKNINORecognizer detects UK National Insurance Numbers.
// Format: 2 letters + 6 digits + 1 letter (A-D), with a small list of
// disallowed prefixes. There's no checksum — validation is by structure.
func NewUKNINORecognizer() *validatedRecognizer {
	r := NewValidatedRecognizer(
		"UKNINORecognizer",
		"UK_NINO",
		[]string{"en"},
		[][2]any{
			// Strong: with optional spaces between groups, e.g. "QQ 12 34 56 C".
			{`\b[A-CEGHJ-PR-TW-Z][A-CEGHJ-NPR-TW-Z]\s?\d{2}\s?\d{2}\s?\d{2}\s?[A-D]\b`, 0.85},
		},
		[]string{"national insurance", "ni number", "nino"},
	)
	r.Normalize = func(s string) string { return strings.ToUpper(stripSeparators(s)) }
	r.Validate = func(s string) (bool, float64) {
		if len(s) != 9 {
			return false, 0
		}
		// Disallowed first letters per HMRC.
		switch s[0] {
		case 'D', 'F', 'I', 'Q', 'U', 'V':
			return false, 0
		}
		switch s[1] {
		case 'D', 'F', 'I', 'O', 'Q', 'U', 'V':
			return false, 0
		}
		// Disallowed prefixes (BG, GB, KN, NK, NT, TN, ZZ).
		switch s[:2] {
		case "BG", "GB", "KN", "NK", "NT", "TN", "ZZ":
			return false, 0
		}
		return true, 1.0
	}
	return r
}
