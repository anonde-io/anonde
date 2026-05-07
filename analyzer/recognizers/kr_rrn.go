package recognizers

// NewKRRRNRecognizer detects Korean Resident Registration Numbers
// (주민등록번호) — 13 digits, often formatted YYMMDD-Sxxxxxx.
func NewKRRRNRecognizer() *validatedRecognizer {
	r := NewValidatedRecognizer(
		"KRRRNRecognizer",
		"KR_RRN",
		[]string{"en", "ko"},
		[][2]any{
			{`\b\d{6}-?\d{7}\b`, 0.5},
		},
		[]string{"rrn", "resident registration", "주민등록번호", "주민번호"},
	)
	r.Normalize = stripSeparators
	r.Validate = func(s string) (bool, float64) {
		if validateKRRRN(s) {
			return true, 1.0
		}
		return false, 0
	}
	return r
}
