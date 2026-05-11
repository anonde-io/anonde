package recognizers

import (
	"context"
	"testing"
)

func TestDEStreetRecognizer(t *testing.T) {
	r := NewDEStreetRecognizer()

	cases := []struct {
		name        string
		text        string
		mustContain []string // each substring must appear in at least one matched span; nil = no match expected
	}{
		// Real GraSCCo shapes.
		{"adj + Str. + number", "Anschrift: Friesische Str. 21 a, 24937 Flensburg", []string{"Friesische Str. 21"}},
		{"compound Hauptstr", "wohnhaft Hauptstr. 8", []string{"Hauptstr. 8"}},
		{"compound Florgasse", "Florgasse 2 ist die Adresse", []string{"Florgasse 2"}},
		{"compound Gartenpfad", "Adresse: Gartenpfad 44", []string{"Gartenpfad 44"}},
		{"compound Sauerbruchplatz", "Sauerbruchplatz 8 erreichbar", []string{"Sauerbruchplatz 8"}},
		{"compound straße", "Kantstraße 21", []string{"Kantstraße 21"}},
		// Negative: prep form ("Am Bauch", "Im Bett") deliberately not
		// matched — too ambiguous in clinical text.
		{"prep form should not match", "Schmerzen Am Bauch und Im Bett.", nil},
		// Negative: common clinical compound endings that used to FP.
		{"Vorgang must not match", "Operativer Vorgang abgeschlossen.", nil},
		{"Anstieg must not match", "Anstieg der Leukozyten.", nil},
		{"Ohrring must not match", "Schmuck wie Ohrring entfernt.", nil},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res, err := r.Analyze(context.Background(), tc.text, nil, "de")
			if err != nil {
				t.Fatalf("Analyze: %v", err)
			}
			if tc.mustContain == nil {
				if len(res) > 0 {
					got := make([]string, 0, len(res))
					for _, x := range res {
						got = append(got, tc.text[x.Start:x.End])
					}
					t.Fatalf("expected no match for %q, got %v", tc.text, got)
				}
				return
			}
			if len(res) == 0 {
				t.Fatalf("expected matches, got none for text: %s", tc.text)
			}
			gotJoined := ""
			for _, x := range res {
				gotJoined += "|" + tc.text[x.Start:x.End]
			}
			for _, want := range tc.mustContain {
				found := false
				for _, x := range res {
					if tc.text[x.Start:x.End] == want {
						found = true
						break
					}
				}
				if !found {
					// Try substring containment as a fallback (recognizer may
					// match a longer span that still contains the expected text).
					if !contains(gotJoined, want) {
						t.Errorf("expected match containing %q, got: %s", want, gotJoined)
					}
				}
			}
		})
	}
}

func contains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
