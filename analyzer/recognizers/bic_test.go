package recognizers

import (
	"context"
	"testing"
)

func TestBICRecognizer(t *testing.T) {
	r := NewBICRecognizer()

	cases := []struct {
		name      string
		text      string
		wantSpans []string
	}{
		// Real BICs from finance_de bench corpus.
		{"Deutsche Bank Frankfurt", "BIC: DEUTDEMMXXX bestätigt", []string{"DEUTDEMMXXX"}},
		{"BayernLB", "Überweisung an BYLADEM1001 weitergeleitet", []string{"BYLADEM1001"}},
		{"Postbank Frankfurt", "Empfänger PBNKDEFFXXX OK", []string{"PBNKDEFFXXX"}},
		{"ING-DiBa", "BIC INGDDEFFXXX", []string{"INGDDEFFXXX"}},
		// 8-char primary office form (no branch suffix).
		{"8-char primary office", "BIC: COBADEFF\nIBAN:", []string{"COBADEFF"}},
		// Foreign BICs from common-country list.
		{"Swiss UBS", "UBSWCHZH80A", []string{"UBSWCHZH80A"}},
		{"US Citibank", "CITIUS33", []string{"CITIUS33"}},

		// Must NOT match; random ALL-CAPS strings, abbreviations.
		{"plain word", "BUNDESREPUBLIK", nil},
		{"short acronym", "GmbH", nil},
		{"all-caps non-BIC", "OVERVIEWDATA1", nil}, // wrong country slot
		{"clinical abbreviation", "EKG-MRI-CT-XRAY", nil},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res, err := r.Analyze(context.Background(), tc.text, nil, "")
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
			for _, want := range tc.wantSpans {
				found := false
				for _, g := range got {
					if g == want {
						found = true
						break
					}
				}
				if !found {
					t.Fatalf("missing %q in %v", want, got)
				}
			}
		})
	}
}
