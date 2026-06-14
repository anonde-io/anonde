//go:build hugot

package recognizers

import "testing"

// TestTrimPersonLeadingNonName covers the PERSON left-boundary trim that
// strips a leading title / role common noun ("Customer", "Mr", …) glued into
// the front of a GLiNER PERSON span, while leaving real multi-token names
// untouched. The live repro is "Customer john doe" @ 1.000 -> "john doe".
func TestTrimPersonLeadingNonName(t *testing.T) {
	cases := []struct {
		name     string
		text     string
		wantSpan string // span after trim (the bytes [newStart, end])
		wantTrim bool
	}{
		// --- should trim --------------------------------------------------
		{"customer prefix", "Customer john doe", "john doe", true},
		{"title Mr", "Mr Smith", "Smith", true},
		{"title Dr two names", "Dr Sarah Williams", "Sarah Williams", true},
		{"client lowercase", "client Anita Brown", "Anita Brown", true},
		{"ticket prefix", "Ticket Jane Roe", "Jane Roe", true},

		// --- must NOT trim real multi-token names -------------------------
		{"three real names", "Mary Jane Watson", "Mary Jane Watson", false},
		{"hyphenated given", "Jean-Luc Picard", "Jean-Luc Picard", false},
		{"plain two names", "Anita Brown", "Anita Brown", false},
		{"given surname", "John Smith", "John Smith", false},

		// --- guards -------------------------------------------------------
		{"single word never trimmed", "Customer", "Customer", false},
		{"lead in set but next not a name", "Account 12345", "Account 12345", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			words := splitWords(tc.text)
			if len(words) == 0 {
				t.Fatalf("splitWords(%q) returned no words", tc.text)
			}
			start := 0
			end := len(words) - 1
			newStart, trimmed := trimPersonLeadingNonName(tc.text, words, start, end)
			if trimmed != tc.wantTrim {
				t.Fatalf("trimmed = %v, want %v", trimmed, tc.wantTrim)
			}
			got := tc.text[words[newStart].start:words[end].end]
			if got != tc.wantSpan {
				t.Fatalf("span = %q, want %q", got, tc.wantSpan)
			}
		})
	}
}

func TestLooksLikeNameToken(t *testing.T) {
	yes := []string{"john", "Smith", "O'Connor", "Jean-Luc", "Müller"}
	no := []string{"", "12345", "#1240", "-Luc", "'Brien", "INV-22"}
	for _, s := range yes {
		if !looksLikeNameToken(s) {
			t.Errorf("looksLikeNameToken(%q) = false, want true", s)
		}
	}
	for _, s := range no {
		if looksLikeNameToken(s) {
			t.Errorf("looksLikeNameToken(%q) = true, want false", s)
		}
	}
}
