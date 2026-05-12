package recognizers

import (
	"context"
	"testing"
)

func TestDEProfessionRecognizer(t *testing.T) {
	r := NewDEProfessionRecognizer()

	cases := []struct {
		name      string
		text      string
		wantSpans []string
	}{
		// Context-anchored
		{"Beruf colon", "Beruf: Lehrer", []string{"Lehrer"}},
		{"Beruf no colon", "Beruf Bäckerin und Mutter.", []string{"Bäckerin"}},
		{"tätig als", "Patient war bis zur Erkrankung tätig als Ingenieur.", []string{"Ingenieur"}},
		{"von Beruf", "Frau Müller ist von Beruf Hausfrau.", []string{"Hausfrau"}},
		{"arbeitete als", "Bis 2020 arbeitete als Verkäuferin.", []string{"Verkäuferin"}},

		// Vocabulary fallback
		{"Rentnerin standalone", "Die Rentnerin stellt sich vor.", []string{"Rentnerin"}},
		{"Lehrer standalone", "Hauptberuf: Lehrer. Verheiratet.", []string{"Lehrer"}},

		// Must NOT match — Arzt is a clinical role mention, not a patient
		// occupation.
		{"Arzt not matched", "Der behandelnde Arzt war Dr. Müller.", nil},
		{"Ärztin not matched", "Vorgestellt durch Ärztin Dr. Schmidt.", nil},
		// Common German word that isn't a profession
		{"unrelated", "Die Therapie war erfolgreich.", nil},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res, err := r.Analyze(context.Background(), tc.text, nil, "de")
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
