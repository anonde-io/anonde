package recognizers

import (
	"context"
	"testing"
)

func TestISINRecognizer(t *testing.T) {
	r := NewISINRecognizer()

	cases := []struct {
		name      string
		text      string
		wantSpans []string
	}{
		// Real ISINs from publicly-listed companies / ETFs (these
		// validate against the ISO 6166 check digit, so they exercise
		// the validator's accept path).
		{"BASF SE", "BASF SE Aktie DE000BASF111 ausgeführt", []string{"DE000BASF111"}},
		{"Allianz SE", "Allianz SE  DE0008404005 25 Stück", []string{"DE0008404005"}},
		{"iShares MSCI World", "iShares IE00B4L5Y983 EUR", []string{"IE00B4L5Y983"}},
		{"Lyxor STOXX Europe 600", "LU0290358497 Lyxor STOXX", []string{"LU0290358497"}},

		// Must NOT match; random 12-char strings that fail the check
		// digit or have an unsupported country code.
		{"random 12-char", "ABCDEFGHIJK1 random fragment", nil},
		{"wrong country", "ZZ0008404005 ZZ is unassigned", nil},
		{"too short", "DE00084040", nil},
		{"too long", "DE0008404005XYZ", nil},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res, err := r.Analyze(context.Background(), tc.text, nil, "")
			if err != nil {
				t.Fatalf("Analyze: %v", err)
			}
			got := make([]string, 0, len(res))
			for _, x := range res {
				got = append(got, tc.text[x.Start:x.End])
			}
			if len(tc.wantSpans) == 0 {
				if len(got) > 0 {
					t.Fatalf("expected no match, got %v", got)
				}
				return
			}
			for _, want := range tc.wantSpans {
				found := false
				for _, g := range got {
					if g == want {
						found = true
						break
					}
				}
				if !found {
					t.Fatalf("missing %q in %v", want, got)
				}
			}
		})
	}
}
