package recognizers

import (
	"context"
	"regexp"
	"strings"

	"github.com/moogacs/anonde/analyzer"
)

// DEAnomalyRecognizer flags PERSON / ORG / LOC candidates using two
// complementary signals that don't need NER:
//
//   1. STRUCTURAL: a closed-vocabulary German title prefix immediately
//      followed by 1–4 capitalised words (e.g. "Frau Müller",
//      "Dr. med. Hans Meyer"). Extremely high precision.
//
//   2. STATISTICAL: multi-word capitalised sequences (≥2 tokens) whose
//      individual tokens are NOT in the embedded medical/common-German
//      vocabulary. The intuition: clinical text is dominated by medical
//      terminology; capitalised tokens that aren't medical are usually
//      proper nouns — names, places, hospitals.
//
// This recognizer is the "innovative" coverage stage: zero LLM cost,
// no external NER model, fires in microseconds. Combined with the
// existing regex / OAS-list recognizers it pushes leak rate on German
// clinical text down without requiring any inference at all.
//
// The vocabulary lists are deliberately *not* exhaustive — they're a
// hand-curated kernel covering the highest-frequency medical and
// general-German tokens. Customers extend via AnalysisConfig.AllowList
// at runtime as their domain reveals new terms.

// -----------------------------------------------------------------------------
// Patterns
// -----------------------------------------------------------------------------

// STRUCTURAL: title + 1–4 capitalised name tokens. Title list includes
// German salutations, medical doctor titles, "Patient:" / "Pat.:" prefixes.
// Capturing group 1 is the name; we emit only that span, not the title.
var deAnomalyTitledRE = regexp.MustCompile(
	`\b(?:Herr|Frau|Hr\.|Fr\.|Hrn\.|Frn\.|` +
		`Dr\.(?:\s*med\.)?|Prof\.(?:\s*Dr\.?)?|PD\s+Dr\.?|` +
		`Pat\.|Patient(?:in)?:?|der\s+Patient|die\s+Patientin)` +
		`\s+([A-ZÄÖÜ][a-zäöüß-]{1,30}(?:\s+[A-ZÄÖÜ][a-zäöüß-]{1,30}){0,3})\b`,
)

// STATISTICAL: ≥2 contiguous capitalised tokens (German letters + hyphens).
// Each token must be ≥2 chars to skip initials.
var deAnomalyMultiTokenRE = regexp.MustCompile(
	`\b[A-ZÄÖÜ][a-zäöüß-]{1,30}(?:\s+[A-ZÄÖÜ][a-zäöüß-]{1,30}){1,4}\b`,
)

// -----------------------------------------------------------------------------
// Embedded vocabulary
// -----------------------------------------------------------------------------

// deClinicalVocab is a kernel of German tokens that should NEVER be flagged
// as PII. Lowercased. Grouped here for readability; collapsed to a single
// set at package init.
var deClinicalVocab = [][]string{
	// Anatomy (top frequency)
	{
		"abdomen", "achillessehne", "arm", "arterien", "auge", "augen", "bauch",
		"becken", "bein", "blase", "blut", "brust", "brustkorb", "darm", "duodenum",
		"finger", "fuß", "galle", "gehirn", "gelenk", "gesäß", "gesicht", "hals",
		"hand", "haut", "herz", "hirn", "hüfte", "kehlkopf", "kiefer", "knie",
		"knochen", "kopf", "körper", "leber", "lunge", "lymphknoten", "magen",
		"milz", "muskel", "muskeln", "nase", "niere", "nieren", "ohr", "pankreas",
		"prostata", "rachen", "rippen", "rücken", "schädel", "schilddrüse",
		"schulter", "sehne", "thorax", "venen", "wirbel", "wirbelsäule", "zahn",
		"zähne", "zunge",
	},
	// Conditions, diagnoses
	{
		"abszess", "adenom", "allergie", "anämie", "aneurysma", "anfall", "angina",
		"arrhythmie", "arteriosklerose", "arthritis", "arthrose", "asthma", "ataxie",
		"asthenie", "asystolie", "bandscheibenvorfall", "blutung", "bronchitis",
		"diabetes", "diarrhoe", "dialyse", "demenz", "depression", "ekzem", "embolie",
		"entzündung", "enzephalitis", "epilepsie", "erbrechen", "erkrankung",
		"erschöpfung", "fieber", "fraktur", "fibrillation", "fibrom", "fistel",
		"gastritis", "gastroenteritis", "gicht", "glaukom", "grippe", "hepatitis",
		"herzinfarkt", "herzinsuffizienz", "hypertonie", "hypotonie", "hyperthyreose",
		"hypothyreose", "infektion", "infarkt", "ischämie", "karzinom", "katarakt",
		"kolitis", "kontusion", "kopfschmerz", "lipom", "leukämie", "lungenembolie",
		"lymphom", "melanom", "metastasen", "migräne", "morbus", "nephritis",
		"neuropathie", "ödem", "osteoporose", "otitis", "pankreatitis", "pneumonie",
		"polyarthritis", "polyp", "psoriasis", "pyelonephritis", "rhinitis",
		"sarkom", "schlaganfall", "schmerz", "schwindel", "sepsis", "shock",
		"sinusitis", "stenose", "stomatitis", "syndrom", "tachykardie", "thrombose",
		"tinnitus", "tumor", "ulkus", "varizen", "verbrennung", "vertigo", "wunde",
		"zyste", "ileus", "apoplex", "shunt", "fraktur",
	},
	// Procedures, imaging, lab
	{
		"ablation", "amputation", "anamnese", "anästhesie", "angiographie",
		"appendektomie", "applikation", "aufnahme", "ausschluss", "behandlung",
		"beatmung", "befund", "biopsie", "blutbild", "blutdruck", "blutgase",
		"blutuntersuchung", "computertomographie", "ct", "diagnose", "diagnostik",
		"differentialdiagnose", "echokardiographie", "eeg", "ekg", "endoskopie",
		"entlassung", "exstirpation", "exzision", "gastroskopie", "histologie",
		"hospitalisation", "infusion", "injektion", "inkontinenz", "intervention",
		"intubation", "intensivstation", "kontrast", "konsil", "koloskopie",
		"laborwerte", "laparoskopie", "laparotomie", "labor", "labordiagnostik",
		"mrt", "mri", "monitoring", "narkose", "neurologisch", "notfall",
		"operation", "pflege", "physiotherapie", "punktion", "radiologie",
		"rehabilitation", "rekonstruktion", "rektoskopie", "rehabilitation",
		"resektion", "röntgen", "sectio", "sonographie", "spiroergometrie",
		"spirometrie", "stationär", "stationsarzt", "szintigraphie", "therapie",
		"transfusion", "ultraschall", "untersuchung", "verlegung", "visite",
		"vorbefund",
	},
	// Drugs / pharmacology (high-frequency German clinical drug names + classes)
	{
		"amoxicillin", "aspirin", "antibiotikum", "antibiotika", "antikoagulanzien",
		"betablocker", "bisoprolol", "ceftriaxon", "ciprofloxacin", "clopidogrel",
		"cortison", "dexamethason", "diclofenac", "diuretikum", "diuretika",
		"enoxaparin", "furosemid", "heparin", "hydrochlorothiazid", "ibuprofen",
		"insulin", "kortison", "lasix", "levothyroxin", "lisinopril", "losartan",
		"marcumar", "metamizol", "metformin", "metoprolol", "morphin", "novalgin",
		"omeprazol", "paracetamol", "pantoprazol", "perfusor", "ramipril",
		"schmerzmittel", "simvastatin", "tramadol", "warfarin",
	},
	// Roles / professional titles
	{
		"arzt", "ärztin", "ärzte", "assistenzarzt", "chefarzt", "doktor", "doktorin",
		"facharzt", "facharztin", "krankenschwester", "oberarzt", "oberärztin",
		"pfleger", "pflegerin", "professor", "professorin", "schwester",
		"stationsarzt", "kollege", "kollegin", "kollegen", "kolleginnen",
	},
	// Calendar — full month / day names that pattern-match as capitalised
	{
		"januar", "februar", "märz", "maerz", "april", "mai", "juni", "juli",
		"august", "september", "oktober", "november", "dezember",
		"montag", "dienstag", "mittwoch", "donnerstag", "freitag", "samstag", "sonntag",
	},
	// Common German nouns that appear capitalised in clinical text
	{
		"abteilung", "absatz", "abschnitt", "alter", "anamnese", "anhang",
		"anlage", "anschrift", "art", "auftrag", "ausbildung", "befund", "behandlung",
		"beschwerden", "bericht", "betreff", "beurteilung", "blut", "datum", "diagnose",
		"diagnostik", "dokument", "ergebnis", "ergebnisse", "fall", "familie",
		"funktion", "geburt", "geschlecht", "gesundheit", "größe", "grund",
		"gewicht", "hause", "informationen", "jahr", "jahre", "kontrolle", "körper",
		"krankheit", "kurz", "labor", "lage", "leistung", "medikamente", "monat",
		"name", "namen", "nummer", "patient", "patientin", "pflege", "phase",
		"problem", "qualität", "quelle", "raum", "rückkehr", "schmerzen", "schule",
		"sicht", "situation", "station", "stunde", "tag", "tage", "termin", "test",
		"therapie", "thema", "uhr", "untersuchung", "ursache", "verlauf", "vertrag",
		"verwaltung", "vorbefund", "vorgeschichte", "vorstellung", "vortrag", "weg",
		"woche", "wochen", "zeit", "zentrum", "ziel", "zustand", "zustellung",
	},
	// Common German words frequently capitalised (mid-sentence or in headers)
	{
		"abklärung", "abschluss", "akut", "akute", "akuter", "akutes", "all",
		"allgemein", "allgemeine", "allgemeiner", "art", "auch", "auf", "aus",
		"bei", "beide", "beim", "blutdruck", "chronisch", "chronische", "chronischer",
		"chronisches", "danach", "dann", "darauf", "darin", "darüber", "davon",
		"deutsch", "diese", "dieser", "dieses", "diesem", "doch", "durch", "eine",
		"einer", "einem", "einen", "einige", "einleitung", "entlassbrief",
		"entlassung", "ergebnis", "erst", "erste", "ersten", "etwa", "etwas",
		"folge", "folgenden", "ggf", "gut", "gute", "hier", "hierbei", "hinblick",
		"im", "in", "inkl", "insgesamt", "international", "intra", "intramural",
		"jetzt", "jeden", "jeder", "jedes", "kein", "keine", "klar", "klinisch",
		"klinische", "klinischer", "klinisches", "kontakt", "kontinuierlich",
		"kurz", "kurze", "länger", "mal", "mehr", "mehrere", "mit", "möglich",
		"nach", "nachgewiesen", "nahezu", "name", "nebenbefundlich", "neben",
		"nicht", "noch", "nun", "nur", "ob", "ohne", "oder", "patient", "rein",
		"rezent", "schon", "sehr", "selbst", "sicher", "sie", "sonstige", "sowohl",
		"später", "stark", "statisch", "status", "stationär", "such", "tag",
		"unauffällig", "und", "unklar", "unter", "viele", "vor", "vorher",
		"vorgehen", "wegen", "weil", "weiter", "weitere", "weiterer", "weiteres",
		"weiterhin", "welche", "wenig", "weniger", "wenn", "wieder", "wie",
		"wir", "wir", "wurde", "während", "über", "überwiegend", "zu", "zudem",
		"zunächst", "zur", "zusätzlich", "zwischen",
	},
	// Top-frequency clinical adjectives appearing capitalised
	{
		"akut", "akute", "akuter", "anhaltend", "ausgeprägt", "ausgeprägter",
		"chronisch", "chronische", "chronischer", "deutlich", "deutliche",
		"deutlicher", "entzündlich", "gering", "geringer", "geringfügig",
		"hochgradig", "klein", "kleiner", "leicht", "leichte", "leichter",
		"maligne", "malignes", "moderat", "moderate", "moderater", "mäßig",
		"mäßige", "mäßiger", "mild", "milde", "milder", "negativ", "normal",
		"normale", "normaler", "normwertig", "ohne", "pathologisch", "pathologische",
		"physiologisch", "physiologische", "positiv", "primär", "primäre",
		"primärer", "schwer", "schwere", "schwerer", "sekundär", "sekundäre",
		"sekundärer", "stark", "starke", "starker", "subakut", "subakute",
		"subakuter", "umschrieben", "unauffällig", "unauffällige", "unauffälliger",
		"vereinzelt", "vermindert",
	},
	// Doc-structure / letter words
	{
		"anrede", "betreff", "betr", "geehrte", "geehrter", "gruß", "mfg",
		"hochachtungsvoll", "freundlich", "freundliche", "freundlichen", "kollegen",
		"kolleginnen", "kollege", "kollegin", "sehr", "verehrte", "verehrter",
		"über", "kollegial", "kollegialer", "viele", "grüße",
	},
	// German articles, pronouns, numbers, and other very-high-frequency
	// closed-class words that frequently appear capitalised at sentence
	// start. These would generate enormous FP volume if not excluded.
	{
		"der", "die", "das", "den", "dem", "des",
		"ein", "eine", "eines", "einer", "einem", "einen",
		"kein", "keine", "keiner", "keinem", "keinen",
		"mein", "meine", "meiner", "meinem", "meinen", "meines",
		"dein", "deine", "deiner", "deinem", "deinen", "deines",
		"sein", "seine", "seiner", "seinem", "seinen", "seines",
		"ihr", "ihre", "ihrer", "ihrem", "ihren", "ihres",
		"unser", "unsere", "unseren", "unserem",
		"euer", "eure", "eurer", "eurem", "euren",
		"ich", "du", "er", "es", "wir", "sie",
		"mich", "dich", "ihn", "uns", "euch",
		"mir", "dir", "ihm", "ihnen",
		"null", "eins", "zwei", "drei", "vier", "fünf", "sechs", "sieben",
		"acht", "neun", "zehn", "elf", "zwölf",
		"aber", "sondern", "denn", "weil", "dass", "wenn", "als", "ob",
	},
}

var deAnomalyDenySet = func() map[string]struct{} {
	out := make(map[string]struct{}, 2000)
	for _, group := range deClinicalVocab {
		for _, w := range group {
			out[strings.ToLower(w)] = struct{}{}
		}
	}
	return out
}()

// -----------------------------------------------------------------------------
// Recognizer
// -----------------------------------------------------------------------------

// DEAnomalyRecognizer implements the title-extraction + statistical-anomaly
// detector described above. Stateless, no I/O.
type DEAnomalyRecognizer struct{}

// NewDEAnomalyRecognizer constructs the recognizer.
func NewDEAnomalyRecognizer() *DEAnomalyRecognizer { return &DEAnomalyRecognizer{} }

// Name returns the recognizer name for logs and conflict resolution.
func (r *DEAnomalyRecognizer) Name() string { return "DEAnomalyRecognizer" }

// SupportedEntities returns the entity types this recognizer can emit.
// We emit PERSON as the most common case for unknown capitalised tokens
// in clinical text. Recognizers downstream (Place, Org) override with
// their more specific types via conflict resolution when applicable.
func (r *DEAnomalyRecognizer) SupportedEntities() []string { return []string{"PERSON"} }

// SupportedLanguages returns the languages this recognizer applies to.
func (r *DEAnomalyRecognizer) SupportedLanguages() []string { return []string{"de"} }

// Analyze scans text for title-prefixed names and anomalous capitalised
// sequences. Header (first 600 chars) and footer (last 400 chars) get a
// modest score boost — most identifying PII clusters in those positions
// in German clinical letters.
func (r *DEAnomalyRecognizer) Analyze(_ context.Context, text string, _ []string, _ string) ([]analyzer.RecognizerResult, error) {
	if text == "" {
		return nil, nil
	}
	n := len(text)
	headerEnd := 600
	if headerEnd > n {
		headerEnd = n
	}
	footerStart := n - 400
	if footerStart < 0 {
		footerStart = 0
	}

	emitted := make(map[[2]int]struct{}, 32)
	var out []analyzer.RecognizerResult

	emit := func(start, end int, score float64) {
		key := [2]int{start, end}
		if _, dup := emitted[key]; dup {
			return
		}
		emitted[key] = struct{}{}
		// Header/footer positional boost.
		if start < headerEnd || start >= footerStart {
			score += 0.05
		}
		out = append(out, analyzer.RecognizerResult{
			Start:          start,
			End:            end,
			Score:          score,
			EntityType:     "PERSON",
			RecognizerName: r.Name(),
		})
	}

	// 1. Title + name patterns (high-precision).
	for _, m := range deAnomalyTitledRE.FindAllStringSubmatchIndex(text, -1) {
		if len(m) < 4 || m[2] < 0 {
			continue
		}
		emit(m[2], m[3], 0.80)
	}

	// 2. Multi-token capitalised sequences whose tokens are not all in the
	// medical/common vocabulary.
	for _, m := range deAnomalyMultiTokenRE.FindAllStringIndex(text, -1) {
		seq := text[m[0]:m[1]]
		if r.allInDenyList(seq) {
			continue
		}
		emit(m[0], m[1], 0.60)
	}

	return out, nil
}

// allInDenyList returns true if every token of a multi-token capitalised
// sequence is in the medical/common-German vocabulary. Used to skip
// sequences like "Akute Bronchitis" that are pure medical phrasing.
func (r *DEAnomalyRecognizer) allInDenyList(seq string) bool {
	tokens := strings.Fields(seq)
	if len(tokens) == 0 {
		return true
	}
	for _, t := range tokens {
		lower := strings.ToLower(strings.TrimFunc(t, func(r rune) bool {
			return !unicodeIsLetter(r)
		}))
		if lower == "" {
			continue
		}
		if _, ok := deAnomalyDenySet[lower]; !ok {
			return false
		}
	}
	return true
}

// unicodeIsLetter is a tiny helper to avoid importing unicode just for one
// predicate. Covers ASCII letters + German umlauts + ß.
func unicodeIsLetter(r rune) bool {
	switch {
	case r >= 'a' && r <= 'z':
		return true
	case r >= 'A' && r <= 'Z':
		return true
	case r == 'ä' || r == 'ö' || r == 'ü' || r == 'ß':
		return true
	case r == 'Ä' || r == 'Ö' || r == 'Ü':
		return true
	}
	return false
}
