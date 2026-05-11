package recognizers

// NewDESteuerIDRecognizer detects German Steuer-ID (Steuerliche
// Identifikationsnummer): an 11-digit tax-identification number issued to
// every German resident.
//
// Pattern accepts the canonical groupings used in practice:
//
//	12345678901       no separators
//	12 345 678 901    space-separated 2-3-3-3
//	12 345 678 90 1   space-separated 2-3-3-2-1
//
// After regex match the candidate is normalised (separators stripped) and
// validated against ISO 7064 MOD 11,10 plus the BMF uniqueness rule
// (validateDESteuerID in checksums.go). Without the checksum, false-positive
// rate on any 11-digit number in a clinical document would be unacceptable.
func NewDESteuerIDRecognizer() *validatedRecognizer {
	r := NewValidatedRecognizer(
		"DESteuerIDRecognizer",
		"DE_STEUER_ID",
		[]string{"de"},
		[][2]any{
			// 11 digits, optional space or hyphen separators every 2-3 digits.
			{`\b\d{2}[\s-]?\d{3}[\s-]?\d{3}[\s-]?\d{3}\b`, 0.5},
			// Variant 2-3-3-2-1 (less common).
			{`\b\d{2}[\s-]?\d{3}[\s-]?\d{3}[\s-]?\d{2}[\s-]?\d{1}\b`, 0.5},
		},
		[]string{
			"steuer-id", "steueridentifikationsnummer",
			"steuerliche identifikationsnummer",
			"steuernummer", "tin", "tax id",
		},
	)
	r.Normalize = stripSeparators
	r.Validate = func(s string) (bool, float64) {
		if validateDESteuerID(s) {
			return true, 1.0
		}
		return false, 0
	}
	return r
}
