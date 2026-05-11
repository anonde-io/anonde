package recognizers

import (
	"context"
	"testing"
)

func TestDEDateTimeRecognizer(t *testing.T) {
	r := NewDEDateTimeRecognizer()

	cases := []struct {
		name      string
		text      string
		wantMatch string // exact substring expected to be matched, "" = no match
	}{
		// Numeric DD.MM.YYYY family (the bulk of GraSCCo dates).
		{"DD.MM.YYYY four-digit year", "Termin am 01.02.2028.", "01.02.2028"},
		{"D.M.YYYY no leading zeros", "geboren 4.4.1997.", "4.4.1997"},
		{"D.M.YY two-digit year", "Aufnahme 5.7.54.", "5.7.54"},
		{"DD.MM YYYY space typo", "Datum: 23.04 2029.", "23.04 2029"},

		// Textual month family.
		{"DD. Monat YYYY full", "Operiert am 27. März 2025.", "27. März 2025"},
		{"DD. Monat YYYY abbrev", "vom 12. Sep 2024.", "12. Sep 2024"},
		{"DD. Monat YYYY ASCII ae", "am 12. Maerz 2024.", "12. Maerz 2024"},
		{"Monat YYYY no day", "Diagnose im November 2018 gestellt.", "November 2018"},

		// Negative cases — must NOT match.
		{"version number", "Library 1.2.3 installed.", ""},
		{"bare year alone", "Studium 2007.", ""},
		{"section number", "siehe 19.3. der Anlage.", ""},
		{"out-of-range day", "Termin 32.05.2023.", ""},
		{"out-of-range month", "Termin 12.13.2023.", ""},
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
			if res[0].EntityType != "DATE_TIME" {
				t.Fatalf("entity type %q, want DATE_TIME", res[0].EntityType)
			}
		})
	}
}
