package recognizers

import (
	"context"
	"testing"
)

// TestValidateDESteuerID exercises the ISO 7064 MOD 11,10 checksum and the
// BMF uniqueness rule directly.
func TestValidateDESteuerID(t *testing.T) {
	// Valid official examples from BMF documentation.
	valid := []string{
		"86095742719",
		"47036892816",
	}
	for _, s := range valid {
		if !validateDESteuerID(s) {
			t.Errorf("expected %q to be valid Steuer-ID", s)
		}
	}

	invalid := []struct {
		s    string
		why  string
	}{
		{"00000000000", "all zeros; fails uniqueness"},
		{"12345678901", "no repeated digit; fails uniqueness"},
		{"11111111111", "one digit repeated 11x; fails (≥4 rule)"},
		{"86095742718", "wrong check digit"},
		{"8609574271",  "too short"},
		{"860957427199", "too long"},
		{"8609574271A", "non-digit"},
	}
	for _, tc := range invalid {
		if validateDESteuerID(tc.s) {
			t.Errorf("expected %q to be INVALID (%s)", tc.s, tc.why)
		}
	}
}

// TestDESteuerIDRecognizer verifies regex + checksum-gated emission.
func TestDESteuerIDRecognizer(t *testing.T) {
	r := NewDESteuerIDRecognizer()

	cases := []struct {
		name      string
		text      string
		wantMatch string // exact substring, "" = no match
	}{
		{"valid no separators", "Steuer-ID: 86095742719", "86095742719"},
		{"valid space-separated", "TIN 86 095 742 719 vorhanden", "86 095 742 719"},

		// Wrong checksum → must NOT emit.
		{"invalid checksum", "Nummer 86095742718 angegeben", ""},

		// 11-digit number failing uniqueness rule.
		{"all zeros", "Akte 00000000000", ""},
		{"no repeats", "Akte 12345678901", ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res, err := r.Analyze(context.Background(), tc.text, nil, "de")
			if err != nil {
				t.Fatalf("Analyze: %v", err)
			}
			if tc.wantMatch == "" {
				if len(res) > 0 {
					t.Fatalf("expected no match, got %d: %+v", len(res), res)
				}
				return
			}
			if len(res) == 0 {
				t.Fatalf("expected match %q, got none", tc.wantMatch)
			}
			got := tc.text[res[0].Start:res[0].End]
			if got != tc.wantMatch {
				t.Fatalf("matched %q, want %q", got, tc.wantMatch)
			}
		})
	}
}
