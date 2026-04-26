package analyzer

import "context"

// EntityRecognizer is implemented by all PII recognizers.
type EntityRecognizer interface {
	Name() string
	SupportedEntities() []string
	SupportedLanguages() []string
	Analyze(ctx context.Context, text string, entities []string, language string) ([]RecognizerResult, error)
}
