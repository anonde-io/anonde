package analyzer

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

// poolStub is a minimal recognizer that counts its Analyze calls and
// emits one deterministic, non-overlapping finding. It exercises the
// bounded pattern-worker fan-out: many of these dispatched through the
// pool must each run exactly once and contribute their finding.
type poolStub struct {
	name      string
	entities  []string
	languages []string
	start     int
	calls     atomic.Int64
}

func (s *poolStub) Name() string                 { return s.name }
func (s *poolStub) SupportedEntities() []string  { return s.entities }
func (s *poolStub) SupportedLanguages() []string { return s.languages }

func (s *poolStub) Analyze(_ context.Context, _ string, _ []string, _ string) ([]RecognizerResult, error) {
	s.calls.Add(1)
	// Fresh slice per call; distinct, width-1, non-overlapping span so
	// RemoveConflicts keeps every finding (no conflict collapses them).
	return []RecognizerResult{{
		Start:          s.start,
		End:            s.start + 1,
		Score:          0.9,
		EntityType:     "EMAIL_ADDRESS",
		RecognizerName: s.name,
	}}, nil
}

// buildPoolRegistry returns a registry with `pattern` pattern stubs and
// `ner` NER-named stubs, all with distinct non-overlapping spans, plus
// the stub slice for call-count assertions. Total recognizers =
// pattern+ner, total span width = 2*(pattern+ner).
func buildPoolRegistry(pattern, ner int) (*RecognizerRegistry, []*poolStub) {
	reg := NewRecognizerRegistry()
	total := pattern + ner
	stubs := make([]*poolStub, 0, total)
	for i := 0; i < pattern; i++ {
		s := &poolStub{
			name:      fmt.Sprintf("Stub%dRecognizer", i), // pattern: does NOT end in "NERRecognizer"
			entities:  []string{"EMAIL_ADDRESS"},
			languages: []string{"en"},
			start:     i * 2,
		}
		stubs = append(stubs, s)
		reg.Add(s)
	}
	for i := 0; i < ner; i++ {
		s := &poolStub{
			name:      fmt.Sprintf("Stub%dNERRecognizer", i), // routed to a dedicated goroutine
			entities:  []string{"EMAIL_ADDRESS"},
			languages: []string{"en"},
			start:     (pattern + i) * 2,
		}
		stubs = append(stubs, s)
		reg.Add(s)
	}
	return reg, stubs
}

// TestAnalyze_PatternWorkerPool_RunsEachRecognizerOnce verifies the
// bounded worker pool dispatches each pattern recognizer exactly once and
// keeps every finding, and that NER-named recognizers on their dedicated
// goroutines do the same. 80 pattern recognizers exceed any plausible
// GOMAXPROCS, so the pool must recycle its workers across the backlog.
func TestAnalyze_PatternWorkerPool_RunsEachRecognizerOnce(t *testing.T) {
	t.Parallel()

	const (
		pattern = 80
		ner     = 3
		total   = pattern + ner
	)
	reg, stubs := buildPoolRegistry(pattern, ner)
	engine := NewAnalyzerEngine(reg)
	text := strings.Repeat("x", total*2)

	got, err := engine.Analyze(context.Background(), text, AnalysisConfig{
		Language:        "en",
		RemoveConflicts: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != total {
		t.Fatalf("expected %d findings (one per recognizer), got %d", total, len(got))
	}
	for _, s := range stubs {
		if c := s.calls.Load(); c != 1 {
			t.Fatalf("recognizer %s ran %d times, want exactly 1", s.name, c)
		}
	}
	// Findings must be sorted by start (RemoveConflicts sort) and cover
	// every recognizer's span — proves the pool loses nothing.
	for i, r := range got {
		if r.Start != i*2 {
			t.Fatalf("finding %d has Start=%d, want %d (a span was dropped or reordered)", i, r.Start, i*2)
		}
	}
}

// TestAnalyze_PatternWorkerPool_ConcurrentCallsAreComplete runs Analyze
// from many goroutines against a shared engine and asserts every call
// returns the complete finding set. Under -race this is the load-bearing
// check that the shared result collection in the worker pool is safe.
func TestAnalyze_PatternWorkerPool_ConcurrentCallsAreComplete(t *testing.T) {
	t.Parallel()

	const (
		pattern    = 64
		ner        = 2
		total      = pattern + ner
		goroutines = 48
	)
	reg, _ := buildPoolRegistry(pattern, ner)
	engine := NewAnalyzerEngine(reg)
	text := strings.Repeat("x", total*2)

	var wg sync.WaitGroup
	errs := make(chan error, goroutines)
	wg.Add(goroutines)
	for g := 0; g < goroutines; g++ {
		go func() {
			defer wg.Done()
			got, err := engine.Analyze(context.Background(), text, AnalysisConfig{
				Language:        "en",
				RemoveConflicts: true,
			})
			if err != nil {
				errs <- err
				return
			}
			if len(got) != total {
				errs <- fmt.Errorf("concurrent Analyze returned %d findings, want %d", len(got), total)
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
}
