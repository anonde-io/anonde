package analyzer

import (
	"context"
	"testing"
)

type stubRecognizer struct {
	name       string
	entities   []string
	languages  []string
	results    []RecognizerResult
	callCount  int
}

func (s *stubRecognizer) Name() string                 { return s.name }
func (s *stubRecognizer) SupportedEntities() []string  { return s.entities }
func (s *stubRecognizer) SupportedLanguages() []string { return s.languages }
func (s *stubRecognizer) Analyze(_ context.Context, _ string, _ []string, _ string) ([]RecognizerResult, error) {
	s.callCount++
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
