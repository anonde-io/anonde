package recognizers

import (
	"context"
	"regexp"

	"github.com/anonde-io/anonde/analyzer"
)

// DEClinicalIDRecognizer detects German clinical identifiers. GraSCCo shows
// three shapes that account for nearly all gold ID spans:
//
//  1. Keyword-anchored long alphanumerics:
//     Fallnummer: 23346011        FN: 445544767
//     Fall-Nr. 6733340001         E-Nr.: 17217277
//     SV Nr.: 4445311299          Patient-ID: A-202344102
//     Fallzahl: 103354008         Fall: 102341651622
//
//  2. Station / ward / room identifiers:
//     Station A33                 Onkologie-Ambulanz 3
//     chirurgischen Ambulanz CH12 Intensivstation I03
//     Station 4A                  Station O-11           OP II
//
//  3. Histology / lab specimen codes (slash-separated):
//     Histologie (H25440/51)
//
// Patterns ordered most-specific first; the analyzer's conflict resolver
// keeps higher-scored matches when they overlap. Standalone pure-numeric
// IDs without an anchoring keyword are intentionally NOT emitted — too
// many false positives on lab values, dosages, and document numbers in
// unanchored positions.

var (
	// Keyword-anchored IDs. Capturing group 1 is the value.
	// Value shape: 0-4 leading uppercase letters (+ optional hyphen) then
	// a digit, then 1-14 more alphanumeric / hyphen / slash chars. The
	// 0-4 leading-letter range covers bare numerics ("23346011"), single
	// letter ("A-202344102"), two letters ("KL699820", "HN999999"), and
	// three letters ("PAT-202344102").
	deClinicalIDKeywordRE = regexp.MustCompile(
		`\b(?:` +
			// German clinical keywords
			`Fall(?:[-\s]?(?:Nr|nummer|zahl))|` +
			`FN|` +
			`E[-\s]?Nr|` +
			`SV[-\s]?Nr|` +
			`MRN|` +
			`Versicherten(?:[-\s]?nummer|[-\s]?nr)|` +
			`Krankenversicherten(?:[-\s]?nummer|[-\s]?nr)|` +
			`Akten(?:zeichen|nummer)|` +
			`Gesch[äa]ftszeichen|` +
			`Patient(?:en)?[-\s]?(?:Nr|nummer|ID)|` +
			`Pat\.[-\s]?ID|` +
			`Aufnahme(?:[-\s]?Nr|[-\s]?nummer)|` +
			`Versicherungs(?:[-\s]?Nr|[-\s]?nummer)|` +
			`Bericht[-\s]?Nr|` +
			`Auftrag(?:s[-\s]?Nr)?|` +
			`ID(?:[-\s]?(?:Nr|nummer))?|` +
			`Fall|` +
			// German finance / banking keywords. "KD-6556039" after
			// "Kundennummer:" was a missed-ID family across finance_de.
			`Kunden(?:[-\s]?(?:nummer|Nr))|` +
			`Kontonummer|` +
			`Konto[-\s]?Nr|` +
			`Kontoauszug(?:[-\s]?Nr)?|` +
			`Vertrag(?:s[-\s]?Nr|s[-\s]?nummer)|` +
			`Rechnung(?:s[-\s]?Nr|s[-\s]?nummer)|` +
			`Bestell(?:[-\s]?(?:nummer|Nr))|` +
			// German legal IDs. "Az.:" / "Aktenzeichen" is the universal
			// abbreviation for court case numbers. "RA-NR" /
			// "Rechtsanwaltsnummer" is the German bar-registry number
			// printed on lawyer letterheads.
			`Az\.?|` +
			`RA[-\s]?NR|` +
			`Rechtsanwalt(?:s)?[-\s]?(?:nummer|Nr)|` +
			`Notar(?:s)?[-\s]?(?:nummer|Nr)|` +
			// English keywords — same shape, different vocabulary. "MRN" is
			// already covered above (universal abbreviation); these add the
			// long forms common in US/UK clinical documents.
			`Medical[ \t]+Record(?:[ \t]*(?:Number|No\.?|#))?|` +
			`Med[ \t]+Rec(?:ord)?(?:[ \t]*(?:Number|No\.?|#))?|` +
			`Account(?:[ \t]*(?:Number|No\.?|#))?|` +
			`Customer(?:[ \t]*(?:Number|No\.?|#|ID))?|` +
			`Invoice(?:[ \t]*(?:Number|No\.?|#))?|` +
			`Encounter(?:[ \t]*(?:Number|No\.?|#|ID))?` +
			`)` +
			// Any sequence of separator chars (whitespace, ., :, ;, tab)
			// — order-agnostic so we match "Nr.: ", "Nr: ", " :", "\t", "-Nr.\t", etc.
			`[\s.:;,\t]*` +
			// Value: 0-4 leading letters, optional hyphen, then digit
			// + alphanumeric/hyphen tail. Slash is INTENTIONALLY excluded
			// from the tail — including it lets the match eat into an
			// adjacent date ("K46473874/26.12.2022" became
			// "K46473874/26" which then loses the conflict pass to the
			// stronger DATE_TIME finding on the date suffix). Slash-
			// containing IDs are handled by the dedicated histology
			// regex `deClinicalIDHistRE`.
			`([A-Z]{0,4}-?\d[\dA-Z-]{1,14})\b`,
	)

	// German court-case number shape (Aktenzeichen). Examples:
	//   "10 Ls 296/22"   "25 VIII 24/22"   "4 VII 532/21"
	// The Roman-numeral chamber notation is distinctive enough that the
	// shape itself is self-anchoring — it's vanishingly rare in clinical
	// text, lab reports, or news prose. Two variants:
	//   (a) keyword-anchored (Az / Aktenzeichen / Geschäftszeichen) —
	//       highest confidence, used when the document signals "this is a
	//       case number" up front.
	//   (b) shape-only, requiring the Roman-numeral chamber form OR a
	//       1-3 letter CamelCase chamber abbreviation ("Ls", "Or", "Kls").
	//       Lower score so contradictory stronger signals can override.
	deCaseNumberAnchoredRE = regexp.MustCompile(
		`(?:Az\.?|Aktenzeichen|Gesch[äa]ftszeichen)[\s.:;,\t]*` +
			`(\d{1,4}[ \t]+(?:[A-Z][a-zA-Z]{0,5}|[IVX]{1,5})[ \t]+\d{1,5}/\d{2,4})\b`,
	)
	deCaseNumberShapeRE = regexp.MustCompile(
		`\b\d{1,4}[ \t]+(?:[A-Z][a-z]{0,3}|[IVX]{2,5})[ \t]+\d{1,5}/\d{2,4}\b`,
	)

	// Station / ward / OP / outpatient-clinic identifiers. The trigger
	// word strongly implies the next token is a room/unit code. Captures
	// short alphanumerics, optionally hyphenated (e.g. "O-11", "KJPP-2",
	// "5A", "302").
	deClinicalIDStationRE = regexp.MustCompile(
		`\b(?:` +
			// German triggers
			`Station|` +
			`Ambulanz|` +
			`Onkologie-?Ambulanz|` +
			`Onkologie|` +
			`Intensivstation|` +
			`Normalstation|` +
			`chirurgisch(?:en|er)?\s+(?:Klinik\s*-?\s*)?(?:Ambulanz|Station)|` +
			`OP|` +
			`Bett|` +
			`Zimmer|` +
			`Etage|` +
			`Gebäude|` +
			// English triggers — same intent.
			`Ward|Room|Unit|Bed|Floor|Suite|ICU|NICU|PICU|Emergency` +
			`)\s+` +
			`([A-Z]{1,4}-?\d{1,4}|\d{1,4}[A-Z]?(?:-\d{1,4})?|[IVX]{1,4})\b`,
	)

	// Histology / lab specimen codes: optional letter + digits + slash + digits.
	deClinicalIDHistRE = regexp.MustCompile(
		`\b[A-Z]\d{3,6}/\d{1,4}\b`,
	)

	// Standalone customer / contract / order identifiers — uppercase letter
	// prefix, hyphen-or-no-hyphen, 4-12 digits. Covers banking-statement
	// surfaces like "KD-6556039", "KDNR 987654321", "D17311590" where the
	// id is printed standalone in a header column without a "Kundennummer:"
	// keyword anchor nearby. Tightly bounded so it doesn't fire on every
	// "A12" or "X999" in clinical lab values: prefix is 2-5 letters and the
	// digit tail is 6-12 digits (so 4-digit doc-section markers stay safe).
	deStandaloneCustomerIDRE = regexp.MustCompile(
		`\b(?:KD|KDNR|KND|KU|KUNR|KN|VTR|VTRG|RG|BST|ORD)[-\s]?\d{6,12}\b`,
	)
)

// DEClinicalIDRecognizer is the concrete recognizer type.
type DEClinicalIDRecognizer struct{}

// NewDEClinicalIDRecognizer constructs the recognizer.
func NewDEClinicalIDRecognizer() *DEClinicalIDRecognizer { return &DEClinicalIDRecognizer{} }

// Name returns the recognizer name used in logs and conflict resolution.
func (r *DEClinicalIDRecognizer) Name() string { return "DEClinicalIDRecognizer" }

// SupportedEntities returns the entity types this recognizer emits.
func (r *DEClinicalIDRecognizer) SupportedEntities() []string { return []string{"ID"} }

// SupportedLanguages returns the languages this recognizer applies to.
// Both — despite the DE prefix in the name, the keyword set is bilingual:
// universal abbreviations (MRN, FN, Patient-ID, ID, histology codes) work
// in any language, and German/English long forms cover both clinical-text
// styles. Renaming would require touching every call site; kept as-is.
func (r *DEClinicalIDRecognizer) SupportedLanguages() []string { return []string{"de", "en"} }

// Analyze scans text for the three ID shapes and emits the VALUE portion
// of each (not the trigger keyword).
func (r *DEClinicalIDRecognizer) Analyze(_ context.Context, text string, _ []string, _ string) ([]analyzer.RecognizerResult, error) {
	if text == "" {
		return nil, nil
	}
	var out []analyzer.RecognizerResult

	// 1. Keyword-anchored. Submatch group 1 = value.
	for _, m := range deClinicalIDKeywordRE.FindAllStringSubmatchIndex(text, -1) {
		if len(m) < 4 || m[2] < 0 {
			continue
		}
		out = append(out, analyzer.RecognizerResult{
			Start:          m[2],
			End:            m[3],
			Score:          0.85,
			EntityType:     "ID",
			RecognizerName: r.Name(),
		})
	}

	// 2. Station / ward. Submatch group 1 = value.
	for _, m := range deClinicalIDStationRE.FindAllStringSubmatchIndex(text, -1) {
		if len(m) < 4 || m[2] < 0 {
			continue
		}
		out = append(out, analyzer.RecognizerResult{
			Start:          m[2],
			End:            m[3],
			Score:          0.75,
			EntityType:     "ID",
			RecognizerName: r.Name(),
		})
	}

	// 3. Histology / lab specimen codes.
	for _, m := range deClinicalIDHistRE.FindAllStringIndex(text, -1) {
		out = append(out, analyzer.RecognizerResult{
			Start:          m[0],
			End:            m[1],
			Score:          0.80,
			EntityType:     "ID",
			RecognizerName: r.Name(),
		})
	}

	// 4a. German court-case numbers — anchored on Az/Aktenzeichen. The
	// keyword anchor disambiguates from incidental "<digit> <word> <digit>/<digit>"
	// fragments. Submatch group 1 = the case number itself.
	for _, m := range deCaseNumberAnchoredRE.FindAllStringSubmatchIndex(text, -1) {
		if len(m) < 4 || m[2] < 0 {
			continue
		}
		out = append(out, analyzer.RecognizerResult{
			Start:          m[2],
			End:            m[3],
			Score:          0.90,
			EntityType:     "ID",
			RecognizerName: r.Name(),
		})
	}

	// 4b. Shape-only case-number detection. Roman-numeral chambers
	// ("VII", "VIII", "IV") or short CamelCase chamber abbreviations
	// ("Ls", "Or", "Kls") followed by /YY are distinctively legal.
	// Lower score than the anchored variant so it loses gracefully to
	// contradictory signals.
	for _, m := range deCaseNumberShapeRE.FindAllStringIndex(text, -1) {
		out = append(out, analyzer.RecognizerResult{
			Start:          m[0],
			End:            m[1],
			Score:          0.78,
			EntityType:     "ID",
			RecognizerName: r.Name(),
		})
	}

	// 5. Standalone customer / contract / order identifiers. Shape is
	// distinctive enough (KD-prefix or similar abbreviated head, then a
	// 6-12-digit tail) to fire without a preceding keyword anchor.
	for _, m := range deStandaloneCustomerIDRE.FindAllStringIndex(text, -1) {
		out = append(out, analyzer.RecognizerResult{
			Start:          m[0],
			End:            m[1],
			Score:          0.82,
			EntityType:     "ID",
			RecognizerName: r.Name(),
		})
	}

	// Within-recognizer dedupe. Pattern 1 (keyword-anchored) and pattern
	// 4 (case-number) can both fire on the same start offset — e.g.
	// "Az.: 10 Ls 296/22" produces the keyword anchor's "10" capture AND
	// the case-number's "10 Ls 296/22" capture. Keep the longer span and
	// drop the shorter overlapping one. The analyzer's RemoveConflicts
	// does this across recognizers; doing it here keeps the recognizer's
	// own output clean and makes unit tests order-stable.
	return dedupeOverlappingIDs(out), nil
}

// dedupeOverlappingIDs keeps the longest finding per overlapping cluster.
// Within a single recognizer two patterns can produce overlapping spans
// for the same identifier — the keyword anchor capturing only the
// leading-digit prefix while the shape pattern captures the full
// canonical case number. Pre-dedupe so downstream conflict resolution
// doesn't have to disambiguate inside the recognizer's own output.
func dedupeOverlappingIDs(in []analyzer.RecognizerResult) []analyzer.RecognizerResult {
	if len(in) < 2 {
		return in
	}
	// Sort by start, then by end desc (longest first at each start).
	// Stable enough since we only need longest-wins ordering.
	for i := 1; i < len(in); i++ {
		for j := i; j > 0; j-- {
			a, b := in[j-1], in[j]
			if a.Start > b.Start || (a.Start == b.Start && (a.End-a.Start) < (b.End-b.Start)) {
				in[j-1], in[j] = in[j], in[j-1]
				continue
			}
			break
		}
	}
	out := in[:0]
	for _, r := range in {
		// Drop r if it's fully contained in the most recently kept span.
		if len(out) > 0 {
			last := out[len(out)-1]
			if r.Start >= last.Start && r.End <= last.End {
				continue
			}
		}
		out = append(out, r)
	}
	return out
}
