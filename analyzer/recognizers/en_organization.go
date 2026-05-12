package recognizers

import "regexp"

// English healthcare-organisation patterns. Mirrors the design of
// DEOrganizationRecognizer: two regex shapes, both precision-tuned, plus
// a closed list of well-known institutions. Captures the full
// institution span (prefix + suffix).
//
// Suffix shape — 1-4 capitalised prefix tokens, then a healthcare suffix.
// Catches:
//
//   "Mercy General"                          (Mercy + General)
//   "St. Joseph's Hospital"                  (St. + Joseph's + Hospital)
//   "Massachusetts General Hospital"         (3-token prefix + Hospital)
//   "Cleveland Clinic"                       (Cleveland + Clinic)
//   "Cedars-Sinai Medical Center"            (hyphenated + Medical Center)
//
// Closed well-known list — institutions whose names don't include a
// generic suffix word (e.g. Johns Hopkins, Kaiser Permanente) or where
// we want a strict high-precision match regardless of the suffix
// pattern's greediness.

var (
	// Suffix form: "<Prefix Words> <Suffix>".
	//
	// Prefix tokens: [A-Z][a-zA-Z']+ with optional trailing period
	// (handles "St." and "Mt."). Internal hyphens caught by the token
	// class? No — apostrophe and letter only. Hyphenated names like
	// "Cedars-Sinai" need a separate alternative in the prefix token.
	//
	// Separators are horizontal whitespace only ([ \t]+) — never
	// newlines — to avoid eating into the next paragraph header.
	enOrgSuffixRE = regexp.MustCompile(
		`\b(?:[A-Z][a-zA-Z']+\.?|[A-Z][a-zA-Z]+-[A-Z][a-zA-Z]+)` +
			`(?:[ \t]+(?:[A-Z][a-zA-Z']+\.?|[A-Z][a-zA-Z]+-[A-Z][a-zA-Z]+)){0,3}` +
			`[ \t]+` +
			`(?:Hospital|Clinic|Medical[ \t]+Center|Health[ \t]+(?:Center|System)|` +
			`General|Memorial|Healthcare|Infirmary|Sanitarium|Sanatorium|` +
			`Medical[ \t]+Plaza|Polyclinic|Hospice|Health[ \t]+Sciences[ \t]+Center)\b`,
	)

	// Well-known US/UK institutions. Closed list, high precision.
	// Score above the suffix pattern so a well-known match wins the
	// conflict resolution when both fire on the same span.
	enOrgWellKnownRE = regexp.MustCompile(
		`\b(?:` +
			`Mayo[ \t]+Clinic|` +
			`Cleveland[ \t]+Clinic|` +
			`Johns[ \t]+Hopkins(?:[ \t]+Hospital)?|` +
			`Massachusetts[ \t]+General(?:[ \t]+Hospital)?|` +
			`Cedars-Sinai(?:[ \t]+Medical[ \t]+Center)?|` +
			`Mount[ \t]+Sinai(?:[ \t]+Hospital)?|` +
			`Kaiser[ \t]+Permanente|` +
			`NewYork-Presbyterian|NYU[ \t]+Langone|` +
			`UCSF(?:[ \t]+Health)?|UCLA[ \t]+Health|` +
			`Stanford[ \t]+(?:Hospital|Health|Medicine)|` +
			`Brigham[ \t]+and[ \t]+Women's(?:[ \t]+Hospital)?|` +
			`Houston[ \t]+Methodist|` +
			`Mass[ \t]+General(?:[ \t]+Brigham)?|` +
			`Karolinska(?:[ \t]+Institutet)?|` +
			`Charing[ \t]+Cross[ \t]+Hospital|` +
			`Royal[ \t]+London[ \t]+Hospital|` +
			`Guy's[ \t]+and[ \t]+St[ \t]+Thomas|` +
			`Imperial[ \t]+College[ \t]+Healthcare` +
			`)\b`,
	)
)

// NewENOrganizationRecognizer detects English-language healthcare
// organisations. Emits the entity type "ORGANIZATION".
func NewENOrganizationRecognizer() *PatternRecognizer {
	return NewPatternRecognizerWithContext(
		"ENOrganizationRecognizer",
		[]string{"ORGANIZATION"},
		[]string{"en"},
		[]namedPattern{
			{re: enOrgWellKnownRE, score: 0.90},
			{re: enOrgSuffixRE, score: 0.80},
		},
		[]string{
			"hospital", "clinic", "medical", "health",
			"admitted", "discharged", "transferred", "presented",
		},
	)
}
