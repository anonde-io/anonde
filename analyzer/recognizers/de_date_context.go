package recognizers

import (
	"context"
	"regexp"
	"strings"

	"github.com/moogacs/anonde/analyzer"
)

// DEDateContextRecognizer detects bare years (1900–2099) and partial dates
// (DD.MM. without a year) in German clinical text when a date-related keyword
// appears within deDateContextWindow chars before the candidate.
//
// Bare 4-digit numbers are too common in clinical text to emit
// unconditionally — they collide with lab values, dosages, document IDs.
// Partial dates collide with section numbers like "siehe 19.3.". The
// keyword window is the only defense against those false positives.
//
// Designed against GraSCCo_PHI, where bare-year PHI typically appears in
// surgical-history bullets ("Z.n. Apoplex 2002") and partial dates appear
// in date ranges ("vom 19.3. bis 22.3.2029").

var (
	// Bare 4-digit year; the surrounding context check decides if it's a date.
	deBareYearRE = regexp.MustCompile(`\b(?:19|20)\d{2}\b`)

	// Partial date DD.MM. — day 1-31, month 1-12, mandatory trailing dot.
	// Overlaps with a full DD.MM.YYYY are resolved by the analyzer's conflict
	// pass (full match has a higher score and wins).
	dePartialDateRE = regexp.MustCompile(
		`\b(?:0?[1-9]|[12]\d|3[01])\.(?:0?[1-9]|1[0-2])\.`,
	)
)

// Trigger phrases scanned in a lower-cased window before each candidate.
// Each entry is matched as a plain substring after lowercasing the text — so
// boundary characters (leading/trailing space) are significant. Avoid bare
// short words like "am" without surrounding spaces, since they would fire on
// any word ending in "am".
var deDateContextTriggers = []string{
	"z.n.", "st.p.",                 // Zustand nach / Status post (surgical history)
	"seit ", "vom ", "bis ", " ab ", // date-range prepositions
	" am ",
	"geb.", "geboren ", "jahrgang", "geburtsjahr",
	"erstdiagnose", "ed ", "ed.",
	"uicc", "ajcc",         // oncology staging classifications
	"stadium ",
	"diagnose",
	"behandelt ", "behandlung",
	"aufnahme", "entlassung",
	"(* ", "* ",            // German birth-marker asterisk: "* 1978"
}

const deDateContextWindow = 40 // chars before candidate scanned for triggers

// DEDateContextRecognizer is the concrete recognizer type. Constructed via
// NewDEDateContextRecognizer.
type DEDateContextRecognizer struct{}

// NewDEDateContextRecognizer builds the context-gated DE date recognizer.
func NewDEDateContextRecognizer() *DEDateContextRecognizer {
	return &DEDateContextRecognizer{}
}

// Name returns the recognizer name used in logs and conflict resolution.
func (r *DEDateContextRecognizer) Name() string { return "DEDateContextRecognizer" }

// SupportedEntities returns the entity types this recognizer can emit.
func (r *DEDateContextRecognizer) SupportedEntities() []string { return []string{"DATE_TIME"} }

// SupportedLanguages returns the languages this recognizer applies to.
func (r *DEDateContextRecognizer) SupportedLanguages() []string { return []string{"de"} }

// Analyze scans text for bare years and partial dates and emits each one
// whose preceding window contains a known date-context trigger.
func (r *DEDateContextRecognizer) Analyze(_ context.Context, text string, _ []string, _ string) ([]analyzer.RecognizerResult, error) {
	if text == "" {
		return nil, nil
	}
	lower := strings.ToLower(text)
	var out []analyzer.RecognizerResult

	tryEmit := func(start, end int, score float64) {
		window := lower[max(start-deDateContextWindow, 0):start]
		for _, trig := range deDateContextTriggers {
			if strings.Contains(window, trig) {
				out = append(out, analyzer.RecognizerResult{
					Start:          start,
					End:            end,
					Score:          score,
					EntityType:     "DATE_TIME",
					RecognizerName: r.Name(),
				})
				return
			}
		}
	}

	for _, m := range deBareYearRE.FindAllStringIndex(text, -1) {
		tryEmit(m[0], m[1], 0.70)
	}
	for _, m := range dePartialDateRE.FindAllStringIndex(text, -1) {
		tryEmit(m[0], m[1], 0.80)
	}
	return out, nil
}
