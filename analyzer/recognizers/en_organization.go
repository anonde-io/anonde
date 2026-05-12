package recognizers

import (
	"context"
	"regexp"
	"strings"

	"github.com/moogacs/anonde/analyzer"
)

// English healthcare-organisation patterns. Mirrors the design of
// DEOrganizationRecognizer: two regex shapes, both precision-tuned, plus
// a closed list of well-known institutions. Captures the full
// institution span (prefix + suffix).
//
// Suffix shape — 1-4 capitalised prefix tokens, then a healthcare suffix.
// Catches:
//
//	"Mercy General"                          (Mercy + General)
//	"St. Joseph's Hospital"                  (St. + Joseph's + Hospital)
//	"Massachusetts General Hospital"         (2-token prefix + Hospital)
//	"Cleveland Clinic"                       (Cleveland + Clinic)
//	"Cedars-Sinai Medical Center"            (hyphenated + Medical Center)
//
// Well-known list — institutions whose names don't include a generic
// suffix (e.g. Johns Hopkins, Kaiser Permanente) or where we want a
// strict high-precision match regardless of suffix greediness.
//
// Two post-processing steps after regex matching:
//
//  1. Leading-verb trim: clinical text often starts a sentence with a
//     verb that gets swallowed by the prefix-token regex (e.g. "Visit",
//     "Admitted", "Transferred"). We trim any leading-token whose
//     lowercased form is a known verb and re-anchor the span.
//  2. Suffix-vs-well-known overlap dedup: both patterns frequently fire
//     on the same span ("Mayo Clinic" matches both). Suffix is dropped
//     when its span overlaps any well-known span — the well-known has a
//     higher score.

var (
	// Suffix form: "<Prefix Words> <Suffix>".
	// Horizontal whitespace only between tokens — newlines never start
	// a continuation of an org name.
	enOrgSuffixRE = regexp.MustCompile(
		`\b(?:[A-Z][a-zA-Z']+\.?|[A-Z][a-zA-Z]+-[A-Z][a-zA-Z]+)` +
			`(?:[ \t]+(?:[A-Z][a-zA-Z']+\.?|[A-Z][a-zA-Z]+-[A-Z][a-zA-Z]+)){0,3}` +
			`[ \t]+` +
			`(?:Hospital|Clinic|Medical[ \t]+Center|Health[ \t]+(?:Center|System)|` +
			`General|Memorial|Healthcare|Infirmary|Sanitarium|Sanatorium|` +
			`Medical[ \t]+Plaza|Polyclinic|Hospice|Health[ \t]+Sciences[ \t]+Center)\b`,
	)

	// Well-known US/UK institutions. Closed list, high precision.
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

// enOrgLeadingVerbs are sentence-leading verbs / prepositions that the
// suffix regex misinterprets as the first prefix token of an org name.
// On a match starting with one of these, we strip it and re-anchor.
var enOrgLeadingVerbs = map[string]struct{}{
	"Visit": {}, "Visited": {},
	"Admitted": {}, "Saw": {}, "Seen": {}, "Met": {},
	"Treated": {}, "Treating": {},
	"Discharged": {}, "Referred": {}, "Transferred": {},
	"Presented": {}, "Walked": {}, "Arrived": {},
	"At": {}, "To": {}, "From": {}, "By": {},
	"The": {}, "This": {}, "That": {}, "These": {}, "Those": {},
}

// ENOrganizationRecognizer recognises English healthcare organisations.
type ENOrganizationRecognizer struct{}

// NewENOrganizationRecognizer constructs the recognizer.
func NewENOrganizationRecognizer() *ENOrganizationRecognizer {
	return &ENOrganizationRecognizer{}
}

// Name returns the recognizer name used in logs and conflict resolution.
func (r *ENOrganizationRecognizer) Name() string { return "ENOrganizationRecognizer" }

// SupportedEntities returns the entity types this recognizer emits.
func (r *ENOrganizationRecognizer) SupportedEntities() []string { return []string{"ORGANIZATION"} }

// SupportedLanguages returns the languages this recognizer applies to.
func (r *ENOrganizationRecognizer) SupportedLanguages() []string { return []string{"en"} }

// Analyze emits ORGANIZATION findings. Well-known matches score 0.90;
// suffix matches score 0.80. Suffix matches that overlap a well-known
// match are dropped to avoid duplicate spans within this recognizer.
func (r *ENOrganizationRecognizer) Analyze(_ context.Context, text string, _ []string, _ string) ([]analyzer.RecognizerResult, error) {
	if text == "" {
		return nil, nil
	}
	var out []analyzer.RecognizerResult
	type span struct{ s, e int }
	var wellKnown []span

	// 1. Well-known (high precision).
	for _, m := range enOrgWellKnownRE.FindAllStringIndex(text, -1) {
		wellKnown = append(wellKnown, span{m[0], m[1]})
		out = append(out, analyzer.RecognizerResult{
			Start:          m[0],
			End:            m[1],
			Score:          0.90,
			EntityType:     "ORGANIZATION",
			RecognizerName: r.Name(),
		})
	}

	// 2. Suffix pattern (post-process: trim leading verb, dedupe vs well-known).
	for _, m := range enOrgSuffixRE.FindAllStringIndex(text, -1) {
		start, end := m[0], m[1]

		// Trim leading-verb tokens one by one. After each removal,
		// re-check the new first token. Stop when the first token is
		// not a known leading verb.
		for {
			sub := text[start:end]
			i := 0
			for i < len(sub) && (sub[i] == ' ' || sub[i] == '\t') {
				i++
			}
			j := i
			for j < len(sub) && sub[j] != ' ' && sub[j] != '\t' {
				j++
			}
			if j == i {
				break // no more tokens to consider
			}
			firstWord := sub[i:j]
			if _, isVerb := enOrgLeadingVerbs[firstWord]; !isVerb {
				break
			}
			// Advance start past the verb and the whitespace after it.
			advance := j
			for advance < len(sub) && (sub[advance] == ' ' || sub[advance] == '\t') {
				advance++
			}
			start += advance
			if start >= end {
				start = end
				break
			}
		}
		if start >= end {
			continue
		}
		// After trimming we still need at least one prefix token plus
		// a suffix (i.e. whitespace inside the surviving span).
		if !strings.ContainsAny(text[start:end], " \t") {
			continue
		}
		// Drop if it overlaps any well-known match — that one has the
		// higher score and represents the same entity.
		overlap := false
		for _, w := range wellKnown {
			if start < w.e && end > w.s {
				overlap = true
				break
			}
		}
		if overlap {
			continue
		}
		out = append(out, analyzer.RecognizerResult{
			Start:          start,
			End:            end,
			Score:          0.80,
			EntityType:     "ORGANIZATION",
			RecognizerName: r.Name(),
		})
	}
	return out, nil
}
