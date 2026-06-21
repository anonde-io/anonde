package recognizers

import (
	"regexp"
	"strings"
)

// itDLPureHexRE matches a normalized IT-driver-license candidate whose every
// character is a hex digit (0-9 A-F). The format (letter+A+7alnum+letter) is
// checksum-free, so a 10-char hex run — a UUID-segment fragment, a hash slice,
// or any structural hex ID where positions happen to fit — collides with it.
// A pure-hex surface is provably structural, never a real licence number as a
// human writes it (a genuine IT licence's bounding letters are virtually never
// both in A-F with an all-hex middle). Provably-structural-only: a single
// non-hex char (G-Z, the common case) keeps the candidate.
var itDLPureHexRE = regexp.MustCompile(`^[0-9A-F]{10}$`)

// NewITDriverLicenseRecognizer detects Italian driver license numbers.
// Format: 1 letter + "A" + 7 alphanumerics + 1 letter (10 chars total).
func NewITDriverLicenseRecognizer() *validatedRecognizer {
	r := NewValidatedRecognizer(
		"ITDriverLicenseRecognizer",
		"IT_DRIVER_LICENSE",
		[]string{"en", "it"},
		[][2]any{
			{`\b[A-Za-z][Aa][A-Za-z0-9]{7}[A-Za-z]\b`, 0.4},
		},
		[]string{"patente", "patente di guida", "driver license", "driver licence", "driving licence"},
	)
	r.Normalize = func(s string) string { return strings.ToUpper(stripSeparators(s)) }
	// Structural-collision guard: drop a candidate that is a pure hex run
	// (UUID fragment / hex blob / structural ID). Leak-safe — these are never
	// real licence numbers; (false, 0) drops the finding entirely.
	r.Validate = func(s string) (bool, float64) {
		if itDLPureHexRE.MatchString(s) {
			return false, 0
		}
		return true, 0.4
	}
	return r
}
