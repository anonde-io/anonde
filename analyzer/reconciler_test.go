package analyzer_test

import (
	"context"
	"errors"
	"testing"

	"github.com/moogacs/anonde/analyzer"
)

// fakeRecognizer returns a fixed set of results for every input. Used to
// exercise the engine's reconciler hook without depending on a real
// recognizer's output.
type fakeRecognizer struct {
	results []analyzer.RecognizerResult
}

func (f *fakeRecognizer) Name() string                  { return "FakeRecognizer" }
func (f *fakeRecognizer) SupportedEntities() []string   { return []string{"PERSON"} }
func (f *fakeRecognizer) SupportedLanguages() []string  { return []string{"*"} }
func (f *fakeRecognizer) Analyze(_ context.Context, _ string, _ []string, _ string) ([]analyzer.RecognizerResult, error) {
	return f.results, nil
}

// recordingReconciler captures the candidates passed to Reconcile so the
// test can assert that the engine called it in the right pipeline position.
type recordingReconciler struct {
	called   int
	received []analyzer.RecognizerResult
	keepAll  bool
	err      error
}

func (r *recordingReconciler) Reconcile(_ context.Context, _ string, c []analyzer.RecognizerResult) ([]analyzer.RecognizerResult, error) {
	r.called++
	r.received = append([]analyzer.RecognizerResult(nil), c...)
	if r.err != nil {
		return nil, r.err
	}
	if r.keepAll {
		return c, nil
	}
	// Default: drop everything to exercise the "reconciler can remove
	// candidates before the threshold filter runs" path.
	return nil, nil
}

// Engine MUST consult a non-nil Reconciler.
func TestEngine_ReconcilerCalled(t *testing.T) {
	reg := analyzer.NewRecognizerRegistry()
	reg.Add(&fakeRecognizer{results: []analyzer.RecognizerResult{
		{Start: 0, End: 3, Score: 0.9, EntityType: "PERSON", RecognizerName: "FakeRecognizer"},
	}})

	rec := &recordingReconciler{keepAll: true}
	e := analyzer.NewAnalyzerEngine(reg)
	e.Reconciler = rec

	got, err := e.Analyze(context.Background(), "Bob is here", analyzer.AnalysisConfig{
		Language: "en",
	})
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if rec.called != 1 {
		t.Errorf("expected reconciler called once, got %d", rec.called)
	}
	if len(got) != 1 {
		t.Errorf("keepAll reconciler should preserve candidates, got %d", len(got))
	}
}

// Engine MUST honor a Reconciler that drops candidates.
func TestEngine_ReconcilerCanDropCandidates(t *testing.T) {
	reg := analyzer.NewRecognizerRegistry()
	reg.Add(&fakeRecognizer{results: []analyzer.RecognizerResult{
		{Start: 0, End: 3, Score: 0.9, EntityType: "PERSON", RecognizerName: "FakeRecognizer"},
	}})

	rec := &recordingReconciler{keepAll: false} // drops everything
	e := analyzer.NewAnalyzerEngine(reg)
	e.Reconciler = rec

	got, err := e.Analyze(context.Background(), "Bob is here", analyzer.AnalysisConfig{
		Language: "en",
	})
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("reconciler dropped all but engine returned %d", len(got))
	}
}

// On Reconciler error, fail-open: keep candidates as-is.
func TestEngine_ReconcilerError_FailsOpen(t *testing.T) {
	reg := analyzer.NewRecognizerRegistry()
	reg.Add(&fakeRecognizer{results: []analyzer.RecognizerResult{
		{Start: 0, End: 3, Score: 0.9, EntityType: "PERSON", RecognizerName: "FakeRecognizer"},
	}})

	rec := &recordingReconciler{err: errors.New("llm exploded")}
	e := analyzer.NewAnalyzerEngine(reg)
	e.Reconciler = rec

	got, err := e.Analyze(context.Background(), "Bob is here", analyzer.AnalysisConfig{
		Language: "en",
	})
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if len(got) != 1 {
		t.Errorf("fail-open broken: expected 1 finding kept on reconciler error, got %d", len(got))
	}
}

// Nil reconciler is the default and must be a no-op.
func TestEngine_NilReconcilerIsNoop(t *testing.T) {
	reg := analyzer.NewRecognizerRegistry()
	reg.Add(&fakeRecognizer{results: []analyzer.RecognizerResult{
		{Start: 0, End: 3, Score: 0.9, EntityType: "PERSON", RecognizerName: "FakeRecognizer"},
	}})

	e := analyzer.NewAnalyzerEngine(reg) // no Reconciler set

	got, err := e.Analyze(context.Background(), "Bob is here", analyzer.AnalysisConfig{
		Language: "en",
	})
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if len(got) != 1 {
		t.Errorf("nil reconciler should not affect results, got %d", len(got))
	}
}
