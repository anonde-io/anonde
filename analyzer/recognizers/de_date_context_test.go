package recognizers

import (
	"context"
	"testing"
)

func TestDEDateContextRecognizer(t *testing.T) {
	r := NewDEDateContextRecognizer()

	cases := []struct {
		name       string
		text       string
		wantSpans  []string // exact substrings expected to be matched, in any order
		mustNotHit []string // substrings that must NOT appear in any match
	}{
		// Real GraSCCo-style positive cases.
		{
			name:      "Z.n. surgery + bare year",
			text:      "Z.n. Apoplex 2002 (Hemiparese).",
			wantSpans: []string{"2002"},
		},
		{
			name:      "St.p. + bare year",
			text:      "St.p. SHT 1995, unauffällig.",
			wantSpans: []string{"1995"},
		},
		{
			name:      "seit + bare year",
			text:      "Symptomatisches Anfallsleiden seit 2007.",
			wantSpans: []string{"2007"},
		},
		{
			name:      "vom + partial date range",
			text:      "die sich vom 19.3. bis zum 7.6. vorstellte",
			wantSpans: []string{"19.3.", "7.6."},
		},
		{
			name:      "UICC staging",
			text:      "Tumorklassifikation: UICC 2009: pT-3c, G2.",
			wantSpans: []string{"2009"},
		},
		{
			name:      "Erstdiagnose abbreviation",
			text:      "Psychose (ED 2018) seither stabil.",
			wantSpans: []string{"2018"},
		},
		{
			name:      "Jahrgang context",
			text:      "Patient Jahrgang 1978, männlich.",
			wantSpans: []string{"1978"},
		},
		{
			name:      "multiple Z.n. years",
			text:      "Z.n. Hüft-TEP bds. 2057 und 2059.",
			wantSpans: []string{"2057", "2059"},
		},

		// Negative cases — must NOT match.
		{
			name:      "lab value resembling year",
			text:      "Ferritin 2007 ng/mL Normalbereich.",
			wantSpans: nil,
		},
		{
			name:      "version number without context",
			text:      "Befundbericht v 2024 ohne Befund.",
			wantSpans: nil,
		},
		{
			name:      "section number alone",
			text:      "Siehe Abschnitt 19.3. in der Anlage.",
			wantSpans: nil,
		},
		{
			name:      "DIN standard reference",
			text:      "Sterilisation nach DIN 2018 erfolgt.",
			wantSpans: nil,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res, err := r.Analyze(context.Background(), tc.text, nil, "de")
			if err != nil {
				t.Fatalf("Analyze: %v", err)
			}
			if len(tc.wantSpans) == 0 {
				if len(res) != 0 {
					t.Fatalf("expected no matches, got %d: %+v", len(res), res)
				}
				return
			}
			got := make([]string, 0, len(res))
			for _, x := range res {
				got = append(got, tc.text[x.Start:x.End])
			}
			if len(got) != len(tc.wantSpans) {
				t.Fatalf("got %d matches %v, want %d %v", len(got), got, len(tc.wantSpans), tc.wantSpans)
			}
			// Order-insensitive check.
			expected := map[string]int{}
			for _, w := range tc.wantSpans {
				expected[w]++
			}
			for _, g := range got {
				if expected[g] == 0 {
					t.Fatalf("unexpected match %q (got=%v, want=%v)", g, got, tc.wantSpans)
				}
				expected[g]--
			}
		})
	}
}
