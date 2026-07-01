package anonde

import (
	"context"
	"testing"

	"github.com/anonde-io/anonde/analyzer"
)

// TestUSHealthcareIDs_DefaultEngine drives the FULL default pattern engine
// (DefaultAnalyzerEngine, no NER) to confirm the Tier-1 US healthcare
// recognizers are wired in AND that conflict resolution keeps the validated,
// high-precision type when another pattern overlaps the same span.
//
// All IDs are synthetic test vectors, not real numbers.
func TestUSHealthcareIDs_DefaultEngine(t *testing.T) {
	eng := DefaultAnalyzerEngine()
	cfg := analyzer.AnalysisConfig{Language: "en", RemoveConflicts: true}

	// helper: is there a surviving finding of entityType covering want?
	has := func(text, entityType, want string, out []analyzer.RecognizerResult) bool {
		for _, r := range out {
			if r.EntityType == entityType && text[r.Start:r.End] == want {
				return true
			}
		}
		return false
	}

	t.Run("NPI detected in context, rejected without", func(t *testing.T) {
		// Luhn-valid NPI WITH a context keyword nearby → detected.
		text := "NPI 1234567893 on file"
		out, err := eng.Analyze(context.Background(), text, cfg)
		if err != nil {
			t.Fatalf("analyze: %v", err)
		}
		if !has(text, "US_NPI", "1234567893", out) {
			t.Fatalf("expected surviving US_NPI covering the NPI, got %+v", out)
		}

		// Precision guard: the SAME Luhn-valid NPI with NO context keyword must
		// NOT surface as US_NPI (RequireContext hard-gates the loose \d{10}).
		text = "order ref 1234567893 shipped"
		out, _ = eng.Analyze(context.Background(), text, cfg)
		for _, r := range out {
			if r.EntityType == "US_NPI" {
				t.Fatalf("context-free Luhn-valid 10-digit must not be US_NPI, got %+v", out)
			}
		}

		// A non-Luhn 10-digit number must not surface as US_NPI even with context.
		text = "provider NPI 1234567890 listed"
		out, _ = eng.Analyze(context.Background(), text, cfg)
		for _, r := range out {
			if r.EntityType == "US_NPI" {
				t.Fatalf("non-Luhn 10-digit must not be US_NPI, got %+v", out)
			}
		}
	})

	// Conflict note (task): "AB1234563" matches BOTH the DEA surface
	// ([A-Z][A-Z9]\d{7}, validates → 1.0) and MedicalLicense
	// ([A-Z]{2}-?\d{5,10}, score 0.5). The validated DEA outscores it, so the
	// span survives as US_DEA — a redacted, correctly-typed span (sane; and
	// leak-free since either label would redact the whole span).
	t.Run("DEA outresolves MedicalLicense overlap", func(t *testing.T) {
		text := "rx AB1234563 dispensed"
		out, err := eng.Analyze(context.Background(), text, cfg)
		if err != nil {
			t.Fatalf("analyze: %v", err)
		}
		if !has(text, "US_DEA", "AB1234563", out) {
			t.Fatalf("expected surviving US_DEA covering the DEA number, got %+v", out)
		}
		for _, r := range out {
			if r.EntityType == "MEDICAL_LICENSE" && text[r.Start:r.End] == "AB1234563" {
				t.Fatalf("MEDICAL_LICENSE must lose the overlap to the validated US_DEA, got %+v", out)
			}
		}
	})

	t.Run("MBI display form detected", func(t *testing.T) {
		text := "Medicare 1EG4-TE5-MK73 on file"
		out, err := eng.Analyze(context.Background(), text, cfg)
		if err != nil {
			t.Fatalf("analyze: %v", err)
		}
		if !has(text, "US_MBI", "1EG4-TE5-MK73", out) {
			t.Fatalf("expected surviving US_MBI covering the hyphenated MBI, got %+v", out)
		}
	})
}
