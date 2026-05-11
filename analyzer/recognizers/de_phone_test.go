package recognizers

import (
	"context"
	"testing"
)

func TestDEPhoneRecognizer(t *testing.T) {
	r := NewDEPhoneRecognizer()

	cases := []struct {
		name      string
		text      string
		wantMatch string // exact substring, "" = no match
	}{
		// GraSCCo-observed positives.
		{"slash separator", "Tel.: 08991/23354", "08991/23354"},
		{"area-hyphen-local", "Mobil: 0699-15099887", "0699-15099887"},
		{"parenthesized area", "Tel. (0461) 708 - 223 erreichbar", "(0461) 708 - 223"},
		{"area-space-local-hyphen-ext", "Fax 0261 210-39989", "0261 210-39989"},
		{"intl AT parens trunk-zero", "Telefon +43(0)333 775-8422334", "+43(0)333 775-8422334"},
		{"intl AT parens area", "Direkt +43 (453) 14", "+43 (453) 14"},

		// Negative cases.
		{"plain bare number", "Patient 0815 hat...", ""},
		{"date-like", "vom 01.02.2023 bis...", ""},
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
