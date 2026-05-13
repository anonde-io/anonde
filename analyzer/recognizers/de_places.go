package recognizers

import (
	"context"
	"regexp"
	"strings"

	"github.com/anonde-io/anonde/analyzer"
)

// DEPlaceRecognizer matches a closed-vocabulary list of countries,
// German/Austrian/Swiss states, and major DACH cities against the input
// text. Closed-vocabulary lookup is ~1000× cheaper than NER and 100%
// precise on exact matches.
//
// Score tiering:
//   - Tier 1 (countries, states):       0.85, no context needed
//   - Tier 2 (cities):                  0.65, context boost helps; many
//                                       German city names overlap with
//                                       surnames (Frankfurt, Hannover…).
//
// The list is curated for German clinical text. Customers can extend it
// at runtime via AnalysisConfig.DenyList for org-specific place names.

// Countries — German names. Closed list, ~unambiguous in clinical text.
var dePlaceCountries = makeSet(
	"Deutschland", "Österreich", "Schweiz", "Liechtenstein", "Luxemburg",
	"Frankreich", "Italien", "Spanien", "Portugal", "Niederlande", "Belgien",
	"Dänemark", "Schweden", "Norwegen", "Finnland", "Island", "Irland",
	"Großbritannien", "Vereinigtes Königreich", "Polen", "Tschechien",
	"Slowakei", "Ungarn", "Slowenien", "Kroatien", "Serbien", "Bosnien",
	"Montenegro", "Albanien", "Kosovo", "Nordmazedonien", "Bulgarien",
	"Rumänien", "Griechenland", "Türkei", "Zypern", "Malta", "Estland",
	"Lettland", "Litauen", "Russland", "Ukraine", "Belarus", "Moldau",
	"Georgien", "Armenien", "Aserbaidschan", "Kasachstan", "Usbekistan",
	"USA", "Vereinigte Staaten", "Kanada", "Mexiko", "Brasilien", "Argentinien",
	"Chile", "Peru", "Kolumbien", "Venezuela", "Kuba",
	"China", "Japan", "Indien", "Pakistan", "Bangladesch", "Indonesien",
	"Vietnam", "Thailand", "Philippinen", "Südkorea", "Nordkorea", "Taiwan",
	"Israel", "Iran", "Irak", "Syrien", "Libanon", "Jordanien",
	"Saudi-Arabien", "Vereinigte Arabische Emirate", "Ägypten", "Marokko",
	"Tunesien", "Algerien", "Libyen", "Südafrika", "Nigeria", "Kenia",
	"Äthiopien", "Ghana", "Senegal", "Sudan",
	"Australien", "Neuseeland",
)

// States — German Bundesländer + Austrian states + Swiss cantons.
var dePlaceStates = makeSet(
	// Germany
	"Baden-Württemberg", "Bayern", "Berlin", "Brandenburg", "Bremen",
	"Hamburg", "Hessen", "Mecklenburg-Vorpommern", "Niedersachsen",
	"Nordrhein-Westfalen", "Rheinland-Pfalz", "Saarland", "Sachsen",
	"Sachsen-Anhalt", "Schleswig-Holstein", "Thüringen",
	// Austria
	"Burgenland", "Kärnten", "Niederösterreich", "Oberösterreich",
	"Salzburg", "Steiermark", "Tirol", "Vorarlberg", "Wien",
	// Swiss cantons (selected)
	"Zürich", "Bern", "Luzern", "Uri", "Schwyz", "Obwalden", "Nidwalden",
	"Glarus", "Zug", "Freiburg", "Solothurn", "Basel-Stadt", "Basel-Landschaft",
	"Schaffhausen", "Appenzell Ausserrhoden", "Appenzell Innerrhoden",
	"St. Gallen", "Graubünden", "Aargau", "Thurgau", "Tessin", "Waadt",
	"Wallis", "Neuenburg", "Genf", "Jura",
)

// Cities — top German/Austrian/Swiss cities by population, plus a
// selection of medium cities common in clinical text. Lower score (0.65)
// because some overlap with German surnames.
var dePlaceCities = makeSet(
	// Germany — top 60+
	"Berlin", "Hamburg", "München", "Köln", "Frankfurt", "Stuttgart",
	"Düsseldorf", "Leipzig", "Dortmund", "Essen", "Bremen", "Dresden",
	"Hannover", "Nürnberg", "Duisburg", "Bochum", "Wuppertal", "Bielefeld",
	"Bonn", "Mannheim", "Karlsruhe", "Augsburg", "Wiesbaden", "Mönchengladbach",
	"Gelsenkirchen", "Braunschweig", "Chemnitz", "Kiel", "Aachen", "Magdeburg",
	"Halle", "Freiburg", "Krefeld", "Lübeck", "Oberhausen", "Erfurt",
	"Mainz", "Rostock", "Kassel", "Hagen", "Saarbrücken", "Mülheim",
	"Potsdam", "Ludwigshafen", "Oldenburg", "Leverkusen", "Osnabrück",
	"Solingen", "Heidelberg", "Herne", "Neuss", "Darmstadt", "Paderborn",
	"Regensburg", "Ingolstadt", "Würzburg", "Fürth", "Wolfsburg", "Offenbach",
	"Ulm", "Heilbronn", "Pforzheim", "Göttingen", "Bottrop", "Trier",
	"Recklinghausen", "Reutlingen", "Bremerhaven", "Koblenz", "Bergisch Gladbach",
	"Jena", "Remscheid", "Erlangen", "Moers", "Siegen", "Hildesheim",
	"Salzgitter", "Flensburg", "Cottbus", "Schwerin", "Tübingen",
	"Lüneburg", "Konstanz", "Marburg", "Gießen",
	// Austria
	"Wien", "Graz", "Linz", "Salzburg", "Innsbruck", "Klagenfurt", "Villach",
	"Wels", "Sankt Pölten", "Dornbirn", "Steyr", "Wiener Neustadt",
	"Feldkirch", "Bregenz",
	// Switzerland
	"Zürich", "Genf", "Basel", "Lausanne", "Bern", "Winterthur", "Luzern",
	"St. Gallen", "Lugano", "Biel", "Thun", "Köniz", "La Chaux-de-Fonds",
	"Schaffhausen", "Fribourg", "Chur",
)

func makeSet(words ...string) map[string]struct{} {
	m := make(map[string]struct{}, len(words))
	for _, w := range words {
		m[strings.ToLower(w)] = struct{}{}
	}
	return m
}

// Word matcher: pulls capitalised tokens (1–4 words), respecting hyphens
// and German letters. We need word-boundary matches but Go regex's `\w`
// is ASCII-only; build the matcher manually.
var dePlaceCandidateRE = regexp.MustCompile(
	`\b[A-ZÄÖÜ][a-zäöüß-]+(?:[ -][A-ZÄÖÜ][a-zäöüß-]+){0,3}\b`,
)

// DEPlaceRecognizer matches against the closed lists above.
type DEPlaceRecognizer struct{}

// NewDEPlaceRecognizer constructs the recognizer.
func NewDEPlaceRecognizer() *DEPlaceRecognizer { return &DEPlaceRecognizer{} }

// Name returns the recognizer name for logs and conflict resolution.
func (r *DEPlaceRecognizer) Name() string { return "DEPlaceRecognizer" }

// SupportedEntities returns the entity types this recognizer emits.
func (r *DEPlaceRecognizer) SupportedEntities() []string { return []string{"LOCATION"} }

// SupportedLanguages returns the languages this recognizer applies to.
func (r *DEPlaceRecognizer) SupportedLanguages() []string { return []string{"de"} }

// Analyze scans text for known countries / states / cities.
func (r *DEPlaceRecognizer) Analyze(_ context.Context, text string, _ []string, _ string) ([]analyzer.RecognizerResult, error) {
	if text == "" {
		return nil, nil
	}
	var out []analyzer.RecognizerResult
	for _, m := range dePlaceCandidateRE.FindAllStringIndex(text, -1) {
		surface := text[m[0]:m[1]]
		lower := strings.ToLower(surface)
		switch {
		case in(dePlaceCountries, lower):
			out = append(out, analyzer.RecognizerResult{
				Start: m[0], End: m[1], Score: 0.85,
				EntityType: "LOCATION", RecognizerName: r.Name(),
			})
		case in(dePlaceStates, lower):
			out = append(out, analyzer.RecognizerResult{
				Start: m[0], End: m[1], Score: 0.85,
				EntityType: "LOCATION", RecognizerName: r.Name(),
			})
		case in(dePlaceCities, lower):
			out = append(out, analyzer.RecognizerResult{
				Start: m[0], End: m[1], Score: 0.65,
				EntityType: "LOCATION", RecognizerName: r.Name(),
			})
		}
	}
	return out, nil
}

func in(m map[string]struct{}, k string) bool {
	_, ok := m[k]
	return ok
}
