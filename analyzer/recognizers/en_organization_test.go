package recognizers

import (
	"context"
	"testing"
)

func TestENOrganizationRecognizer(t *testing.T) {
	r := NewENOrganizationRecognizer()

	cases := []struct {
		name      string
		text      string
		wantSpans []string
	}{
		// Suffix pattern
		{"Hospital plain", "Admitted to Mercy Hospital yesterday.", []string{"Mercy Hospital"}},
		{"General as suffix", "Seen at Mercy General on Tuesday.", []string{"Mercy General"}},
		{"Massachusetts General Hospital", "Transferred to Massachusetts General Hospital.", []string{"Massachusetts General Hospital"}},
		{"Medical Center", "Care at Stanford Medical Center.", []string{"Stanford Medical Center"}},
		{"St period prefix", "Visit St. Joseph's Hospital.", []string{"St. Joseph's Hospital"}},
		{"Hyphenated prefix", "Treated at Cedars-Sinai Medical Center.", []string{"Cedars-Sinai Medical Center"}},
		{"Memorial", "Houston Memorial admitted patient.", []string{"Houston Memorial"}},

		// Well-known list
		{"Mayo Clinic", "Referred to Mayo Clinic for evaluation.", []string{"Mayo Clinic"}},
		{"Cleveland Clinic", "Discharged from Cleveland Clinic.", []string{"Cleveland Clinic"}},
		{"Johns Hopkins", "Studied at Johns Hopkins Hospital.", []string{"Johns Hopkins Hospital"}},
		{"Kaiser Permanente", "Insured by Kaiser Permanente.", []string{"Kaiser Permanente"}},

		// Must NOT match
		{"No suffix nearby", "He works in New York.", nil},
		{"Plain country", "United States Department of Defense.", nil},
		{"Lowercase", "the hospital was busy.", nil},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res, err := r.Analyze(context.Background(), tc.text, nil, "en")
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
			for w, c := range expected {
				if c > 0 {
					t.Fatalf("missing match %q (got=%v, want=%v)", w, got, tc.wantSpans)
				}
			}
		})
	}
}
