package recognizers

import (
	"context"
	"testing"
)

// ---------------------------------------------------------------------------
// US healthcare identifiers (Tier 1): NPI, DEA, MBI.
//
// All IDs below are SYNTHETIC test vectors constructed to satisfy (or, for the
// negative cases, deliberately break) the documented checksum/format — they are
// not real provider/prescriber/beneficiary numbers. The negative cases are the
// load-bearing ones: they prove the recognizers are high-precision by
// construction and reject checksum/format near-misses.
// ---------------------------------------------------------------------------

// --- NPI (National Provider Identifier) -----------------------------------

// TestValidateUSNPI exercises the Luhn+80840 check digit directly.
func TestValidateUSNPI(t *testing.T) {
	t.Parallel()
	// Synthetic Luhn(80840 + first9)-valid NPIs (verified by hand):
	//   1234567893 → Luhn("808401234567893") sums to 70 (≡0 mod 10)
	//   1245319599 → Luhn("808401245319599") sums to 80 (≡0 mod 10)
	valid := []string{"1234567893", "1245319599"}
	for _, s := range valid {
		if !validateUSNPI(s) {
			t.Errorf("expected %q to be a valid NPI", s)
		}
	}
	invalid := []struct{ s, why string }{
		{"1234567890", "wrong Luhn check digit (0 vs 3)"},
		{"1234567892", "wrong Luhn check digit (2 vs 3)"},
		{"1245319590", "wrong Luhn check digit"},
		{"123456789", "too short (9 digits)"},
		{"12345678930", "too long (11 digits)"},
		{"123456789X", "non-digit"},
	}
	for _, tc := range invalid {
		if validateUSNPI(tc.s) {
			t.Errorf("expected %q to be INVALID NPI (%s)", tc.s, tc.why)
		}
	}
}

// TestUSNPIRecognizer verifies checksum-gated emission at the recognizer level:
// a Luhn-valid NPI lands at 1.0; a non-Luhn 10-digit number is NOT emitted.
func TestUSNPIRecognizer(t *testing.T) {
	t.Parallel()
	r := NewUSNPIRecognizer()

	// (a) known-valid → detected as US_NPI at 1.0.
	out, err := r.Analyze(context.Background(), "provider NPI 1234567893 on file", nil, "en")
	if err != nil {
		t.Fatalf("analyze: %v", err)
	}
	if len(out) != 1 || out[0].EntityType != "US_NPI" || out[0].Score != 1.0 {
		t.Fatalf("expected one US_NPI hit at 1.0, got %+v", out)
	}

	// (b) checksum near-miss → dropped (precision guard). A random 10-digit
	// number that fails Luhn must produce zero US_NPI findings.
	out, _ = r.Analyze(context.Background(), "order ref 1234567890 shipped", nil, "en")
	for _, h := range out {
		if h.EntityType == "US_NPI" {
			t.Fatalf("non-Luhn 10-digit number must not emit US_NPI, got %+v", out)
		}
	}

	// (c) context guard: the SAME Luhn-valid NPI with NO nearby context keyword
	// must be dropped (RequireContext), so we don't over-redact arbitrary
	// 10-digit IDs on general traffic.
	out, _ = r.Analyze(context.Background(), "order ref 1234567893 shipped", nil, "en")
	for _, h := range out {
		if h.EntityType == "US_NPI" {
			t.Fatalf("context-free Luhn-valid NPI must be dropped, got %+v", out)
		}
	}
}

// --- DEA (prescriber registration number) ---------------------------------

// TestValidateUSDEA exercises the (d1+d3+d5)+2*(d2+d4+d6) units-digit check.
func TestValidateUSDEA(t *testing.T) {
	t.Parallel()
	// digits 1234563: (1+3+5)+2*(2+4+6)=9+24=33 → units 3 == d7. Valid for any
	// allowed leading pair, incl. a '9' in position 2.
	valid := []string{"AB1234563", "A91234563", "FA1234563"}
	for _, s := range valid {
		if !validateUSDEA(s) {
			t.Errorf("expected %q to be a valid DEA number", s)
		}
	}
	invalid := []struct{ s, why string }{
		{"AB1234564", "wrong check digit (4 vs 3)"},
		{"AB1234560", "wrong check digit (0 vs 3)"},
		{"aB1234563", "lowercase registrant letter"},
		{"A11234563", "position 2 is a digit other than 9"},
		{"AB123456", "too short (6 digits)"},
		{"AB12345633", "too long (8 digits)"},
	}
	for _, tc := range invalid {
		if validateUSDEA(tc.s) {
			t.Errorf("expected %q to be INVALID DEA (%s)", tc.s, tc.why)
		}
	}
}

// TestUSDEARecognizer verifies regex + checksum-gated emission.
func TestUSDEARecognizer(t *testing.T) {
	t.Parallel()
	r := NewUSDEARecognizer()

	// (a) known-valid → US_DEA at 1.0.
	out, err := r.Analyze(context.Background(), "DEA# AB1234563 for the script", nil, "en")
	if err != nil {
		t.Fatalf("analyze: %v", err)
	}
	if len(out) != 1 || out[0].EntityType != "US_DEA" || out[0].Score != 1.0 {
		t.Fatalf("expected one US_DEA hit at 1.0, got %+v", out)
	}
	if got := "DEA# AB1234563 for the script"[out[0].Start:out[0].End]; got != "AB1234563" {
		t.Fatalf("matched %q, want %q", got, "AB1234563")
	}

	// (b) bad check digit → dropped.
	out, _ = r.Analyze(context.Background(), "code AB1234564 noted", nil, "en")
	if len(out) != 0 {
		t.Fatalf("bad-check-digit DEA must not emit, got %+v", out)
	}
}

// --- MBI (Medicare Beneficiary Identifier) --------------------------------

// TestValidateUSMBI exercises the strict positional format, incl. the
// excluded-letter rule.
func TestValidateUSMBI(t *testing.T) {
	t.Parallel()
	// CMS canonical example, hyphens stripped.
	valid := []string{"1EG4TE5MK73"}
	for _, s := range valid {
		if !validateUSMBI(s) {
			t.Errorf("expected %q to be a valid MBI", s)
		}
	}
	invalid := []struct{ s, why string }{
		{"1SG4TE5MK73", "excluded letter S in pos 2"},
		{"1EG4TE5SK73", "excluded letter S in pos 8"},
		{"0EG4TE5MK73", "pos 1 must be 1-9, not 0"},
		{"11G4TE5MK73", "pos 2 must be alphabetic, not a digit"},
		{"1EGXTE5MK73", "pos 4 must be numeric, not a letter"},
		{"1EG4TE5MK7", "too short (10 chars)"},
		{"1EG4TE5MK733", "too long (12 chars)"},
	}
	for _, tc := range invalid {
		if validateUSMBI(tc.s) {
			t.Errorf("expected %q to be INVALID MBI (%s)", tc.s, tc.why)
		}
	}
}

// TestUSMBIRecognizer verifies detection of both the plain and hyphenated
// display forms, and that an excluded letter is rejected (at the regex level).
func TestUSMBIRecognizer(t *testing.T) {
	t.Parallel()
	r := NewUSMBIRecognizer()

	cases := []struct {
		name      string
		text      string
		wantMatch string // "" = expect no US_MBI finding
	}{
		{"plain", "MBI 1EG4TE5MK73 active", "1EG4TE5MK73"},
		{"hyphenated 4-3-4", "Medicare 1EG4-TE5-MK73 card", "1EG4-TE5-MK73"},
		{"excluded letter S", "id 1SG4TE5MK73 here", ""},
		{"bare 11 digits (never MBI)", "num 12345678901 ref", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, err := r.Analyze(context.Background(), tc.text, nil, "en")
			if err != nil {
				t.Fatalf("analyze: %v", err)
			}
			var got string
			for _, h := range out {
				if h.EntityType == "US_MBI" {
					if h.Score != 1.0 {
						t.Fatalf("US_MBI score = %.2f, want 1.0", h.Score)
					}
					got = tc.text[h.Start:h.End]
				}
			}
			if got != tc.wantMatch {
				t.Fatalf("US_MBI match = %q, want %q (out=%+v)", got, tc.wantMatch, out)
			}
		})
	}
}

// --- Context keywords surfaced --------------------------------------------

func TestUSHealthcareIDs_ContextKeywords(t *testing.T) {
	t.Parallel()
	checks := []struct {
		rec    interface{ ContextKeywords() map[string][]string }
		entity string
		want   string
	}{
		{NewUSNPIRecognizer(), "US_NPI", "npi"},
		{NewUSDEARecognizer(), "US_DEA", "dea"},
		{NewUSMBIRecognizer(), "US_MBI", "mbi"},
	}
	for _, c := range checks {
		kws := c.rec.ContextKeywords()[c.entity]
		found := false
		for _, k := range kws {
			if k == c.want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("%s context keywords %v missing %q", c.entity, kws, c.want)
		}
	}
}
