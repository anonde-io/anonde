package recognizers

import (
	"context"
	"regexp"

	"github.com/anonde-io/anonde/analyzer"
)

// BIC / SWIFT business identifier codes. The ISO 9362 layout is always
// 8 or 11 alphanumeric chars:
//
//	BBBBCCLLBBB     <- BBBB inst, CC country, LL location, BBB branch (opt.)
//	         |--- branch (3 chars, often "XXX" for primary office)
//	       |----- location (2 chars)
//	     |------- country (2 letters)
//	|------------ institution (4 letters)
//
// Examples from the finance_de bench corpus:
//
//	DEUTDEMMXXX     Deutsche Bank Frankfurt
//	BYLADEM1001     BayernLB
//	PBNKDEFFXXX     Postbank Frankfurt
//	INGDDEFFXXX     ING-DiBa Frankfurt
//
// Two precision guards keep the regex from FP'ing on random ALL-CAPS
// strings: (1) the first 6 chars MUST be letters (not digits), and
// (2) the country code is restricted to a list of common ISO 3166-1
// codes whose BICs actually appear in production traffic. This catches
// the realistic universe (DE, AT, CH, FR, IT, ES, NL, BE, GB, US, …)
// without lighting up on every 8-char acronym in clinical text.

var bicRE = regexp.MustCompile(
	`\b[A-Z]{4}` +
		// ISO country-code subset; 30 most-common BIC countries.
		`(?:DE|AT|CH|LI|LU|FR|IT|ES|PT|NL|BE|GB|IE|US|CA|DK|SE|NO|FI|IS|` +
		`PL|CZ|SK|HU|RO|BG|GR|HR|SI|EE|LT|LV|CY|MT|TR|JP|CN|HK|SG|AU|NZ)` +
		`[A-Z0-9]{2}(?:[A-Z0-9]{3})?\b`,
)

// BICRecognizer detects BIC/SWIFT codes. Emits the entity type "ID" so
// the bench label map (anonde.ID -> ID) routes it to the same canonical
// bucket as bank account numbers / clinical IDs / customer numbers.
type BICRecognizer struct{}

// NewBICRecognizer constructs the recognizer.
func NewBICRecognizer() *BICRecognizer { return &BICRecognizer{} }

// Name returns the recognizer name used in logs and conflict resolution.
func (r *BICRecognizer) Name() string { return "BICRecognizer" }

// SupportedEntities returns the entity types this recognizer emits.
func (r *BICRecognizer) SupportedEntities() []string { return []string{"ID"} }

// SupportedLanguages reports `*`; BIC layout is language-independent.
func (r *BICRecognizer) SupportedLanguages() []string { return []string{"*"} }

// Analyze scans text for BIC/SWIFT codes.
func (r *BICRecognizer) Analyze(_ context.Context, text string, _ []string, _ string) ([]analyzer.RecognizerResult, error) {
	if text == "" {
		return nil, nil
	}
	var out []analyzer.RecognizerResult
	for _, m := range bicRE.FindAllStringIndex(text, -1) {
		out = append(out, analyzer.RecognizerResult{
			Start:          m[0],
			End:            m[1],
			Score:          0.90,
			EntityType:     "ID",
			RecognizerName: r.Name(),
		})
	}
	return out, nil
}
