package analyzer

import (
	"context"
	"strings"
	"sync"
	"unicode"
)

// AnalysisConfig configures a single Analyze call.
type AnalysisConfig struct {
	// Language to use for recognizer filtering (e.g. "en"). Defaults to "en".
	Language string
	// Entities to detect. Empty means all supported entities.
	Entities []string
	// ScoreThreshold filters results below this score. Defaults to 0.
	ScoreThreshold float64
	// RemoveConflicts removes overlapping spans, keeping the best one.
	RemoveConflicts bool
	// DisableNER skips NER-based recognizers (PERSON, LOCATION, ORGANIZATION, NRP).
	// Use when you only need pattern-based entities and want maximum throughput.
	DisableNER bool
}

// AnalyzerEngine detects PII entities in text.
type AnalyzerEngine struct {
	Registry *RecognizerRegistry
}

// hasCapitalisedWords returns true if the text contains at least one word that
// starts with an uppercase letter and is not the very first character — a
// necessary (not sufficient) condition for NER entities to be present.
func hasCapitalisedWords(text string) bool {
	inWord := false
	for i, r := range text {
		if unicode.IsLetter(r) {
			if !inWord && i > 0 && unicode.IsUpper(r) {
				return true
			}
			inWord = true
		} else {
			inWord = false
		}
	}
	return false
}

func isNERBasedRecognizer(rec EntityRecognizer) bool {
	for _, entity := range rec.SupportedEntities() {
		switch entity {
		case "PERSON", "LOCATION", "ORGANIZATION", "NRP":
			return true
		}
	}
	return false
}

// usesCapitalisedTextHeuristic returns true for recognizers that rely heavily on
// case cues and can be safely skipped when no interior capitalized words exist.
func usesCapitalisedTextHeuristic(rec EntityRecognizer) bool {
	return rec.Name() == "NERRecognizer"
}

// NewAnalyzerEngine returns an engine backed by the given registry.
func NewAnalyzerEngine(registry *RecognizerRegistry) *AnalyzerEngine {
	return &AnalyzerEngine{Registry: registry}
}

// Analyze runs all applicable recognizers against text and returns deduplicated results.
func (e *AnalyzerEngine) Analyze(ctx context.Context, text string, cfg AnalysisConfig) ([]RecognizerResult, error) {
	if cfg.Language == "" {
		cfg.Language = "en"
	}

	candidates := e.Registry.GetByLanguage(cfg.Language)

	hasCaps := hasCapitalisedWords(text)

	// Drop NER-based recognizers when disabled.
	// Also apply the capitalised-word heuristic only to recognizers that opt in.
	if cfg.DisableNER || !hasCaps {
		filtered := candidates[:0:0]
		for _, rec := range candidates {
			if cfg.DisableNER && isNERBasedRecognizer(rec) {
				continue
			}
			if !cfg.DisableNER && !hasCaps && usesCapitalisedTextHeuristic(rec) {
				continue
			}
			filtered = append(filtered, rec)
		}
		candidates = filtered
	}

	if len(cfg.Entities) > 0 {
		entitySet := make(map[string]struct{}, len(cfg.Entities))
		for _, en := range cfg.Entities {
			entitySet[strings.ToUpper(en)] = struct{}{}
		}
		filtered := candidates[:0]
		for _, rec := range candidates {
			for _, se := range rec.SupportedEntities() {
				if _, ok := entitySet[se]; ok {
					filtered = append(filtered, rec)
					break
				}
			}
		}
		candidates = filtered
	}

	type partial struct {
		results []RecognizerResult
		err     error
	}

	ch := make(chan partial, len(candidates))
	var wg sync.WaitGroup
	for _, rec := range candidates {
		wg.Add(1)
		go func(r EntityRecognizer) {
			defer wg.Done()
			res, err := r.Analyze(ctx, text, cfg.Entities, cfg.Language)
			ch <- partial{res, err}
		}(rec)
	}
	wg.Wait()
	close(ch)

	var all []RecognizerResult
	for p := range ch {
		if p.err != nil {
			return nil, p.err
		}
		for _, r := range p.results {
			if r.Score >= cfg.ScoreThreshold {
				all = append(all, r)
			}
		}
	}

	if cfg.RemoveConflicts {
		all = RemoveConflicts(all)
	} else {
		SortResults(all)
	}
	return all, nil
}
