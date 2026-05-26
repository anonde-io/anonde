package recognizers

import "regexp"

// English (US / UK) street-address patterns. Real ai4privacy_en gold
// surfaces this recognizer targets:
//
//	123 Main Street            <number> <name> <street-type>
//	N 5th Street               <direction> <ordinal> <street-type>
//	S Broadway                 <direction> <street-name>
//	Albany Road                <name> <street-type>
//	Apt. 259                   apartment / unit / suite markers
//	Wiza Spur                  <name> <type-tail>
//
// Three regexes capture the structural shapes; each emits STREET_ADDRESS,
// which the bench harness folds to LOCATION for the ai4privacy / MAPA
// corpora via --fold-parity-labels.

var (
	// Numbered street: "390 Wiza Spur", "123 Main Street", "1525 N Hampton Blvd".
	// The ai4privacy gold also splits the address into "390" and
	// "Wiza Spur" as two separate LOCATION spans on the surface
	// "390, Wiza Spur"; so the separator between the house number
	// and the street name accepts an optional comma. The full match
	// then overlaps both gold spans (the number AND the street).
	// Optional direction qualifier ("N", "S", "E", "W", "NE", "NW", "SE", "SW")
	// between the house number and the street name. Street-type list is
	// closed and case-tolerant on the final token.
	enStreetNumberedRE = regexp.MustCompile(
		`\b\d{1,5}` +
			`[,\s]+` +
			`(?:[NSEW]{1,2}\s+)?` +
			`[A-Z][A-Za-z]+(?:\s+[A-Z][A-Za-z]+){0,3}` +
			`\s+(?:` +
			`Street|St\.?|Avenue|Ave\.?|Boulevard|Blvd\.?|Road|Rd\.?|Drive|Dr\.?|` +
			`Lane|Ln\.?|Court|Ct\.?|Place|Pl\.?|Way|Spur|Trail|Pike|` +
			`Highway|Hwy\.?|Circle|Cir\.?|Plaza|Plz\.?|Terrace|Ter\.?|` +
			`Parkway|Pkwy\.?|Square|Sq\.?|Crescent|Cres\.?|Mews|Row|Walk|Close|Gardens|Heights|Park` +
			`)\b`,
	)

	// Directional + ordinal: "N 5th Street", "E 42nd Avenue".
	enStreetOrdinalRE = regexp.MustCompile(
		`\b[NSEW]{1,2}\s+\d{1,3}(?:st|nd|rd|th)` +
			`\s+(?:Street|St\.?|Avenue|Ave\.?|Boulevard|Blvd\.?|Road|Rd\.?|` +
			`Drive|Dr\.?|Lane|Ln\.?|Place|Pl\.?|Way)\b`,
	)

	// Bare street name with directional prefix: "S Broadway", "W Sunset".
	// The directional + Capitalized name shape is distinctive enough
	// (no number needed) to fire as a standalone catch.
	enStreetDirectionalNameRE = regexp.MustCompile(
		`\b[NSEW]{1,2}\s+[A-Z][A-Za-z]{3,30}(?:\s+[A-Z][A-Za-z]{3,30})?\b`,
	)

	// Apartment / unit / suite markers, optionally with the number on
	// the same line. "Apt. 259", "Apt 4B", "Suite 100", "Unit 12B".
	enApartmentRE = regexp.MustCompile(
		`\b(?:Apt\.?|Apartment|Suite|Ste\.?|Unit|Room|Rm\.?|Bldg\.?|Building|Floor|Fl\.?)\s+` +
			`[A-Z0-9][A-Z0-9-]{0,8}\b`,
	)
)

// NewENStreetRecognizer detects English (US / UK) street addresses.
// Emits STREET_ADDRESS, which the bench harness folds to LOCATION for
// corpora using the LOCATION bucket for street-level detail.
func NewENStreetRecognizer() *PatternRecognizer {
	return NewPatternRecognizerWithContext(
		"ENStreetRecognizer",
		[]string{"STREET_ADDRESS"},
		[]string{"en"},
		[]namedPattern{
			{re: enStreetNumberedRE, score: 0.85},
			{re: enStreetOrdinalRE, score: 0.88},
			{re: enStreetDirectionalNameRE, score: 0.72},
			{re: enApartmentRE, score: 0.85},
		},
		[]string{
			"address", "street", "ave", "avenue", "boulevard",
			"residence", "mailing", "shipping", "billing",
			"home", "office",
		},
	)
}
