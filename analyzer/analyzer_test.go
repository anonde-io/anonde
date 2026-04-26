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

func TestAnalyze_LowercaseTextKeepsRemoteNER(t *testing.T) {
	t.Parallel()

	reg := NewRecognizerRegistry()
	localNER := &stubRecognizer{
		name:      "NERRecognizer",
		entities:  []string{"PERSON", "LOCATION", "ORGANIZATION", "NRP"},
		languages: []string{"en"},
		results:   []RecognizerResult{{Start: 0, End: 4, Score: 0.8, EntityType: "PERSON"}},
	}
	remoteNER := &stubRecognizer{
		name:      "PresidioRemoteNERRecognizer",
		entities:  []string{"PERSON", "LOCATION", "ORGANIZATION", "NRP"},
		languages: []string{"en"},
		results:   []RecognizerResult{{Start: 11, End: 19, Score: 0.9, EntityType: "PERSON"}},
	}
	reg.Add(localNER, remoteNER)

	engine := NewAnalyzerEngine(reg)
	_, err := engine.Analyze(context.Background(), "my name is john doe", AnalysisConfig{Language: "en"})
	if err != nil {
		t.Fatal(err)
	}

	if localNER.callCount != 0 {
		t.Fatalf("expected local NER to be skipped for lowercase text, got %d calls", localNER.callCount)
	}
	if remoteNER.callCount != 1 {
		t.Fatalf("expected remote NER to run for lowercase text, got %d calls", remoteNER.callCount)
	}
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
