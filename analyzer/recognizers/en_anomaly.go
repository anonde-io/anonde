package recognizers

import (
	"context"
	"regexp"

	"github.com/moogacs/anonde/analyzer"
)

// ENAnomalyRecognizer detects PERSON candidates in English clinical text
// using a single structural signal: an English honorific or clinical
// label immediately followed by 1–4 capitalised name tokens. Emits PERSON
// at score 0.85.
//
// Why no statistical multi-token pattern (unlike DEAnomalyRecognizer):
// the German recognizer uses an embedded medical vocabulary as a
// denylist to suppress "Klinische Befunde" / "Postoperative Anordnungen"
// type false positives. We don't ship an English clinical vocabulary,
// so a bare "two capitalised tokens" pattern would over-fire on every
// section header, drug name, and section title in an English clinical
// letter. Structural title-anchored capture is the precision-friendly
// floor; deeper recall on English narrative text requires the hugot
// NER variant.
//
// Captures examples:
//   - "Mr. John Smith"            → "John Smith"
//   - "Dr. Sarah Williams"        → "Sarah Williams"
//   - "Patient: Omar Hassan"      → "Omar Hassan"
//   - "Pt. Mary Jane Watson"      → "Mary Jane Watson"   (3 tokens)
//   - "Mrs Eliza Thompson-Brown"  → "Eliza Thompson-Brown" (hyphenated)
//
// Anchors are matched without surrounding capture so only the name
// portion ends up as the finding span — consistent with the German
// recognizer's NAME_PATIENT / NAME_DOCTOR conventions.

// enAnomalyTitledRE matches an English honorific / clinical label, then
// captures 1-4 capitalised name tokens in group 1.
//
// Name token shape: [A-Z][a-zA-Z'-]{1,30}.
//   - Apostrophe allowed: "O'Connor", "D'Angelo".
//   - Internal hyphen allowed: "Thompson-Brown", "Smith-Jones".
//   - No spaces in a single token; multi-word names span via [ \t]+.
var enAnomalyTitledRE = regexp.MustCompile(
	`\b(?:` +
		// Honorifics — period optional ("Mr Smith" vs "Mr. Smith")
		`Mr\.?|Mrs\.?|Ms\.?|Miss|Mister|Madam|Sir|` +
		// Medical honorifics
		`Dr\.?|Prof\.?|Doctor|Professor|` +
		// Clinical labels
		`Pt\.?|Patient:?|the[ \t]+patient` +
		`)` +
		`[ \t]+` +
		`([A-Z][a-zA-Z'-]{1,30}(?:[ \t]+[A-Z][a-zA-Z'-]{1,30}){0,3})\b`,
)

// ENAnomalyRecognizer recognises English-language PERSON candidates by
// title-anchored capture.
type ENAnomalyRecognizer struct{}

// NewENAnomalyRecognizer constructs the recognizer.
func NewENAnomalyRecognizer() *ENAnomalyRecognizer { return &ENAnomalyRecognizer{} }

// Name returns the recognizer name used in logs and conflict resolution.
func (r *ENAnomalyRecognizer) Name() string { return "ENAnomalyRecognizer" }

// SupportedEntities returns the entity types this recognizer emits.
func (r *ENAnomalyRecognizer) SupportedEntities() []string { return []string{"PERSON"} }

// SupportedLanguages returns the languages this recognizer applies to.
func (r *ENAnomalyRecognizer) SupportedLanguages() []string { return []string{"en"} }

// Analyze emits PERSON findings for each title-anchored name match.
// Group 1 of the regex is the name; we emit that span, not the title.
func (r *ENAnomalyRecognizer) Analyze(_ context.Context, text string, _ []string, _ string) ([]analyzer.RecognizerResult, error) {
	if text == "" {
		return nil, nil
	}
	var out []analyzer.RecognizerResult
	for _, m := range enAnomalyTitledRE.FindAllStringSubmatchIndex(text, -1) {
		if len(m) < 4 || m[2] < 0 {
			continue
		}
		out = append(out, analyzer.RecognizerResult{
			Start:          m[2],
			End:            m[3],
			Score:          0.85,
			EntityType:     "PERSON",
			RecognizerName: r.Name(),
		})
	}
	return out, nil
}
