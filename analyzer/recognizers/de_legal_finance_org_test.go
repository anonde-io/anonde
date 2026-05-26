package recognizers

import (
	"context"
	"testing"
)

func TestDELegalFinanceOrgRecognizer(t *testing.T) {
	r := NewDELegalFinanceOrgRecognizer()

	cases := []struct {
		name      string
		text      string
		wantSpans []string // substrings expected, order-insensitive
	}{
		// Courts; the legal_de corpus's #1 ORG-leak family.
		{"Amtsgericht with city", "An das Amtsgericht Bremen gerichtet.", []string{"Amtsgericht Bremen"}},
		{"Arbeitsgericht with city", "An das Arbeitsgericht Krefeld", []string{"Arbeitsgericht Krefeld"}},
		{"BGH abbreviation", "Das BGH-Urteil vom 12.05.", []string{"BGH"}},
		{"Landgericht with hyphenated city", "Landgericht Frankfurt-Höchst entschied.", []string{"Landgericht Frankfurt-Höchst"}},

		// Banks; finance_de's headline org family.
		{"KfW Bankengruppe", "KfW Bankengruppe — Online-Banking", []string{"KfW Bankengruppe"}},
		{"HypoVereinsbank", "HypoVereinsbank\nFiliale Wiesbaden", []string{"HypoVereinsbank"}},
		{"Sparkasse with city", "Bei der Sparkasse Köln-Bonn eingerichtet.", []string{"Sparkasse Köln-Bonn"}},
		{"Volksbank multi-place + eG", "Volksbank Bonn Rhein-Sieg eG bestätigt", []string{"Volksbank Bonn Rhein-Sieg eG"}},

		// Government agencies.
		{"Finanzamt with city", "Steuererstattung Finanzamt München angekündigt", []string{"Finanzamt München"}},
		{"Bundesamt with subject", "Bundesamt für Migration und Flüchtlinge entscheidet.", []string{"Bundesamt für Migration und Flüchtlinge"}},

		// Law firms.
		{"Kanzlei multi name", "Kanzlei Fischer & Lorenz & Kollegen vertritt", []string{"Kanzlei Fischer & Lorenz & Kollegen"}},

		// Companies with German legal-form suffix.
		{"Firma GmbH & Co KG", "der Firma Elbschloss Möbel GmbH & Co. KG", []string{"Firma Elbschloss Möbel GmbH & Co. KG"}},
		{"AG plain", "Volkswagen AG meldet", []string{"Volkswagen AG"}},

		// Must NOT match; non-institutional surfaces.
		{"plain city", "in München", nil},
		{"plain person", "Beate Roth wohnt hier.", nil},
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
					t.Fatalf("expected no match, got: %v", got)
				}
				return
			}
			// every expected substring must appear in `got` at least once.
			for _, want := range tc.wantSpans {
				found := false
				for _, g := range got {
					if g == want {
						found = true
						break
					}
				}
				if !found {
					t.Fatalf("missing expected span %q in matches %v", want, got)
				}
			}
		})
	}
}
