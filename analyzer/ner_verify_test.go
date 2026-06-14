package analyzer

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// errFakeNERLoad simulates a GLiNER model-load / ONNX-session-open
// failure: the real recognizer's init() surfaces exactly this shape of
// error (model file missing, libonnxruntime unreachable, tokenizer
// missing) through Analyze. We can't load the real model in the sandbox,
// so we inject the failure here.
var errFakeNERLoad = errors.New("gliner: open onnx session: no such file")

// failingNERRecognizer is a model-backed NER recognizer whose Analyze
// always fails to load — the in-sandbox stand-in for a real GLiNER
// recognizer that can't open its ONNX session. Its Name ends in
// "NERRecognizer" so it's treated as NER by both the DisableNER suffix
// check and VerifyNERBackend's broader matcher.
type failingNERRecognizer struct {
	name      string
	callCount int
}

func (f *failingNERRecognizer) Name() string                 { return f.name }
func (f *failingNERRecognizer) SupportedEntities() []string  { return []string{"PERSON"} }
func (f *failingNERRecognizer) SupportedLanguages() []string { return []string{"*"} }
func (f *failingNERRecognizer) Analyze(_ context.Context, _ string, _ []string, _ string) ([]RecognizerResult, error) {
	f.callCount++
	return nil, errFakeNERLoad
}

// okPatternRecognizer is a stand-in for the ~51 pattern recognizers that
// always succeed. Its presence in the registry is what makes the silent
// fallback possible: engine.Analyze returns its findings with err == nil
// even when the NER recognizer alongside it failed to load.
type okPatternRecognizer struct{}

func (okPatternRecognizer) Name() string                 { return "FakePatternRecognizer" }
func (okPatternRecognizer) SupportedEntities() []string  { return []string{"EMAIL_ADDRESS"} }
func (okPatternRecognizer) SupportedLanguages() []string { return []string{"*"} }
func (okPatternRecognizer) Analyze(_ context.Context, _ string, _ []string, _ string) ([]RecognizerResult, error) {
	return []RecognizerResult{{Start: 0, End: 4, Score: 0.9, EntityType: "EMAIL_ADDRESS", RecognizerName: "FakePatternRecognizer"}}, nil
}

// TestAnalyze_SwallowsNERLoadFailure documents the bug VerifyNERBackend
// exists to catch: when a NER recognizer fails to load but a pattern
// recognizer succeeds alongside it, engine.Analyze returns the
// patterns-only findings with NO error. This is the silent fallback —
// asserting it here pins the behaviour the boot-time guard works around.
func TestAnalyze_SwallowsNERLoadFailure(t *testing.T) {
	reg := NewRecognizerRegistry()
	reg.Add(&failingNERRecognizer{name: "GLiNERRecognizer"})
	reg.Add(okPatternRecognizer{})
	engine := NewAnalyzerEngine(reg)

	results, err := engine.Analyze(context.Background(), "test text with PII", AnalysisConfig{Language: "en"})
	if err != nil {
		t.Fatalf("engine.Analyze returned an error (%v); the bug is that it does NOT — "+
			"the NER failure is swallowed and patterns-only results come back clean", err)
	}
	if len(results) == 0 {
		t.Fatal("expected patterns-only results to survive the NER failure")
	}
	// Confirm the surviving findings are patterns-only (NER produced nothing).
	for _, r := range results {
		if r.RecognizerName == "GLiNERRecognizer" {
			t.Fatalf("did not expect any finding from the failing NER recognizer; got %+v", r)
		}
	}
}

// TestVerifyNERBackend_FailsLoudOnLoadFailure is the load-bearing test:
// a forced NER-load failure must make VerifyNERBackend return an error
// (so the server / bench runner can fail closed), even though
// engine.Analyze for the same engine succeeds (see the test above).
func TestVerifyNERBackend_FailsLoudOnLoadFailure(t *testing.T) {
	reg := NewRecognizerRegistry()
	failing := &failingNERRecognizer{name: "GLiNERRecognizer"}
	reg.Add(failing)
	reg.Add(okPatternRecognizer{})
	engine := NewAnalyzerEngine(reg)

	err := VerifyNERBackend(context.Background(), engine)
	if err == nil {
		t.Fatal("VerifyNERBackend returned nil; expected a loud error when the NER recognizer fails to load")
	}
	if !errors.Is(err, errFakeNERLoad) {
		t.Errorf("error should wrap the underlying load failure; got %v", err)
	}
	if !strings.Contains(err.Error(), "GLiNERRecognizer") {
		t.Errorf("error should name the failing recognizer; got %v", err)
	}
	if failing.callCount == 0 {
		t.Error("VerifyNERBackend did not probe the NER recognizer at all")
	}
}

// TestVerifyNERBackend_PoolNameMatched ensures the verifier also catches
// pool wrappers (GLiNERPool / GLiNERFlatPool) whose Name() does NOT end
// in "NERRecognizer". A load failure inside a pool is just as silent.
func TestVerifyNERBackend_PoolNameMatched(t *testing.T) {
	reg := NewRecognizerRegistry()
	reg.Add(&failingNERRecognizer{name: "GLiNERPool"})
	reg.Add(okPatternRecognizer{})
	engine := NewAnalyzerEngine(reg)

	if err := VerifyNERBackend(context.Background(), engine); err == nil {
		t.Fatal("VerifyNERBackend did not catch a failing GLiNERPool (name does not end in NERRecognizer)")
	}
}

// TestVerifyNERBackend_PatternsOnlyNoError confirms the guard is a no-op
// on a patterns-only engine: no NER recognizer registered → nil error.
// This is what keeps the patterns-only deployment path and per-request
// disable_ner from being broken by the fail-closed change.
func TestVerifyNERBackend_PatternsOnlyNoError(t *testing.T) {
	reg := NewRecognizerRegistry()
	reg.Add(okPatternRecognizer{})
	engine := NewAnalyzerEngine(reg)

	if err := VerifyNERBackend(context.Background(), engine); err != nil {
		t.Fatalf("VerifyNERBackend errored on a patterns-only engine: %v", err)
	}
	if HasNERRecognizer(engine) {
		t.Error("HasNERRecognizer reported true for a patterns-only engine")
	}
}

// TestVerifyNERBackend_HealthyNERNoError confirms a NER recognizer that
// loads cleanly (Analyze returns no error) passes verification.
func TestVerifyNERBackend_HealthyNERNoError(t *testing.T) {
	reg := NewRecognizerRegistry()
	reg.Add(&stubRecognizer{
		name:      "GLiNERRecognizer",
		entities:  []string{"PERSON"},
		languages: []string{"*"},
		results:   []RecognizerResult{{Start: 0, End: 4, Score: 0.9, EntityType: "PERSON"}},
	})
	engine := NewAnalyzerEngine(reg)

	if !HasNERRecognizer(engine) {
		t.Fatal("HasNERRecognizer should report true with a NER recognizer registered")
	}
	if err := VerifyNERBackend(context.Background(), engine); err != nil {
		t.Fatalf("VerifyNERBackend errored on a healthy NER engine: %v", err)
	}
}
