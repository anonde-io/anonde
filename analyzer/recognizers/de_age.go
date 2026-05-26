package recognizers

import "regexp"

// German age expressions. AGE is a HIPAA Safe Harbor identifier and a gold
// class in GraSCCo. Patterns are tuned for clinical text:
//
//	65-jährig             compound -jährig adjective
//	65-Jährige             noun form (m/f)
//	65 Jahre alt           "X years old"
//	Patient (65)           parenthesised age after Patient/Patientin
//	Lebensalter 65         labelled age
//	im Alter von 65        "at the age of X"
//
// Bare numbers like "65" alone are intentionally NOT matched; far too
// many such numbers in clinical text (lab values, doses). Every pattern
// here requires a German age-context cue inline.

// (?i) makes the keyword head case-insensitive ("Jahre", "JAhRe",
// "jahre" all match); clean German always title-cases these but
// adversarial corpora scramble case at the character level. The digit
// span is naturally case-irrelevant.
var (
	deAgeJaehrigRE = regexp.MustCompile(
		`(?i)\b(?:0?[1-9]|[1-9]\d|1[01]\d)[-\s]?j[äaä]hrig(?:e[nrms]?)?\b`,
	)
	deAgeJaehrigeRE = regexp.MustCompile(
		`(?i)\b(?:0?[1-9]|[1-9]\d|1[01]\d)[-\s]?j[äaä]hrige[nrms]?\b`,
	)
	deAgeJahreAltRE = regexp.MustCompile(
		`(?i)\b(?:0?[1-9]|[1-9]\d|1[01]\d)\s+(?:jahre|j\.)\s+alt\b`,
	)
	// X Jahre followed by comma / closing paren / semicolon; the common
	// surface for ages in patient-vorstellung headers like
	// "(86 Jahre, geb. 02.08.1954)" where there's no "alt" keyword.
	deAgeJahreClauseRE = regexp.MustCompile(
		`(?i)\b(?:0?[1-9]|[1-9]\d|1[01]\d)\s+jahre[,;)\s]`,
	)
	deAgeParenRE = regexp.MustCompile(
		`(?i)\b(?:Patient|Patientin|Pat\.)\s*\((?:0?[1-9]|[1-9]\d|1[01]\d)\)`,
	)
	deAgeImAlterRE = regexp.MustCompile(
		`(?i)\bim\s+Alter\s+von\s+(?:0?[1-9]|[1-9]\d|1[01]\d)\b`,
	)
	deAgeLebensalterRE = regexp.MustCompile(
		`(?i)\b(?:Lebensalter|Alter)\s*[:.]?\s*(?:0?[1-9]|[1-9]\d|1[01]\d)\b`,
	)
)

// NewDEAgeRecognizer detects AGE entities in German clinical text.
// Emits the entity type "AGE" which maps to the canonical AGE bucket in
// the bench label map.
func NewDEAgeRecognizer() *PatternRecognizer {
	return NewPatternRecognizerWithContext(
		"DEAgeRecognizer",
		[]string{"AGE"},
		[]string{"de"},
		[]namedPattern{
			{re: deAgeJaehrigeRE, score: 0.90},
			{re: deAgeJaehrigRE, score: 0.85},
			{re: deAgeJahreAltRE, score: 0.85},
			{re: deAgeJahreClauseRE, score: 0.78},
			{re: deAgeParenRE, score: 0.85},
			{re: deAgeImAlterRE, score: 0.90},
			{re: deAgeLebensalterRE, score: 0.80},
		},
		[]string{
			"alter", "jahre", "jahrgang", "geburtsjahr",
			"patient", "patientin", "pat.",
		},
	)
}
