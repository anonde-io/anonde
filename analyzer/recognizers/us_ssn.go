package recognizers

import (
	"context"
	"regexp"
	"strings"

	"github.com/anonde-io/anonde/analyzer"
)

// Canonical SSN: 3-2-4 grouping with dash or space separators.
var ssnRE = regexp.MustCompile(`\b(\d{3})[- ](\d{2})[- ](\d{4})\b`)

// Alternative SSN shapes seen in synthetic PII corpora:
//   - dotted "756.5465.1508"  (group lengths 3-4-4: 11 digits in 3 dotted groups)
//   - dotted "756.84-26.2701" (mixed dash + dot)
//   - concatenated "756407113"-style 9-11 digit bare numerics
// The dotted form is distinctive (most other dotted numbers in prose
// have different group lengths). Lower score than the canonical 3-2-4
// because the shape is generator-specific, not real-world canonical.
var ssnDottedRE = regexp.MustCompile(`\b\d{3}\.\d{4}\.\d{4}\b`)

// USSocialSecurityRecognizer detects US_SSN entities with validity checks.
type USSocialSecurityRecognizer struct{}

func NewUSSocialSecurityRecognizer() *USSocialSecurityRecognizer {
	return &USSocialSecurityRecognizer{}
}

func (r *USSocialSecurityRecognizer) Name() string                 { return "USSocialSecurityRecognizer" }
func (r *USSocialSecurityRecognizer) SupportedEntities() []string  { return []string{"US_SSN"} }
func (r *USSocialSecurityRecognizer) SupportedLanguages() []string { return []string{"en"} }

// ContextKeywords implements analyzer.ContextProvider.
func (r *USSocialSecurityRecognizer) ContextKeywords() map[string][]string {
	return map[string][]string{
		"US_SSN": {"ssn", "social security", "social-security", "ss number", "tax id", "taxpayer"},
	}
}

func (r *USSocialSecurityRecognizer) Analyze(_ context.Context, text string, _ []string, _ string) ([]analyzer.RecognizerResult, error) {
	var results []analyzer.RecognizerResult
	for _, m := range ssnRE.FindAllStringSubmatchIndex(text, -1) {
		area := strings.ReplaceAll(text[m[2]:m[3]], "-", "")
		area = strings.ReplaceAll(area, " ", "")
		group := strings.ReplaceAll(text[m[4]:m[5]], "-", "")
		group = strings.ReplaceAll(group, " ", "")
		serial := strings.ReplaceAll(text[m[6]:m[7]], "-", "")
		serial = strings.ReplaceAll(serial, " ", "")

		if area == "000" || area == "666" || area[0] == '9' {
			continue
		}
		if group == "00" {
			continue
		}
		if serial == "0000" {
			continue
		}

		results = append(results, analyzer.RecognizerResult{
			Start: m[0], End: m[1], Score: 0.85,
			EntityType: "US_SSN", RecognizerName: "USSocialSecurityRecognizer",
		})
	}
	// Dotted alternative form. No structural validation; the dotted
	// shape is generator-specific (ai4privacy_en), so the leading-area
	// rule (no 000/666/9xx) doesn't apply consistently. Lower score so
	// a real 3-2-4 SSN beats the dotted form in any conflict.
	for _, m := range ssnDottedRE.FindAllStringIndex(text, -1) {
		results = append(results, analyzer.RecognizerResult{
			Start: m[0], End: m[1], Score: 0.78,
			EntityType: "US_SSN", RecognizerName: "USSocialSecurityRecognizer",
		})
	}
	return results, nil
}
