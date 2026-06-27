package recognizers

import (
	"context"
	"regexp"
	"strings"

	"github.com/anonde-io/anonde/analyzer"
)

// DefaultDenyListEntityType is the entity type assigned to deny-term matches
// when NewDenyListRecognizer is called with an empty entityType.
const DefaultDenyListEntityType = "CUSTOM"

// denyListScore is the score emitted for deny-term matches. Deny terms are an
// explicit user instruction ("always anonymize this"), so they carry a high,
// near-certain score — above every heuristic recognizer — to win conflict
// resolution and guarantee the term is tokenized + vaulted.
const denyListScore = 1.0

// DenyListRecognizer emits a finding for every occurrence of a user-supplied
// term. It is the term-level DENY policy: a deny term is ALWAYS detected
// (even when the built-in recognizers miss it) and flows downstream to be
// anonymized (tokenized + vaulted + reversible) under its entity type.
//
// Matching is case-insensitive. Each term is matched whole-word where that is
// meaningful: when a term begins (resp. ends) with a Unicode word character,
// a \b word boundary is required on that side, so "acme" does not fire inside
// "acmecorp". Terms whose edges are non-word characters (e.g. "@acme", "C++")
// fall back to a plain case-insensitive literal match on that side, since a
// word boundary there would never hold.
type DenyListRecognizer struct {
	name       string
	entityType string
	terms      []*regexp.Regexp
}

// NewDenyListRecognizer builds a recognizer that flags every occurrence of any
// term in terms. entityType is the entity type stamped on each finding; when
// empty it defaults to DefaultDenyListEntityType ("CUSTOM"). Blank / whitespace
// -only terms are ignored. The returned recognizer satisfies
// analyzer.EntityRecognizer and can be registered on an AnalyzerEngine like any
// built-in recognizer.
func NewDenyListRecognizer(terms []string, entityType string) *DenyListRecognizer {
	if strings.TrimSpace(entityType) == "" {
		entityType = DefaultDenyListEntityType
	}
	compiled := make([]*regexp.Regexp, 0, len(terms))
	seen := make(map[string]bool, len(terms))
	for _, t := range terms {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		key := strings.ToLower(t)
		if seen[key] {
			continue
		}
		seen[key] = true
		compiled = append(compiled, compileDenyTerm(t))
	}
	return &DenyListRecognizer{
		name:       "DenyListRecognizer",
		entityType: entityType,
		terms:      compiled,
	}
}

// compileDenyTerm builds a case-insensitive regexp for one term, applying a \b
// boundary only on edges that start/end with a word character.
func compileDenyTerm(term string) *regexp.Regexp {
	pattern := regexp.QuoteMeta(term)
	if isWordRune(firstRune(term)) {
		pattern = `\b` + pattern
	}
	if isWordRune(lastRune(term)) {
		pattern = pattern + `\b`
	}
	return regexp.MustCompile(`(?i)` + pattern)
}

func firstRune(s string) rune {
	for _, r := range s {
		return r
	}
	return 0
}

func lastRune(s string) rune {
	var last rune
	for _, r := range s {
		last = r
	}
	return last
}

// isWordRune reports whether r is a [A-Za-z0-9_] word rune, matching the
// character class behind regexp's \b boundary.
func isWordRune(r rune) bool {
	return r == '_' ||
		(r >= 'a' && r <= 'z') ||
		(r >= 'A' && r <= 'Z') ||
		(r >= '0' && r <= '9')
}

// Name implements analyzer.EntityRecognizer.
func (d *DenyListRecognizer) Name() string { return d.name }

// SupportedEntities implements analyzer.EntityRecognizer.
func (d *DenyListRecognizer) SupportedEntities() []string { return []string{d.entityType} }

// SupportedLanguages implements analyzer.EntityRecognizer. Deny terms are
// language-agnostic surface matches, so the recognizer supports all languages.
func (d *DenyListRecognizer) SupportedLanguages() []string { return []string{"*"} }

// Analyze returns a finding for every occurrence of every deny term.
func (d *DenyListRecognizer) Analyze(_ context.Context, text string, _ []string, _ string) ([]analyzer.RecognizerResult, error) {
	if len(d.terms) == 0 {
		return nil, nil
	}
	var results []analyzer.RecognizerResult
	for _, re := range d.terms {
		for _, m := range re.FindAllStringIndex(text, -1) {
			results = append(results, analyzer.RecognizerResult{
				Start:          m[0],
				End:            m[1],
				Score:          denyListScore,
				EntityType:     d.entityType,
				RecognizerName: d.name,
			})
		}
	}
	return results, nil
}
