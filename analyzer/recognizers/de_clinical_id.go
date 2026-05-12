package recognizers

import (
	"context"
	"regexp"

	"github.com/moogacs/anonde/analyzer"
)

// DEClinicalIDRecognizer detects German clinical identifiers. GraSCCo shows
// three shapes that account for nearly all gold ID spans:
//
//  1. Keyword-anchored long alphanumerics:
//     Fallnummer: 23346011        FN: 445544767
//     Fall-Nr. 6733340001         E-Nr.: 17217277
//     SV Nr.: 4445311299          Patient-ID: A-202344102
//     Fallzahl: 103354008         Fall: 102341651622
//
//  2. Station / ward / room identifiers:
//     Station A33                 Onkologie-Ambulanz 3
//     chirurgischen Ambulanz CH12 Intensivstation I03
//     Station 4A                  Station O-11           OP II
//
//  3. Histology / lab specimen codes (slash-separated):
//     Histologie (H25440/51)
//
// Patterns ordered most-specific first; the analyzer's conflict resolver
// keeps higher-scored matches when they overlap. Standalone pure-numeric
// IDs without an anchoring keyword are intentionally NOT emitted — too
// many false positives on lab values, dosages, and document numbers in
// unanchored positions.

var (
	// Keyword-anchored IDs. Capturing group 1 is the value.
	// Value shape: 0-4 leading uppercase letters (+ optional hyphen) then
	// a digit, then 1-14 more alphanumeric / hyphen / slash chars. The
	// 0-4 leading-letter range covers bare numerics ("23346011"), single
	// letter ("A-202344102"), two letters ("KL699820", "HN999999"), and
	// three letters ("PAT-202344102").
	deClinicalIDKeywordRE = regexp.MustCompile(
		`\b(?:` +
			`Fall(?:[-\s]?(?:Nr|nummer|zahl))|` +
			`FN|` +
			`E[-\s]?Nr|` +
			`SV[-\s]?Nr|` +
			`MRN|` +
			`Versicherten(?:[-\s]?nummer|[-\s]?nr)|` +
			`Krankenversicherten(?:[-\s]?nummer|[-\s]?nr)|` +
			`Akten(?:zeichen|nummer)|` +
			`Patient(?:en)?[-\s]?(?:Nr|nummer|ID)|` +
			`Pat\.[-\s]?ID|` +
			`Aufnahme(?:[-\s]?Nr|[-\s]?nummer)|` +
			`Versicherungs(?:[-\s]?Nr|[-\s]?nummer)|` +
			`Bericht[-\s]?Nr|` +
			`Auftrag(?:s[-\s]?Nr)?|` +
			`ID(?:[-\s]?(?:Nr|nummer))?|` +
			`Fall` +
			`)` +
			// Any sequence of separator chars (whitespace, ., :, ;, tab)
			// — order-agnostic so we match "Nr.: ", "Nr: ", " :", "\t", "-Nr.\t", etc.
			`[\s.:;,\t]*` +
			`([A-Z]{0,4}-?\d[\dA-Z/-]{1,14})\b`,
	)

	// Station / ward / OP / outpatient-clinic identifiers. The trigger
	// word strongly implies the next token is a room/unit code. Captures
	// short alphanumerics, optionally hyphenated (e.g. "O-11", "KJPP-2").
	deClinicalIDStationRE = regexp.MustCompile(
		`\b(?:` +
			`Station|` +
			`Ambulanz|` +
			`Onkologie-?Ambulanz|` +
			`Onkologie|` +
			`Intensivstation|` +
			`Normalstation|` +
			`chirurgisch(?:en|er)?\s+(?:Klinik\s*-?\s*)?(?:Ambulanz|Station)|` +
			`OP|` +
			`Bett|` +
			`Zimmer|` +
			`Etage|` +
			`Gebäude` +
			`)\s+` +
			`([A-Z]{1,4}-?\d{1,4}|\d{1,4}[A-Z]?(?:-\d{1,4})?|[IVX]{1,4})\b`,
	)

	// Histology / lab specimen codes: optional letter + digits + slash + digits.
	deClinicalIDHistRE = regexp.MustCompile(
		`\b[A-Z]\d{3,6}/\d{1,4}\b`,
	)
)

// DEClinicalIDRecognizer is the concrete recognizer type.
type DEClinicalIDRecognizer struct{}

// NewDEClinicalIDRecognizer constructs the recognizer.
func NewDEClinicalIDRecognizer() *DEClinicalIDRecognizer { return &DEClinicalIDRecognizer{} }

// Name returns the recognizer name used in logs and conflict resolution.
func (r *DEClinicalIDRecognizer) Name() string { return "DEClinicalIDRecognizer" }

// SupportedEntities returns the entity types this recognizer emits.
func (r *DEClinicalIDRecognizer) SupportedEntities() []string { return []string{"ID"} }

// SupportedLanguages returns the languages this recognizer applies to.
func (r *DEClinicalIDRecognizer) SupportedLanguages() []string { return []string{"de"} }

// Analyze scans text for the three ID shapes and emits the VALUE portion
// of each (not the trigger keyword).
func (r *DEClinicalIDRecognizer) Analyze(_ context.Context, text string, _ []string, _ string) ([]analyzer.RecognizerResult, error) {
	if text == "" {
		return nil, nil
	}
	var out []analyzer.RecognizerResult

	// 1. Keyword-anchored. Submatch group 1 = value.
	for _, m := range deClinicalIDKeywordRE.FindAllStringSubmatchIndex(text, -1) {
		if len(m) < 4 || m[2] < 0 {
			continue
		}
		out = append(out, analyzer.RecognizerResult{
			Start:          m[2],
			End:            m[3],
			Score:          0.85,
			EntityType:     "ID",
			RecognizerName: r.Name(),
		})
	}

	// 2. Station / ward. Submatch group 1 = value.
	for _, m := range deClinicalIDStationRE.FindAllStringSubmatchIndex(text, -1) {
		if len(m) < 4 || m[2] < 0 {
			continue
		}
		out = append(out, analyzer.RecognizerResult{
			Start:          m[2],
			End:            m[3],
			Score:          0.75,
			EntityType:     "ID",
			RecognizerName: r.Name(),
		})
	}

	// 3. Histology / lab specimen codes.
	for _, m := range deClinicalIDHistRE.FindAllStringIndex(text, -1) {
		out = append(out, analyzer.RecognizerResult{
			Start:          m[0],
			End:            m[1],
			Score:          0.80,
			EntityType:     "ID",
			RecognizerName: r.Name(),
		})
	}

	return out, nil
}
