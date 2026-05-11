package recognizers

import (
	"context"
	"testing"
)

func TestDEPostalCodeRecognizer(t *testing.T) {
	r := NewDEPostalCodeRecognizer()

	cases := []struct {
		name      string
		text      string
		wantSpans []string
	}{
		// Canonical PLZ + city.
		{"plz + single city", "Anschrift: 24937 Flensburg", []string{"24937 Flensburg"}},
		{"plz + two-word city", "12345 Bad Oeynhausen ist erreichbar", []string{"12345 Bad Oeynhausen"}},

		// Country-prefixed.
		{"austria prefix", "A-2236 ist Niederösterreich.", []string{"A-2236"}},
		{"switzerland prefix", "CH-8001 Zürich, Schweiz.", []string{"CH-8001"}}, // CH-8001 alone; city not picked because regex anchors on bare \d{5}.

		// Bare 5-digit with explicit context keyword.
		{"plz keyword before", "PLZ 80339 für die Adresse.", []string{"80339"}},

		// Negative — bare number with no context.
		{"bare number no context", "Befund 12345 in der Reihe.", nil},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res, err := r.Analyze(context.Background(), tc.text, nil, "de")
			if err != nil {
				t.Fatalf("Analyze: %v", err)
			}
			if len(tc.wantSpans) == 0 {
				if len(res) > 0 {
					t.Fatalf("expected no match, got %d: %+v", len(res), res)
				}
				return
			}
			got := make([]string, 0, len(res))
			for _, x := range res {
				got = append(got, tc.text[x.Start:x.End])
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
