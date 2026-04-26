package recognizers

import (
	"context"
	"regexp"

	"github.com/moogacs/anonde/analyzer"
)

// ITIN: 9XX-7X-XXXX where the middle group is 70-88, 90-92, or 94-99.
var usITINRE = regexp.MustCompile(`\b(9\d{2})[- ](\d{2})[- ](\d{4})\b`)

// USITINRecognizer detects US_ITIN entities.
type USITINRecognizer struct{}

func NewUSITINRecognizer() *USITINRecognizer { return &USITINRecognizer{} }

func (r *USITINRecognizer) Name() string                 { return "USITINRecognizer" }
func (r *USITINRecognizer) SupportedEntities() []string  { return []string{"US_ITIN"} }
func (r *USITINRecognizer) SupportedLanguages() []string { return []string{"en"} }

func (r *USITINRecognizer) Analyze(_ context.Context, text string, _ []string, _ string) ([]analyzer.RecognizerResult, error) {
	var results []analyzer.RecognizerResult
	for _, m := range usITINRE.FindAllStringSubmatchIndex(text, -1) {
		mid := text[m[4]:m[5]]
		var n int
		for _, ch := range mid {
			n = n*10 + int(ch-'0')
		}
		valid := (n >= 70 && n <= 88) || (n >= 90 && n <= 92) || (n >= 94 && n <= 99)
		if !valid {
			continue
		}
		results = append(results, analyzer.RecognizerResult{
			Start: m[0], End: m[1], Score: 0.85,
			EntityType: "US_ITIN", RecognizerName: "USITINRecognizer",
		})
	}
	return results, nil
}
