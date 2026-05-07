package recognizers

import "strings"

// NewINVoterRecognizer detects Indian Voter ID (EPIC) numbers.
// Format: 3 letters + 7 digits. Older formats varied; we accept the standard.
func NewINVoterRecognizer() *validatedRecognizer {
	r := NewValidatedRecognizer(
		"INVoterRecognizer",
		"IN_VOTER",
		[]string{"en"},
		[][2]any{
			{`\b[A-Za-z]{3}\d{7}\b`, 0.5},
		},
		[]string{"voter id", "epic", "epic number", "voter card", "election id"},
	)
	r.Normalize = strings.ToUpper
	return r
}
