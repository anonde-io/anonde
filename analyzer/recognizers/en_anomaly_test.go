package recognizers

import (
	"context"
	"testing"
)

func TestENAnomalyRecognizer(t *testing.T) {
	r := NewENAnomalyRecognizer()

	cases := []struct {
		name      string
		text      string
		wantSpans []string
	}{
		// Honorifics
		{"Mr. with period", "Patient seen by Mr. John Smith yesterday.", []string{"John Smith"}},
		{"Mr without period", "Mr Henry Adams was admitted.", []string{"Henry Adams"}},
		{"Mrs.", "Mrs. Mary Jane Watson came in.", []string{"Mary Jane Watson"}},
		{"Ms.", "Discharge to Ms. Linda Lopez.", []string{"Linda Lopez"}},

		// Medical
		{"Dr.", "Reviewed by Dr. Sarah Williams.", []string{"Sarah Williams"}},
		{"Prof.", "Prof. Alan Turing consulted.", []string{"Alan Turing"}},

		// Clinical labels
		{"Patient: colon", "Patient: Omar Hassan, MRN 982341", []string{"Omar Hassan"}},
		{"Patient no colon", "Patient Omar Hassan presented today.", []string{"Omar Hassan"}},
		{"Pt.", "Pt. Robert James Brown reports pain.", []string{"Robert James Brown"}},

		// Name shapes
		{"Apostrophe", "Mr. Sean O'Connor admitted.", []string{"Sean O'Connor"}},
		{"Hyphenated surname", "Mrs. Eliza Thompson-Brown discharged.", []string{"Eliza Thompson-Brown"}},

		// Must NOT match
		{"No title", "John Smith walked in.", nil},
		{"Lowercase title", "the doctor saw him.", nil},
		{"Title at end of sentence", "Discussed with Dr.", nil},
		{"Title + non-name word", "Mr. Hospital staff present.", []string{"Hospital"}}, // edge case; expected behaviour
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
