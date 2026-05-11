package recognizers

import (
	"context"
	"regexp"
	"strings"

	"github.com/moogacs/anonde/analyzer"
)

// DE postal code (Postleitzahl) is 5 digits. A bare 5-digit number is FAR
// too ambiguous in clinical text (case numbers, dosages, sample IDs) to
// emit unconditionally. This recognizer therefore requires one of two
// disambiguating signals:
//
//	1. Followed by a capitalized city/town name on the same line, e.g.
//	   "24937 Flensburg", "80339 München". This is the canonical
//	   PLZ-City pattern from German addresses.
//	2. Preceded by an address-context keyword within a small window
//	   ("PLZ", "Postleitzahl", "Anschrift", a German street-suffix word
//	   ending in "-str."/"-straße"/"-weg"/"-platz" recently appearing).
//
// Also handles Swiss/Austrian formats with country prefix ("A-1010",
// "CH-8001") because GraSCCo includes those.

var (
	// 5-digit German PLZ followed by a city: "12345 Berlin" or
	// "12345 Sankt Augustin" — accept 1-3 city tokens.
	dePLZWithCityRE = regexp.MustCompile(
		`\b\d{5}\s+[A-ZÄÖÜ][a-zäöüß-]+(?:\s+[A-ZÄÖÜ][a-zäöüß-]+){0,2}\b`,
	)

	// Country-prefixed PLZ: A-1010, CH-8001, D-12345, FL-9490.
	dePLZCountryRE = regexp.MustCompile(
		`\b(?:A|D|CH|FL|L|I|F)-\d{4,5}\b`,
	)

	// Bare 5-digit number — only emitted with explicit context (see below).
	dePLZBareRE = regexp.MustCompile(`\b\d{5}\b`)
)

// dePLZContextTriggers must appear within deDateContextWindow (reused — 40
// chars) of a bare 5-digit number for it to be emitted as a PLZ. Same
// design as DEDateContextRecognizer: lower-cased substring match.
var dePLZContextTriggers = []string{
	"plz", "postleitzahl",
	"anschrift", "adresse", "wohnhaft",
	"-str.", "-straße", "straße", "str.",
	"weg ", "platz ", "gasse ", "pfad ",
}

const dePLZContextWindow = 60

// DEPostalCodeRecognizer emits ADDRESS spans for German/Austrian/Swiss
// postal codes when they appear in an address-y context.
type DEPostalCodeRecognizer struct{}

// NewDEPostalCodeRecognizer constructs the recognizer.
func NewDEPostalCodeRecognizer() *DEPostalCodeRecognizer {
	return &DEPostalCodeRecognizer{}
}

// Name returns the recognizer name for logs and conflict resolution.
func (r *DEPostalCodeRecognizer) Name() string { return "DEPostalCodeRecognizer" }

// SupportedEntities returns the entity types this recognizer emits.
// POSTAL_CODE is a distinct type so callers can redact postal codes
// differently from city names (LOCATION) — e.g. keep the city for medical
// statistics but mask the postal code.
func (r *DEPostalCodeRecognizer) SupportedEntities() []string { return []string{"POSTAL_CODE"} }

// SupportedLanguages returns the languages this recognizer applies to.
func (r *DEPostalCodeRecognizer) SupportedLanguages() []string { return []string{"de"} }

// Analyze scans text for PLZ candidates and emits each one whose context
// disambiguates it from an unrelated 5-digit number.
func (r *DEPostalCodeRecognizer) Analyze(_ context.Context, text string, _ []string, _ string) ([]analyzer.RecognizerResult, error) {
	if text == "" {
		return nil, nil
	}
	var out []analyzer.RecognizerResult

	// 1. High-confidence: PLZ + city on same line.
	for _, m := range dePLZWithCityRE.FindAllStringIndex(text, -1) {
		out = append(out, analyzer.RecognizerResult{
			Start:          m[0],
			End:            m[1],
			Score:          0.85,
			EntityType:     "POSTAL_CODE",
			RecognizerName: r.Name(),
		})
	}

	// 2. High-confidence: country-prefixed PLZ.
	for _, m := range dePLZCountryRE.FindAllStringIndex(text, -1) {
		out = append(out, analyzer.RecognizerResult{
			Start:          m[0],
			End:            m[1],
			Score:          0.85,
			EntityType:     "POSTAL_CODE",
			RecognizerName: r.Name(),
		})
	}

	// 3. Medium-confidence: bare 5-digit with nearby address keyword.
	lower := strings.ToLower(text)
	for _, m := range dePLZBareRE.FindAllStringIndex(text, -1) {
		// Skip if already inside a high-confidence match.
		if covered(out, m[0], m[1]) {
			continue
		}
		winStart := max(m[0]-dePLZContextWindow, 0)
		window := lower[winStart:m[0]]
		hasContext := false
		for _, trig := range dePLZContextTriggers {
			if strings.Contains(window, trig) {
				hasContext = true
				break
			}
		}
		if hasContext {
			out = append(out, analyzer.RecognizerResult{
				Start:          m[0],
				End:            m[1],
				Score:          0.65,
				EntityType:     "POSTAL_CODE",
				RecognizerName: r.Name(),
			})
		}
	}
	return out, nil
}

// covered returns true if any existing finding fully contains [start,end].
// Used to skip bare-PLZ matches that already live inside a high-confidence
// "PLZ + city" span.
func covered(found []analyzer.RecognizerResult, start, end int) bool {
	for _, r := range found {
		if r.Start <= start && r.End >= end {
			return true
		}
	}
	return false
}
