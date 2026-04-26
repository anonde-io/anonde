package recognizers

import (
	"context"
	"regexp"

	"github.com/moogacs/anonde/analyzer"
)

// PatternRecognizer is a regex-based recognizer.
type PatternRecognizer struct {
	name       string
	entities   []string
	languages  []string
	patterns   []namedPattern
}

type namedPattern struct {
	re    *regexp.Regexp
	score float64
}

// NewPatternRecognizer builds a recognizer from one or more named regex patterns.
func NewPatternRecognizer(name string, entities, languages []string, patterns []namedPattern) *PatternRecognizer {
	return &PatternRecognizer{name: name, entities: entities, languages: languages, patterns: patterns}
}

func (p *PatternRecognizer) Name() string                { return p.name }
func (p *PatternRecognizer) SupportedEntities() []string { return p.entities }
func (p *PatternRecognizer) SupportedLanguages() []string { return p.languages }

func (p *PatternRecognizer) Analyze(_ context.Context, text string, _ []string, _ string) ([]analyzer.RecognizerResult, error) {
	var results []analyzer.RecognizerResult
	for _, pat := range p.patterns {
		matches := pat.re.FindAllStringIndex(text, -1)
		for _, m := range matches {
			results = append(results, analyzer.RecognizerResult{
				Start:          m[0],
				End:            m[1],
				Score:          pat.score,
				EntityType:     p.entities[0],
				RecognizerName: p.name,
			})
		}
	}
	return results, nil
}
