package recognizers

import "strings"

// NewITIdentityCardRecognizer detects Italian Carta d'Identità (Identity Card).
// Older format: 2 letters + 7 digits. Electronic format (CIE): 2 letters + 5 digits + 2 letters.
func NewITIdentityCardRecognizer() *validatedRecognizer {
	r := NewValidatedRecognizer(
		"ITIdentityCardRecognizer",
		"IT_IDENTITY_CARD",
		[]string{"en", "it"},
		[][2]any{
			{`\b[A-Za-z]{2}\d{5}[A-Za-z]{2}\b`, 0.4}, // CIE
			{`\b[A-Za-z]{2}\d{7}\b`, 0.3},            // legacy paper card
		},
		[]string{"carta d'identita", "carta di identita", "identity card", "id card", "documento d'identita"},
	)
	r.Normalize = strings.ToUpper
	return r
}
