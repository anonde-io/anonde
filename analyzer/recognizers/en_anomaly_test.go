package recognizers

import (
	"context"
	"testing"

	"github.com/anonde-io/anonde/analyzer"
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
		// "Discharge" is a sentence-start FP the bare path accepts. See
		// "Sentence-start FPs are accepted" below.
		{"Ms.", "Discharge to Ms. Linda Lopez.", []string{"Linda Lopez", "Discharge"}},

		// Medical
		{"Dr.", "Reviewed by Dr. Sarah Williams.", []string{"Sarah Williams", "Reviewed"}},
		{"Prof.", "Prof. Alan Turing consulted.", []string{"Alan Turing"}},

		// Clinical labels — MRN is filtered by isLeadTokenGated's short-all-caps rule.
		{"Patient: colon", "Patient: Omar Hassan, MRN 982341", []string{"Omar Hassan"}},
		{"Patient no colon", "Patient Omar Hassan presented today.", []string{"Omar Hassan"}},
		{"Pt.", "Pt. Robert James Brown reports pain.", []string{"Robert James Brown"}},

		// Name shapes
		{"Apostrophe", "Mr. Sean O'Connor admitted.", []string{"Sean O'Connor"}},
		{"Hyphenated surname", "Mrs. Eliza Thompson-Brown discharged.", []string{"Eliza Thompson-Brown"}},

		// Bare-name path (no honorific)
		{"Bare two tokens", "John Smith walked in.", []string{"John Smith"}},
		{"Bare single token, sentence-start", "Smith called the clinic.", []string{"Smith"}},
		{"Bare three tokens", "Mary Jane Watson was admitted.", []string{"Mary Jane Watson"}},
		{"Bare hyphenated", "Eliza Thompson-Brown left.", []string{"Eliza Thompson-Brown"}},
		{"Closed-class prefix gated", "He met John yesterday.", []string{"John"}},
		{"Pronoun-only stays out", "He left.", nil},
		{"Article-only stays out", "The cat ran.", nil},
		{"Compound closed-class", "The Smith family arrived.", []string{"Smith"}},
		// The bare path can't distinguish a sentence-start past-participle
		// from a name without an English clinical vocabulary. The
		// AnalysisConfig.AllowList is the documented mitigation.
		{"Sentence-start FPs are accepted", "Discussed with Dr.", []string{"Discussed"}},
		// Short all-caps acronyms (MRN, DOB, ICU, …) are never names.
		{"Short all-caps acronym filtered", "MRN 982341 was issued.", nil},

		// Must NOT match
		{"Lowercase title", "the doctor saw him.", nil},
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

// TestENAnomalyScores asserts the score difference between the structural
// (title-anchored) and bare paths — important because the analyzer's context-
// keyword boost relies on the bare path landing below the default 0.30 score
// threshold so that non-clinical capitalised sequences are dropped, and only
// context-boosted (clinical) names rise above threshold.
func TestENAnomalyScores(t *testing.T) {
	r := NewENAnomalyRecognizer()

	cases := []struct {
		text     string
		span     string
		wantMin  float64
		wantMax  float64
		wantType string
	}{
		{"Dr. Sarah Williams reviewed it.", "Sarah Williams", 0.84, 0.86, "PERSON"},
		{"John Smith was admitted.", "John Smith", 0.24, 0.26, "PERSON"},
	}
	for _, tc := range cases {
		res, err := r.Analyze(context.Background(), tc.text, nil, "en")
		if err != nil {
			t.Fatalf("Analyze: %v", err)
		}
		var found bool
		for _, x := range res {
			if tc.text[x.Start:x.End] != tc.span {
				continue
			}
			found = true
			if x.EntityType != tc.wantType {
				t.Fatalf("%q: type = %s, want %s", tc.span, x.EntityType, tc.wantType)
			}
			if x.Score < tc.wantMin || x.Score > tc.wantMax {
				t.Fatalf("%q: score = %.3f, want in [%.2f,%.2f]", tc.span, x.Score, tc.wantMin, tc.wantMax)
			}
		}
		if !found {
			t.Fatalf("%q not produced for input %q", tc.span, tc.text)
		}
	}
}

// TestENAnomalyContextBoost verifies that the recognizer's ContextProvider
// keywords cause the engine's EnhanceWithContext step to boost a bare-name
// score in clinical context. This is the end-to-end contract for bare names:
// without context they sit at 0.25 (below default 0.30 threshold, dropped);
// with nearby clinical keywords they lift to ~0.60 and survive the threshold
// filter alongside structural-path matches.
func TestENAnomalyContextBoost(t *testing.T) {
	registry := analyzer.NewRecognizerRegistry()
	registry.Add(NewENAnomalyRecognizer())
	engine := analyzer.NewAnalyzerEngine(registry)

	text := "John Smith was admitted to the clinic."
	res, err := engine.Analyze(context.Background(), text, analyzer.AnalysisConfig{Language: "en"})
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	var got *analyzer.RecognizerResult
	for i, x := range res {
		if text[x.Start:x.End] == "John Smith" {
			got = &res[i]
			break
		}
	}
	if got == nil {
		t.Fatalf("expected PERSON for %q, got %v", "John Smith", res)
	}
	// Base 0.25 + default boost 0.35 = 0.60
	if got.Score < 0.55 || got.Score > 0.65 {
		t.Fatalf("expected context-boosted score ~0.60, got %.3f", got.Score)
	}
}
