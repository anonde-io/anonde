package recognizers

import "regexp"

// German phone-number formats observed in real clinical text (GraSCCo_PHI):
//
//	08991/23354          area / local with slash separator
//	0699-15099887        area-local with hyphen
//	(0461) 708 - 223     parenthesized area + extension
//	0261 210-39989       space-separated area + hyphen extension
//	+43 (453) 14         international with parenthesized area
//	+43(0)333 775-8422   international with trunk-zero in parens
//
// The English PhoneRecognizer already handles +CC and US/UK formats; this
// recognizer fills the German gap (slash-separated and area-then-extension
// shapes). Patterns are ordered specific → general; the analyzer's conflict
// resolver keeps the highest-score match on overlaps.

var (
	// 0AAAA/NNNNN — area code "0…" of 3–6 digits, slash, 4–10 local digits.
	dePhoneSlashRE = regexp.MustCompile(
		`\b0\d{2,5}\/\s?\d{3,10}(?:[-\s]\d{1,8})?\b`,
	)

	// 0AAA NNNN-NNNN   or   0AAA-NNNN-NNNN
	// Area code starts with 0, 2–5 digits; separators are space or hyphen;
	// at least one separator group; ≥7 digits total after the area code.
	dePhoneAreaSepRE = regexp.MustCompile(
		`\b0\d{2,4}[-\s]\d{2,5}[-\s]?\d{2,8}\b`,
	)

	// (0AAA) NNN[-NNNNN]   parenthesized area, optional extension
	dePhoneParenRE = regexp.MustCompile(
		`\(\s?0\d{2,4}\s?\)\s?\d{2,6}(?:\s?[-\s]\s?\d{1,8})?`,
	)

	// +49 / +43 / +41 with various groupings (covers Germany, Austria,
	// Switzerland). The leading +CC is mandatory. Area-code segment may be:
	//   (0)NNNN   trunk-zero in parens then area      e.g. +43(0)333
	//   (NNNN)    area in parens                       e.g. +43 (453)
	//   NNNN      bare area                            e.g. +49 30
	// Trailing 0-4 separator-delimited digit groups capture multi-part
	// extensions like " 775-8422334".
	dePhoneIntlRE = regexp.MustCompile(
		`\+(?:49|43|41)\s?(?:\(0\)\s?\d{1,5}|\(\d{1,5}\)|\d{1,5})(?:[\s-]+\d{1,8}){0,4}`,
	)
)

// NewDEPhoneRecognizer detects PHONE_NUMBER entities in German-style formats.
// Registered for language "de" so it complements (does not replace) the
// language-agnostic English/international PhoneRecognizer.
func NewDEPhoneRecognizer() *PatternRecognizer {
	return NewPatternRecognizerWithContext(
		"DEPhoneRecognizer",
		[]string{"PHONE_NUMBER"},
		[]string{"de"},
		[]namedPattern{
			{re: dePhoneSlashRE, score: 0.85},
			{re: dePhoneAreaSepRE, score: 0.75},
			{re: dePhoneParenRE, score: 0.85},
			{re: dePhoneIntlRE, score: 0.85},
		},
		// German context keywords for score boosting via EnhanceWithContext.
		[]string{
			"telefon", "tel", "tel.", "fon",
			"handy", "mobil", "mobile",
			"fax", "telefax",
			"rufnummer", "durchwahl",
			"erreichbar", "kontakt",
		},
	)
}
