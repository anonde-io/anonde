package recognizers

import "regexp"

// German street-address patterns. Real GraSCCo examples:
//
//	Friesische Str. 21 a       adjective + Str. + number (+ letter)
//	Rote Str. 3                adjective + Str. + number
//	Kantstraße. 21 a           compound + straße + number
//	Gartenpfad 44              compound + pfad + number
//	Sauerbruchplatz 8          compound + platz + number
//	Hauptstr. 8                compound + str. + number
//	Florgasse 2                compound + gasse + number
//	Korekamp 15                compound + kamp + number
//	Am Hasenstall              Am + capitalized place (often no number)
//
// Two regexes capture the structural shapes; emitting at LOCATION matches
// the gold's LOCATION_STREET canonical label.

// Two regex shapes, tuned for high precision on clinical text:
//
//  1. Compound (single word) — only high-specificity lowercase suffixes
//     (-straße, -str., -pfad, -gasse, -allee, -platz, -kamp). Lowercase
//     "-weg", "-gang", "-ring", "-steig" are deliberately omitted because
//     they collide with common clinical words ("Vorgang", "Anstieg",
//     "Ohrring", "Vorweg").
//
//  2. Separated (with whitespace) — any capitalized street suffix
//     including Str., Weg, Ring, etc. The space-then-Capital form rarely
//     occurs accidentally in clinical prose.
//
// The preposition form ("Am Bauch", "Im Bett") is deliberately excluded
// because it fires constantly in clinical text on anatomical and
// procedural references.
var (
	// Compound, single-token: Hauptstr., Florgasse, Sauerbruchplatz, ...
	deStreetCompoundRE = regexp.MustCompile(
		`\b[A-ZÄÖÜ][a-zäöüß-]{2,30}` + // stem ≥3 chars to reduce noise
			`(?:stra(?:ß|ss)e|str\.|pfad|gasse|allee|platz|kamp)` +
			`(?:\s+\d{1,4}\s?[a-z]?)?\b`,
	)

	// Separated, two-token: "Friesische Str. 21", "Rote Allee 5", etc.
	deStreetSeparatedRE = regexp.MustCompile(
		`\b[A-ZÄÖÜ][a-zäöüß-]{2,30}\s+` +
			`(?:Stra(?:ß|ss)e|Str\.|Weg|Platz|Pfad|Gasse|Kamp|Allee|Ring|Promenade|Steig|Gang|Chaussee)` +
			`\.?` +
			`(?:\s+\d{1,4}\s?[a-z]?)?\b`,
	)
)

// NewDEStreetRecognizer detects German street-address spans.
// Emits STREET_ADDRESS — distinct from LOCATION so downstream operators
// can redact street + number while preserving city information.
func NewDEStreetRecognizer() *PatternRecognizer {
	return NewPatternRecognizerWithContext(
		"DEStreetRecognizer",
		[]string{"STREET_ADDRESS"},
		[]string{"de"},
		[]namedPattern{
			{re: deStreetCompoundRE, score: 0.80},
			{re: deStreetSeparatedRE, score: 0.85},
		},
		[]string{
			"anschrift", "adresse", "wohnhaft", "wohnort",
			"hausarzt", "patient",
		},
	)
}
