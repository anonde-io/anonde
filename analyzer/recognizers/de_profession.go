package recognizers

import (
	"context"
	"regexp"

	"github.com/anonde-io/anonde/analyzer"
)

// DEProfessionRecognizer detects German profession words. PROFESSION is a
// HIPAA Safe Harbor identifier and a gold class in GraSCCo. Two pattern
// types:
//
//  1. Context-anchored: a profession word immediately follows a German
//     occupation cue ("Beruf:", "von Beruf", "tätig als", "arbeitete als",
//     "Tätigkeit:", "Profession:"). Captures group 1 = the profession
//     word itself, NOT the anchor.
//
//  2. Vocabulary fallback: a closed list of unambiguous German
//     occupation words (Rentnerin, Lehrer, Ingenieurin, Bäcker, …).
//     Excludes words that appear too often in non-occupation roles in
//     clinical text; most importantly "Arzt"/"Ärztin", which almost
//     always refer to a treating physician (a NAME_DOCTOR context, not
//     patient profession).

var (
	deProfessionAnchorRE = regexp.MustCompile(
		`(?:` +
			// Label form: "Beruf:", "Tätigkeit:", "Profession:"
			`\b(?:Beruf|Tätigkeit|Profession|Berufstätigkeit)\s*[:.-]?\s+|` +
			// Verb form: "von Beruf", "tätig als", "arbeitet(e)? als",
			// "war tätig als", "berufstätig als"
			`\b(?:von\s+Beruf|tätig\s+als|arbeitete?t?\s+als|war\s+tätig\s+als|` +
			`berufstätig\s+als|ausgebildete[rn]?)\s+` +
			`)` +
			// Single token (capital initial + lowercase tail) with an
			// optional hyphenated continuation that must also be capitalised
			// (e.g. "Maschinenbau-Techniker"). No space-joined continuation,
			// that would greedily eat the next sentence word.
			`([A-ZÄÖÜ][A-Za-zäöüß]+(?:-[A-ZÄÖÜ][A-Za-zäöüß]+)?)\b`,
	)

	// Closed vocabulary of German profession words that are
	// unambiguous patient-occupation tokens. Deliberately excludes
	// "Arzt", "Ärztin"; those mean *treating physician* in clinical
	// text, not patient profession.
	deProfessionVocabRE = regexp.MustCompile(`\b(?:` +
		`Rentner(?:in)?|` +
		`Pensionist(?:in)?|` +
		`Hausfrau|Hausmann|` +
		`Lehrer(?:in)?|` +
		`Student(?:in)?|Schüler(?:in)?|Auszubildende[rn]?|` +
		`Krankenschwester|Krankenpfleger(?:in)?|Altenpfleger(?:in)?|` +
		`Ingenieur(?:in)?|` +
		`Polizist(?:in)?|Beamt(?:er|e[rn]?)|` +
		`Architekt(?:in)?|` +
		`Bankangestellte[rn]?|Verwaltungsangestellte[rn]?|` +
		`Verkäufer(?:in)?|` +
		`Kaufmann|Kauffrau|` +
		`Bäcker(?:in)?|Metzger(?:in)?|Koch|Köchin|` +
		`Schreiner(?:in)?|Tischler(?:in)?|Schreiner-Meister(?:in)?|` +
		`Mechaniker(?:in)?|Elektriker(?:in)?|` +
		`Maschinenbau(?:techniker(?:in)?|ingenieur(?:in)?)|` +
		`Friseur(?:in)?|Friseuse|` +
		`Erzieher(?:in)?|Sozialarbeiter(?:in)?|Sozialpädagoge|Sozialpädagogin|` +
		`Selbstständige[rn]?|Selbstständig|` +
		`Hauswirtschafterin|Reinigungskraft` +
		`)\b`)
)

// DEProfessionRecognizer recognises German occupation words.
type DEProfessionRecognizer struct{}

// NewDEProfessionRecognizer constructs the recognizer.
func NewDEProfessionRecognizer() *DEProfessionRecognizer { return &DEProfessionRecognizer{} }

// Name returns the recognizer name used in logs and conflict resolution.
func (r *DEProfessionRecognizer) Name() string { return "DEProfessionRecognizer" }

// SupportedEntities returns the entity types this recognizer emits.
func (r *DEProfessionRecognizer) SupportedEntities() []string { return []string{"PROFESSION"} }

// SupportedLanguages returns the languages this recognizer applies to.
func (r *DEProfessionRecognizer) SupportedLanguages() []string { return []string{"de"} }

// Analyze emits PROFESSION findings. Context-anchored matches score 0.80;
// bare-vocabulary matches score 0.65. We dedupe inside the recognizer so
// the same word matched by both patterns is emitted once (as the anchored
// span), rather than relying on the engine's conflict resolver to drop
// duplicate same-type spans downstream.
func (r *DEProfessionRecognizer) Analyze(_ context.Context, text string, _ []string, _ string) ([]analyzer.RecognizerResult, error) {
	if text == "" {
		return nil, nil
	}
	var out []analyzer.RecognizerResult
	type span struct{ s, e int }
	var anchors []span

	for _, m := range deProfessionAnchorRE.FindAllStringSubmatchIndex(text, -1) {
		if len(m) < 4 || m[2] < 0 {
			continue
		}
		anchors = append(anchors, span{m[2], m[3]})
		out = append(out, analyzer.RecognizerResult{
			Start:          m[2],
			End:            m[3],
			Score:          0.80,
			EntityType:     "PROFESSION",
			RecognizerName: r.Name(),
		})
	}
	for _, m := range deProfessionVocabRE.FindAllStringIndex(text, -1) {
		covered := false
		for _, a := range anchors {
			if m[0] < a.e && m[1] > a.s {
				covered = true
				break
			}
		}
		if covered {
			continue
		}
		out = append(out, analyzer.RecognizerResult{
			Start:          m[0],
			End:            m[1],
			Score:          0.65,
			EntityType:     "PROFESSION",
			RecognizerName: r.Name(),
		})
	}
	return out, nil
}
