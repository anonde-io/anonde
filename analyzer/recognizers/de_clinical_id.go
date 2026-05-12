package recognizers

import (
	"context"
	"regexp"

	"github.com/moogacs/anonde/analyzer"
)

// German clinical-identifier patterns. These are GENERIC clinical IDs
// (case numbers, patient numbers, MRNs, accession numbers) — heterogeneous
// in format and only meaningful in context. The patterns below ALL
// require an inline German identifier keyword to avoid massive false
// positives on every alphanumeric sequence in the document.
//
// Examples this catches from GraSCCo:
//
//	Fall-Nr.: 23346011        → 23346011
//	Patienten-Nr. 100101911   → 100101911
//	Aufnahme-Nr.: A33         → A33
//	MRN: PS3                  → PS3
//	Versichertennummer: ...   → ...

var (
	// Match keyword + optional separator + alnum value of 2-12 chars.
	deClinIDKeywordRE = regexp.MustCompile(
		`\b(?:Fall(?:-)?(?:Nr|Nummer)\.?|` +
			`Patient(?:en)?(?:-)?(?:Nr|Nummer)\.?|` +
			`Aufnahme(?:-)?(?:Nr|Nummer)\.?|` +
			`Akten(?:zeichen|nummer)|` +
			`MRN|` +
			`Versicherten(?:-)?(?:nummer|nr)\.?|` +
			`Patienten[-\s]?ID|` +
			`Krankenversicherten(?:-)?nummer)` +
			`[\s:.]*([A-Z0-9][A-Z0-9-]{1,11})\b`,
	)
)

// DEClinicalIDRecognizer detects clinical identifiers in German PHI text.
// Emits the entity type "ID".
type DEClinicalIDRecognizer struct{}

// NewDEClinicalIDRecognizer constructs the recognizer.
func NewDEClinicalIDRecognizer() *DEClinicalIDRecognizer { return &DEClinicalIDRecognizer{} }

// Name returns the recognizer name for logs and conflict resolution.
func (r *DEClinicalIDRecognizer) Name() string { return "DEClinicalIDRecognizer" }

// SupportedEntities returns the entity types this recognizer emits.
func (r *DEClinicalIDRecognizer) SupportedEntities() []string { return []string{"ID"} }

// SupportedLanguages returns the languages this recognizer applies to.
func (r *DEClinicalIDRecognizer) SupportedLanguages() []string { return []string{"de"} }

// Analyze scans the text and emits ID spans on the *value* portion of each
// keyword-anchored match (not the keyword itself).
func (r *DEClinicalIDRecognizer) Analyze(_ context.Context, text string, _ []string, _ string) ([]analyzer.RecognizerResult, error) {
	if text == "" {
		return nil, nil
	}
	var out []analyzer.RecognizerResult
	for _, m := range deClinIDKeywordRE.FindAllStringSubmatchIndex(text, -1) {
		// Submatch group 1 is the value; m is [fullStart, fullEnd, g1Start, g1End].
		if len(m) < 4 || m[2] < 0 {
			continue
		}
		out = append(out, analyzer.RecognizerResult{
			Start:          m[2],
			End:            m[3],
			Score:          0.80,
			EntityType:     "ID",
			RecognizerName: r.Name(),
		})
	}
	return out, nil
}
