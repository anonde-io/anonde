package analyzer

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type stubRecognizer struct {
	name      string
	entities  []string
	languages []string
	results   []RecognizerResult
	err       error // when set, Analyze returns this error
	callCount int
}

func (s *stubRecognizer) Name() string                 { return s.name }
func (s *stubRecognizer) SupportedEntities() []string  { return s.entities }
func (s *stubRecognizer) SupportedLanguages() []string { return s.languages }
func (s *stubRecognizer) Analyze(_ context.Context, _ string, _ []string, _ string) ([]RecognizerResult, error) {
	s.callCount++
	if s.err != nil {
		return nil, s.err
	}
	return s.results, nil
}

func TestAnalyze_DisableNERSkipsAllNERRecognizers(t *testing.T) {
	t.Parallel()

	reg := NewRecognizerRegistry()
	localNER := &stubRecognizer{
		name:      "NERRecognizer",
		entities:  []string{"PERSON"},
		languages: []string{"en"},
	}
	remoteNER := &stubRecognizer{
		name:      "PresidioRemoteNERRecognizer",
		entities:  []string{"PERSON"},
		languages: []string{"en"},
	}
	reg.Add(localNER, remoteNER)

	engine := NewAnalyzerEngine(reg)
	_, err := engine.Analyze(context.Background(), "John Doe", AnalysisConfig{
		Language:   "en",
		DisableNER: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	if localNER.callCount != 0 || remoteNER.callCount != 0 {
		t.Fatalf("expected all NER recognizers to be skipped, got local=%d remote=%d", localNER.callCount, remoteNER.callCount)
	}
}

// TestAnalyze_NERFailureFailsClosed pins the silent-fallback fix: when a
// model-backed (NER) recognizer errors, Analyze must fail the whole request
// rather than quietly return the pattern recognizers' findings — patterns
// cover none of PERSON/ORG/LOC, so degrading to patterns-only would leak
// exactly what NER catches. A pattern recognizer error alongside it stays
// tolerated.
func TestAnalyze_NERFailureFailsClosed(t *testing.T) {
	t.Parallel()

	nerErr := errors.New("onnx inference failed")
	failingNER := &stubRecognizer{
		name:      "GLiNERPool", // model-backed (in nerRecognizerNames)
		entities:  []string{"PERSON"},
		languages: []string{"en"},
		err:       nerErr,
	}
	okPattern := &stubRecognizer{
		name:      "EmailRecognizer",
		entities:  []string{"EMAIL_ADDRESS"},
		languages: []string{"en"},
		results:   []RecognizerResult{{Start: 0, End: 5, Score: 0.9, EntityType: "EMAIL_ADDRESS", RecognizerName: "EmailRecognizer"}},
	}

	reg := NewRecognizerRegistry()
	reg.Add(failingNER)
	reg.Add(okPattern)

	res, err := NewAnalyzerEngine(reg).Analyze(context.Background(), "text here", AnalysisConfig{Language: "en"})
	if err == nil {
		t.Fatalf("expected fail-closed error on NER failure; got nil (res=%d findings — SILENT LEAK)", len(res))
	}
	if !errors.Is(err, nerErr) {
		t.Fatalf("error must wrap the underlying NER failure; got %v", err)
	}
	if res != nil {
		t.Fatalf("fail-closed must return no results; got %d", len(res))
	}

	// Control: a PATTERN recognizer error alongside a working one is tolerated
	// (findings still returned, no error) — only NER failures fail closed.
	failingPattern := &stubRecognizer{
		name: "BrokenPatternRecognizer", entities: []string{"PHONE"}, languages: []string{"en"},
		err: errors.New("regex blew up"),
	}
	reg2 := NewRecognizerRegistry()
	reg2.Add(failingPattern)
	reg2.Add(okPattern)
	got, err := NewAnalyzerEngine(reg2).Analyze(context.Background(), "text here", AnalysisConfig{Language: "en"})
	if err != nil {
		t.Fatalf("a single pattern-recognizer error must be tolerated, not fail the request; got %v", err)
	}
	if len(got) == 0 {
		t.Fatalf("expected the surviving pattern recognizer's findings; got none")
	}
}

// TestAnalyze_DisableNERSkipsPoolRecognizer pins the fix for the pooled-GLiNER
// leak: a recognizer whose Name() is a pool name in nerRecognizerNames
// ("GLiNERPool") but does NOT end in "NERRecognizer" must still be filtered
// out under DisableNER. The narrow isNERBasedRecognizer suffix check missed it,
// so disable_ner still ran expensive pooled NER inference. The fix routes the
// filter through isModelBackedRecognizer (the superset that consults
// nerRecognizerNames). Without DisableNER the pool must still run.
func TestAnalyze_DisableNERSkipsPoolRecognizer(t *testing.T) {
	t.Parallel()

	// Sanity: the fixture name must be one the conflict resolver already
	// treats as model-backed, yet must NOT satisfy the narrow suffix check —
	// that gap is exactly what this test guards.
	const poolName = "GLiNERPool"
	if strings.HasSuffix(poolName, "NERRecognizer") {
		t.Fatalf("fixture %q must NOT end in NERRecognizer or the test proves nothing", poolName)
	}
	if !nerRecognizerNames[poolName] {
		t.Fatalf("fixture %q must be in nerRecognizerNames for the superset predicate to catch it", poolName)
	}

	newPool := func() *stubRecognizer {
		return &stubRecognizer{
			name:      poolName,
			entities:  []string{"PERSON"},
			languages: []string{"en"},
			results: []RecognizerResult{
				{Start: 0, End: 8, Score: 0.9, EntityType: "PERSON", RecognizerName: poolName},
			},
		}
	}

	// DisableNER: the pool must be skipped (not dispatched, no findings).
	off := newPool()
	regOff := NewRecognizerRegistry()
	regOff.Add(off)
	res, err := NewAnalyzerEngine(regOff).Analyze(context.Background(), "John Doe", AnalysisConfig{
		Language:   "en",
		DisableNER: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if off.callCount != 0 {
		t.Fatalf("DisableNER: pool recognizer %q was dispatched (callCount=%d); expected 0", poolName, off.callCount)
	}
	if len(res) != 0 {
		t.Fatalf("DisableNER: expected no findings from skipped pool, got %d", len(res))
	}

	// Control: without DisableNER the pool must run.
	on := newPool()
	regOn := NewRecognizerRegistry()
	regOn.Add(on)
	if _, err := NewAnalyzerEngine(regOn).Analyze(context.Background(), "John Doe", AnalysisConfig{
		Language: "en",
	}); err != nil {
		t.Fatal(err)
	}
	if on.callCount == 0 {
		t.Fatalf("without DisableNER: pool recognizer %q was not dispatched; expected it to run", poolName)
	}
}
