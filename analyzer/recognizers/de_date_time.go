package recognizers

import "regexp"

// German date formats. Designed against the GraSCCo_PHI corpus, which uses:
//
//   01.02.2028        DD.MM.YYYY
//   4.4.1997          D.M.YYYY  (no leading zeros)
//   5.7.54            D.M.YY    (two-digit year)
//   23.04 2029        DD.MM YYYY (data quirk: missing second dot)
//   27. März 2025     DD. Monat YYYY
//   November 2018     Monat YYYY (month + year, no day)
//
// Patterns deliberately do NOT match:
//   - Bare years (2002, 2007) — too many false positives in clinical text
//     (lab values, sample IDs). Catching these requires NER or context.
//   - Partial dates (19.3.) without a year — collide with section numbers.

var (
	// DD.MM.YYYY  or  D.M.YY  with day 1-31 and month 1-12.
	deDateNumericRE = regexp.MustCompile(
		`\b(?:0?[1-9]|[12]\d|3[01])\.(?:0?[1-9]|1[0-2])\.\d{2,4}\b`,
	)

	// DD.MM YYYY — a data quirk in GraSCCo where the second dot is missing.
	// Lower score: more ambiguous, looks like "ratio.month year".
	deDateNumericLooseRE = regexp.MustCompile(
		`\b(?:0?[1-9]|[12]\d|3[01])\.(?:0?[1-9]|1[0-2])\s\d{4}\b`,
	)

	// DD. Monat YYYY (textual German month, full or abbreviated).
	// "März" written as "Mär", "Mar", or "Maerz" all permitted.
	deDateTextualRE = regexp.MustCompile(
		`\b(?:0?[1-9]|[12]\d|3[01])\.\s?` +
			`(?:Januar|Februar|M(?:ä|ae)rz|April|Mai|Juni|Juli|August|September|Oktober|November|Dezember|` +
			`Jan|Feb|M(?:ä|ae)?r|Apr|Jun|Jul|Aug|Sep|Sept|Okt|Nov|Dez)\.?` +
			`\s+\d{2,4}\b`,
	)

	// Monat YYYY (month name + 4-digit year, no day).
	deMonthYearRE = regexp.MustCompile(
		`\b(?:Januar|Februar|M(?:ä|ae)rz|April|Mai|Juni|Juli|August|September|Oktober|November|Dezember)` +
			`\s+\d{4}\b`,
	)
)

// German context keywords that boost DATE_TIME score when they appear nearby.
// Used by analyzer.EnhanceWithContext.
var deDateContextKeywords = []string{
	"geboren", "geb", "datum", "vom", "am", "stattgehabt",
	"aufnahmedatum", "entlassdatum", "operationsdatum",
	"geburtsdatum", "geburtstag",
}

// NewDEDateTimeRecognizer detects DATE_TIME entities in German text.
//
// Registered with language "de" so it only fires when AnalysisConfig.Language
// is set to "de". The existing language-agnostic DateTimeRecognizer covers
// ISO 8601 and US/UK formats and continues to run for all languages.
func NewDEDateTimeRecognizer() *PatternRecognizer {
	return NewPatternRecognizerWithContext(
		"DEDateTimeRecognizer",
		[]string{"DATE_TIME"},
		[]string{"de"},
		[]namedPattern{
			{re: deDateTextualRE, score: 0.95},
			{re: deMonthYearRE, score: 0.85},
			{re: deDateNumericRE, score: 0.85},
			{re: deDateNumericLooseRE, score: 0.65},
		},
		deDateContextKeywords,
	)
}
