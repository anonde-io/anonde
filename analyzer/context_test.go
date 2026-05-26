package analyzer

import (
	"context"
	"testing"
)

type fakeRec struct {
	name     string
	entities []string
	results  []RecognizerResult
}

func (f *fakeRec) Name() string                 { return f.name }
func (f *fakeRec) SupportedEntities() []string  { return f.entities }
func (f *fakeRec) SupportedLanguages() []string { return []string{"*"} }
func (f *fakeRec) Analyze(_ context.Context, _ string, _ []string, _ string) ([]RecognizerResult, error) {
	return f.results, nil
}

type fakeCtxRec struct {
	*fakeRec
	keywords map[string][]string
}

func (f *fakeCtxRec) ContextKeywords() map[string][]string { return f.keywords }

func TestEnhanceWithContext_BoostsScore(t *testing.T) {
	t.Parallel()
	text := "my ssn is 123-45-6789 thanks"
	results := []RecognizerResult{
		{Start: 10, End: 21, Score: 0.5, EntityType: "US_SSN", RecognizerName: "x"},
	}
	out := EnhanceWithContext(text, results, map[string][]string{"US_SSN": {"ssn"}}, DefaultContextEnhancement())
	if out[0].Score <= 0.5 {
		t.Fatalf("expected score boost, got %.2f", out[0].Score)
	}
	if out[0].Score > 1.0 {
		t.Fatalf("score must cap at 1.0, got %.2f", out[0].Score)
	}
}

func TestEnhanceWithContext_NoMatch(t *testing.T) {
	t.Parallel()
	text := "some unrelated text 123-45-6789 elsewhere"
	results := []RecognizerResult{
		{Start: 20, End: 31, Score: 0.5, EntityType: "US_SSN"},
	}
	out := EnhanceWithContext(text, results, map[string][]string{"US_SSN": {"ssn"}}, DefaultContextEnhancement())
	if out[0].Score != 0.5 {
		t.Fatalf("expected score unchanged, got %.2f", out[0].Score)
	}
}

func TestEnhanceWithContext_WordBoundary(t *testing.T) {
	t.Parallel()
	// "lessness" must NOT match the keyword "less" on its own; word boundary required.
	text := "uselessness 123-45-6789"
	results := []RecognizerResult{
		{Start: 12, End: 23, Score: 0.5, EntityType: "X"},
	}
	out := EnhanceWithContext(text, results, map[string][]string{"X": {"less"}}, DefaultContextEnhancement())
	if out[0].Score != 0.5 {
		t.Fatalf("substring inside word must not boost score, got %.2f", out[0].Score)
	}
}

func TestCollectContextKeywords(t *testing.T) {
	t.Parallel()
	a := &fakeCtxRec{
		fakeRec:  &fakeRec{name: "A", entities: []string{"X"}},
		keywords: map[string][]string{"X": {"foo", "bar"}},
	}
	b := &fakeCtxRec{
		fakeRec:  &fakeRec{name: "B", entities: []string{"X"}},
		keywords: map[string][]string{"X": {"baz"}},
	}
	out := CollectContextKeywords([]EntityRecognizer{a, b})
	if len(out["X"]) != 3 {
		t.Fatalf("expected merged keywords, got %v", out["X"])
	}
}

func TestAnalyze_ContextBoostLetsWeakHitsClearThreshold(t *testing.T) {
	t.Parallel()
	registry := NewRecognizerRegistry()
	registry.Add(&fakeCtxRec{
		fakeRec: &fakeRec{
			name:     "WeakSSN",
			entities: []string{"US_SSN"},
			// "weak" finding at 0.3, threshold 0.5; without context boost this drops.
			results: []RecognizerResult{{Start: 10, End: 21, Score: 0.3, EntityType: "US_SSN"}},
		},
		keywords: map[string][]string{"US_SSN": {"ssn"}},
	})
	engine := NewAnalyzerEngine(registry)
	out, err := engine.Analyze(context.Background(), "user ssn: 123-45-6789", AnalysisConfig{
		ScoreThreshold: 0.5,
	})
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("expected 1 finding after context boost, got %d", len(out))
	}
}

func TestAnalyze_AllowListDropsFinding(t *testing.T) {
	t.Parallel()
	registry := NewRecognizerRegistry()
	registry.Add(&fakeRec{
		name:     "Email",
		entities: []string{"EMAIL_ADDRESS"},
		results:  []RecognizerResult{{Start: 0, End: 17, Score: 1.0, EntityType: "EMAIL_ADDRESS"}},
	})
	engine := NewAnalyzerEngine(registry)
	out, err := engine.Analyze(context.Background(), "fixture@test.com is the value here", AnalysisConfig{
		AllowList: []string{"fixture@test.com"},
	})
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if len(out) != 0 {
		t.Fatalf("allowlisted text must be filtered, got %d findings", len(out))
	}
}

func TestAnalyze_DenyListAddsForcedFinding(t *testing.T) {
	t.Parallel()
	registry := NewRecognizerRegistry()
	engine := NewAnalyzerEngine(registry)
	out, err := engine.Analyze(context.Background(), "internal codename: STARFLEET goes here", AnalysisConfig{
		DenyList: []string{"STARFLEET"},
	})
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if len(out) != 1 || out[0].EntityType != "DENY_LIST" {
		t.Fatalf("expected one DENY_LIST finding, got %+v", out)
	}
}
