package recognizers

import "regexp"

// German healthcare-organisation patterns. Hospitals, clinics, and
// medical practices have predictable name shapes in German:
//
//	*-Klinikum, *-Krankenhaus, *-Klinik
//	Universitätsklinik(um) *, Universitätsspital *
//	Praxis Dr. *, Praxis Prof. *, Praxis für *
//	*-Zentrum (clinical centres)
//	Medizinische Hochschule *
//
// The patterns below are precision-tuned: each requires either a
// structural suffix/prefix that strongly indicates an organisation, OR
// a "Dr."/"Prof." token + plausible institution name nearby.

var (
	// Suffix form: <stem>-Klinikum / -Krankenhaus / -Klinik / -Zentrum / -Spital
	// Optionally followed by 1-2 capitalised words ("Klinikum rechts der Isar").
	//
	// (?i) makes the whole pattern case-insensitive; clean clinical
	// German always title-cases these words, but adversarial corpora
	// scramble case ("SAnA KLiNiKuM"). The case-insensitive flag costs
	// nothing on legitimate input (matches the same strings) and recovers
	// ~100 LOCATION_HOSPITAL leaks on adversarial_de. The inner stem
	// class drops the ALL-CAPS requirement for the same reason.
	deOrgSuffixRE = regexp.MustCompile(
		`(?i)\b[a-zäöüß][a-zäöüß-]{2,30}(?:[- ](?:Klinikum|Krankenhaus|Klinik|Zentrum|Spital|Hospital|Praxis|Ambulanz|Reha-?Zentrum))\b`,
	)

	// Prefix form: Universitätsklinik(um) / Medizinische Hochschule / ...
	//
	// Separators are horizontal whitespace only ([ \t]+). \s+ would let the
	// pattern eat across newlines into the next line, e.g. "Klinikum
	// rechts der Isar\nNotaufnahme" incorrectly captured "...der Isar
	// Notaufnahme" as one organisation. Fix matches typical document
	// layout where a clinic name doesn't break across a newline.
	deOrgPrefixRE = regexp.MustCompile(
		`(?i)\b(?:Universit[äa]tsklinik(?:um)?|Universit[äa]tsspital|Klinikum|Krankenhaus|` +
			`Medizinische[ \t]+Hochschule|Fachklinik|Reha-?Klinik|` +
			`Praxis[ \t]+(?:Dr\.|Prof\.|für[ \t]+\w+))[ \t]+[a-zäöüß][a-zäöüß-]+(?:[ \t]+[a-zäöüß][a-zäöüß-]+){0,2}`,
	)

	// Standalone famous German hospital names. Closed list; high precision.
	deOrgWellKnownRE = regexp.MustCompile(
		`(?i)\b(?:Charit[ée]|Vivantes|Asklepios|Helios|Sana|MediClin|` +
			`Schön[ \t]+Klinik|Rhön-?Klinikum|Diakonissenkrankenhaus)\b`,
	)
)

// NewDEOrganizationRecognizer detects German healthcare organisations.
// Emits the entity type "ORGANIZATION".
func NewDEOrganizationRecognizer() *PatternRecognizer {
	return NewPatternRecognizerWithContext(
		"DEOrganizationRecognizer",
		[]string{"ORGANIZATION"},
		[]string{"de"},
		[]namedPattern{
			{re: deOrgWellKnownRE, score: 0.90},
			{re: deOrgPrefixRE, score: 0.85},
			{re: deOrgSuffixRE, score: 0.80},
		},
		[]string{
			"klinik", "klinikum", "krankenhaus", "praxis", "abteilung",
			"station", "behandelnde", "einweisende",
		},
	)
}
