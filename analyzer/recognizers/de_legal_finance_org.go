package recognizers

import "regexp"

// German legal, financial, and government organisation patterns. The
// healthcare-org recognizer in de_organization.go was scoped to clinical
// text; this file picks up the rest of the German institutional vocabulary
// that shows up in finance / legal / public-sector documents.
//
// Why patterns and not just NER: GLiNER PII base reliably tags
// "Amtsgericht Bremen" or "Sparkasse Köln-Bonn" as PERSON, not
// ORGANIZATION. Conflict resolution then picks the right span but the
// wrong type, and type-aware leak scoring counts the span as missed. Each
// pattern below is anchored on an institutional head-word (Gericht, Bank,
// Amt, Kanzlei, …) followed by a bounded capitalised tail, so the FP risk
// on free prose is low.

var (
	// Courts. <CourtType> [<City>]; city tail is one to three capitalised
	// tokens, each optionally hyphenated to a second capitalised token
	// ("Frankfurt-Höchst"). The inner `[a-zäöüß]+` deliberately omits `-`
	// so the explicit `(?:-[A-ZÄÖÜ][a-zäöüß]+)?` continuation can match,
	// otherwise the inner class greedy-consumes the hyphen and the
	// hyphenated tail is dropped.
	deOrgCourtRE = regexp.MustCompile(
		`\b(?:` +
			`Amtsgericht|Landgericht|Oberlandesgericht|Bundesgerichtshof|BGH|` +
			`Arbeitsgericht|Landesarbeitsgericht|Bundesarbeitsgericht|BAG|` +
			`Sozialgericht|Landessozialgericht|Bundessozialgericht|BSG|` +
			`Finanzgericht|Bundesfinanzhof|BFH|` +
			`Verwaltungsgericht|Oberverwaltungsgericht|Bundesverwaltungsgericht|BVerwG|` +
			`Bundesverfassungsgericht|BVerfG|Bundespatentgericht` +
			`)` +
			`(?:[ \t]+[A-ZÄÖÜ][a-zäöüß]+(?:-[A-ZÄÖÜ][a-zäöüß]+)?){0,3}\b`,
	)

	// Banks. Either a famous standalone (KfW, HypoVereinsbank, …) OR a
	// generic German bank head (Sparkasse / Volksbank / Raiffeisenbank /
	// Postbank / Commerzbank / Deutsche Bank / …) optionally followed by a
	// place. "Volksbank Bonn Rhein-Sieg eG" needs the place tail to grab
	// the full institution name, including the trailing legal-form suffix.
	// The big-bank list includes the abbreviations that show up on
	// statement letterheads (LBBW, BayernLB, NordLB, HSH Nordbank, …) plus
	// the Privatbanken (Berenberg, Warburg, Metzler, Hauck Aufhäuser).
	deOrgBankRE = regexp.MustCompile(
		`\b(?:` +
			`KfW(?:[ \t]+Bankengruppe)?|HypoVereinsbank|HypoRealEstate|` +
			`Deutsche[ \t]+Bank|Commerzbank|Postbank|DKB|ING-DiBa|ING|` +
			`Targobank|N26|DZ[ \t]+Bank|Apobank|BBBank|PSD[ \t]+Bank|` +
			`LBBW|BayernLB|NordLB|HSH[ \t]+Nordbank|Helaba|Landesbank[ \t]+[A-ZÄÖÜ][a-zäöüß-]+|` +
			`DekaBank|Berenberg|M\.M\.[ \t]+Warburg(?:[ \t]+&[ \t]+Co\.?)?|Metzler|` +
			`Hauck[ \t]+Aufh[äa]user|` +
			`(?:Sparkasse|Volksbank|Raiffeisenbank|VR-Bank|Genobank|PSD[ \t]+Bank|Sparda-?Bank)` +
			`(?:[- ][A-ZÄÖÜ][a-zäöüß]+(?:-[A-ZÄÖÜ][a-zäöüß]+)?(?:[ \t]+[A-ZÄÖÜ][a-zäöüß]+(?:-[A-ZÄÖÜ][a-zäöüß]+)?){0,2})?` +
			`)(?:[ \t]+eG)?\b`,
	)

	// Government agencies. "Finanzamt Köln", "Bundesamt für Migration",
	// "Landesamt für Verfassungsschutz", etc. The optional "für/des/der + X"
	// tail captures the subject-of-jurisdiction common in agency names
	// (including conjoined subjects with "und": "Migration und Flüchtlinge").
	deOrgGovtRE = regexp.MustCompile(
		`\b(?:` +
			`Bundesamt|Landesamt|Kreisamt|Stadtamt|` +
			`Finanzamt|Hauptzollamt|Zollamt|` +
			`Sozialamt|Jugendamt|Ordnungsamt|Standesamt|Gewerbeamt|Einwohnermeldeamt|Gesundheitsamt|` +
			`Polizeipr[äa]sidium|Polizeidirektion|Polizeiinspektion|Polizeirevier|` +
			`Staatsanwaltschaft|Generalstaatsanwaltschaft|` +
			`Ministerium|Bundesministerium|Landesministerium|` +
			`Beh[öo]rde|Bundesbeh[öo]rde|Landesbeh[öo]rde` +
			`)` +
			`(?:[ \t]+(?:f[üu]r|der|des|von)[ \t]+[A-ZÄÖÜ][a-zäöüß]+(?:[ \t]+und[ \t]+[A-ZÄÖÜ][a-zäöüß]+)?)?` +
			`(?:[ \t]+[A-ZÄÖÜ][a-zäöüß]+(?:-[A-ZÄÖÜ][a-zäöüß]+)?){0,2}\b`,
	)

	// Law firms. "Kanzlei <Name>"; name is one or more capitalised tokens,
	// optionally joined by "&" or ", ". Closes on "& Kollegen" / "& Partner"
	// / line end.
	deOrgKanzleiRE = regexp.MustCompile(
		`\b(?:Kanzlei|Rechtsanwaltskanzlei|Notariat|Sozietät|Anwaltskanzlei)` +
			`[ \t]+[A-ZÄÖÜ][a-zäöüß]+` +
			`(?:[ \t]*[&,][ \t]*[A-ZÄÖÜ][a-zäöüß]+){0,4}` +
			`(?:[ \t]*&[ \t]*(?:Kollegen|Partner|Partnerschaft))?\b`,
	)

	// Companies with a German legal-form suffix. Tail-anchored: scan
	// backward from the suffix to grab a bounded run of capitalised tokens
	// (1-6 tokens max). Avoids consuming whole paragraphs that happen to
	// end in "GmbH". The leading token must start a word boundary AND begin
	// with a capital, ruling out lowercase-tail matches like "irgendeine
	// GmbH" that aren't actually proper-noun company names.
	deOrgCompanyRE = regexp.MustCompile(
		`\b(?:Firma[ \t]+)?` +
			`[A-ZÄÖÜ][A-Za-zäöüß0-9.&'-]+` +
			`(?:[ \t]+(?:&[ \t]+)?[A-ZÄÖÜ][A-Za-zäöüß0-9.&'-]+){0,5}` +
			`[ \t]+` +
			`(?:GmbH(?:[ \t]+&[ \t]+Co\.?[ \t]+(?:KG|KGaA|OHG))?|` +
			`AG|KGaA|KG|OHG|oHG|e\.?V\.?|e\.?K\.?|SE|UG(?:[ \t]*\(haftungsbeschr[äa]nkt\))?|` +
			`PartG(?:mbB)?|Stiftung(?:[ \t]+b[üu]rgerlichen[ \t]+Rechts)?)\b`,
	)
)

// NewDELegalFinanceOrgRecognizer detects German legal / financial /
// government / corporate organisation names. Complements
// DEOrganizationRecognizer (which is healthcare-only). Emits ORGANIZATION.
func NewDELegalFinanceOrgRecognizer() *PatternRecognizer {
	return NewPatternRecognizerWithContext(
		"DELegalFinanceOrgRecognizer",
		[]string{"ORGANIZATION"},
		[]string{"de"},
		[]namedPattern{
			{re: deOrgCourtRE, score: 0.90},
			{re: deOrgGovtRE, score: 0.88},
			{re: deOrgBankRE, score: 0.88},
			{re: deOrgKanzleiRE, score: 0.85},
			{re: deOrgCompanyRE, score: 0.82},
		},
		[]string{
			"gericht", "kanzlei", "rechtsanwalt", "notar", "richter", "kläger", "beklagter",
			"bank", "sparkasse", "iban", "konto", "kontoauszug", "kundennummer",
			"amt", "behörde", "ministerium", "verfügung", "bescheid",
			"firma", "gesellschaft", "geschäft",
		},
	)
}
