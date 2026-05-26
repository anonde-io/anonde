package recognizers

import (
	"context"
	"testing"
)

func TestDEClinicalIDRecognizer(t *testing.T) {
	r := NewDEClinicalIDRecognizer()

	cases := []struct {
		name      string
		text      string
		wantSpans []string // exact substrings expected, order-insensitive
	}{
		// Keyword-anchored long IDs.
		{"Fallnummer pure numeric", "Fallnummer: 23346011, geboren", []string{"23346011"}},
		{"Fall-Nr.", "Fall-Nr. 6733340001 *24.12.1972", []string{"6733340001"}},
		{"FN abbreviation", "Euripedes Erler (FN:445544767), geb", []string{"445544767"}},
		{"E-Nr.", "E-Nr.: 17217277: NE Oberbauch", []string{"17217277"}},
		{"SV Nr.", "SV Nr.: 4445311299", []string{"4445311299"}},
		{"Fall: long", "Fall: 102341651622", []string{"102341651622"}},
		{"Fallzahl", "Fallzahl: 103354008", []string{"103354008"}},
		{"Patient-ID hyphenated", "Fallnummer: A-202344102", []string{"A-202344102"}},

		// Alpha-prefix IDs after the new ID / Pat.-ID / ID-Nr. anchors.
		{"ID colon alpha prefix", "ID: KL699820", []string{"KL699820"}},
		{"ID-Nr alpha prefix", "ID-Nr.: HN999999", []string{"HN999999"}},
		{"Pat.-ID hyphenated", "Pat.-ID: PAT-202344102", []string{"PAT-202344102"}},
		{"Patient-ID alpha", "Patient-ID MR123456 vorgestellt", []string{"MR123456"}},

		// Station / ward identifiers.
		{"Station letter+digits", "auf Station A23 befand", []string{"A23"}},
		{"Station hyphenated", "Station O-11 vorstellen", []string{"O-11"}},
		{"OP roman", "im OP II am 31.10.2021", []string{"II"}},
		{"Intensivstation", "unserer Intensivstation I03 auf", []string{"I03"}},
		{"Ambulanz CH", "chirurgischen Ambulanz CH12", []string{"CH12"}},
		{"Station digits-letter", "auf Station 4A. Ein Termin", []string{"4A"}},

		// Histology codes.
		{"Histology slash", "Histologie (H25440/51): Kein", []string{"H25440/51"}},

		// Banking / finance anchors.
		{"Kundennummer dashed", "Kundennummer: KD-6556039\nIBAN:", []string{"KD-6556039"}},
		{"Kunden-Nr", "Kunden-Nr.: 9988776655", []string{"9988776655"}},
		{"Kontonummer", "Kontonummer: 100200300", []string{"100200300"}},
		{"Kontoauszug Nr slash date", "Kontoauszug Nr. K46473874/26.12.2022", []string{"K46473874"}},
		{"Customer EN", "Customer Number: ACC-552211", []string{"ACC-552211"}},
		{"Rechnungs-Nr", "Rechnungs-Nr.: R-4477", []string{"R-4477"}},

		// German court-case numbers (Aktenzeichen / Az / Geschäftszeichen).
		{"Az Roman case num", "Az.: 10 Ls 296/22\nKlage", []string{"10 Ls 296/22"}},
		{"Aktenzeichen", "Aktenzeichen: 25 VIII 24/22 vom", []string{"25 VIII 24/22"}},
		{"Az with citation", "Beweis: Vorlage der Rechnung Nr. 4 VII 532/21 vom", []string{"4 VII 532/21"}},

		// Must NOT match; short standalone numbers, lab values, dates.
		{"date should not match", "vom 12.05.2023 bis 24.06.2023", nil},
		{"lab value", "Leukozyten 8700", nil},
		{"dosage", "Insulin 12 IE", nil},
		{"random number mid-sentence", "Die Studie umfasste 23 Patienten.", nil},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res, err := r.Analyze(context.Background(), tc.text, nil, "de")
			if err != nil {
				t.Fatalf("Analyze: %v", err)
			}
			if len(tc.wantSpans) == 0 {
				if len(res) > 0 {
					got := make([]string, 0, len(res))
					for _, x := range res {
						got = append(got, tc.text[x.Start:x.End])
					}
					t.Fatalf("expected no match, got %d: %v", len(res), got)
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
