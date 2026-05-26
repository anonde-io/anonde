package recognizers

import "regexp"

// Multilingual street-address patterns for French, Italian, and Spanish.
// Mirrors the structural shape of de_street.go but for Romance languages.
//
// Pattern philosophy: high precision over high recall. We require an
// explicit street-type prefix (Rue, Via, Calle, …) followed by a
// capitalized name component, optionally followed by a house number.
// Bare numeric house numbers and bare place names without a street-type
// prefix are intentionally NOT caught here; those are an open-set NER
// problem and the FP risk on prose is too high.

var (
	// French: "Rue de la Paix", "Avenue Victor Hugo 12", "Boulevard Saint-Germain".
	// Word starts with capital (accented or not). Allow 1-3 name tokens after
	// the prefix to cover "Rue Louis Murville" and "Avenue du Général Leclerc".
	// Optional trailing house number (1-4 digits + optional letter, with or
	// without comma separator).
	frStreetRE = regexp.MustCompile(
		`\b(?:Rue|Avenue|Av\.|Boulevard|Bd\.|Bvd\.|Place|Pl\.|Allée|Allee|Impasse|Cours|Quai|Chemin|Route|Rte\.|Voie|Square|Sq\.)\s+` +
			`(?:de\s+la\s+|du\s+|de\s+|des\s+|le\s+|la\s+|les\s+|l['’])?` +
			`[A-ZÀ-ÖØ-Þ][\p{L}\-']{1,30}` +
			`(?:\s+[A-ZÀ-ÖØ-Þ][\p{L}\-']{1,30}){0,2}` +
			`(?:,?\s+\d{1,4}\s?[a-z]?)?\b`,
	)

	// Italian: "Via Roma 12", "Viale dei Mille", "Piazza San Marco 3", "Corso Italia".
	itStreetRE = regexp.MustCompile(
		`\b(?:Via|Viale|V\.le|Piazza|P\.zza|Corso|C\.so|Largo|Vicolo|Strada|Str\.|Piazzale|P\.le|Lungomare|Calle|Salita|Discesa)\s+` +
			`(?:dei\s+|delle\s+|della\s+|del\s+|dello\s+|degli\s+|delle\s+|di\s+|San\s+|Santa\s+|Sant['’])?` +
			`[A-ZÀ-ÖØ-Þ][\p{L}\-']{1,30}` +
			`(?:\s+[A-ZÀ-ÖØ-Þ][\p{L}\-']{1,30}){0,2}` +
			`(?:,?\s+\d{1,4}\s?[a-z]?)?\b`,
	)

	// Spanish: "Calle Mayor 23", "Avenida de la Constitución", "Plaza España",
	// "Carrera 7 #45-12" (Colombian), "Ruta Nacional 9", "Paseo del Prado".
	esStreetRE = regexp.MustCompile(
		`\b(?:Calle|C\/|Avenida|Av\.|Avda\.|Plaza|Pl\.|Paseo|Pso\.|Camino|Carrera|Cra\.|Carretera|Ctra\.|Ronda|Travesía|Travesia|Glorieta|Ruta)\s+` +
			`(?:de\s+la\s+|de\s+los\s+|de\s+las\s+|del\s+|de\s+|la\s+|el\s+|los\s+|las\s+)?` +
			`[A-ZÀ-ÖØ-Þ][\p{L}\-']{1,30}` +
			`(?:\s+[A-ZÀ-ÖØ-Þ][\p{L}\-']{1,30}){0,2}` +
			`(?:,?\s+\d{1,4}\s?[a-z]?)?\b`,
	)
)

// NewFRStreetRecognizer detects French-language street-address spans.
func NewFRStreetRecognizer() *PatternRecognizer {
	return NewPatternRecognizer(
		"FRStreetRecognizer",
		[]string{"STREET_ADDRESS"},
		[]string{"*"},
		[]namedPattern{{re: frStreetRE, score: 0.80}},
	)
}

// NewITStreetRecognizer detects Italian-language street-address spans.
func NewITStreetRecognizer() *PatternRecognizer {
	return NewPatternRecognizer(
		"ITStreetRecognizer",
		[]string{"STREET_ADDRESS"},
		[]string{"*"},
		[]namedPattern{{re: itStreetRE, score: 0.80}},
	)
}

// NewESStreetRecognizer detects Spanish-language street-address spans.
func NewESStreetRecognizer() *PatternRecognizer {
	return NewPatternRecognizer(
		"ESStreetRecognizer",
		[]string{"STREET_ADDRESS"},
		[]string{"*"},
		[]namedPattern{{re: esStreetRE, score: 0.80}},
	)
}
