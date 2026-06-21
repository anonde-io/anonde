package recognizers

import (
	"context"
	"regexp"

	"github.com/anonde-io/anonde/analyzer"
)

// English-language PERSON patterns. Complements the NER backend for
// surfaces it consistently misses on the ai4privacy_en synthetic gold:
//
//	Mr.       Mrs.      Ms.    Dr.     Prof.    Sir   Madam
//	Roma_Altenwerth     Joe_Schuster53   Edwin_Nitzsche
//
// The honorifics are a closed list; emit as PERSON so a leak-rate
// overlap counts even when the NER misses the bound name on the right.
// The underscored-username shape matches the
// First_Last(digits) pattern these synthetic corpora use; deliberately
// shape-restricted (capitalised start, underscore mid, capitalised
// continuation) to avoid FP on snake_case identifiers like
// "max_connections" or "user_id".

var (
	// Honorifics with a trailing period, optionally followed by a
	// capitalised name. The trailing-boundary is intentionally
	// permissive; the period followed by ANYTHING that isn't a word
	// character used to be a clean boundary, but ai4privacy gold has
	// run-together surfaces like "Hello Mr.Kerluke" where the period
	// is followed by an uppercase letter. We accept that case too: an
	// honorific followed by a period is a complete match regardless
	// of the next character, because no English word ends with one of
	// these specific abbreviations followed by a period.
	enHonorificRE = regexp.MustCompile(
		`\b(?:Mr|Mrs|Ms|Mx|Dr|Prof|Sir|Madam|Lord|Lady|Hon|Rev|Capt|Lt|Sgt|Col|Gen|Adm)\.`,
	)

	// First_Last underscored username; capitalised first segment,
	// underscore, capitalised second segment, optional digit suffix.
	enUnderscoredNameRE = regexp.MustCompile(
		`\b[A-Z][a-z]{2,30}_[A-Z][a-z]{2,30}\d{0,4}\b`,
	)

	// Bare-name + digit suffix ("Camilla10", "Zachariah54"). Strict
	// shape: capitalised letter, 4+ lowercase letters, then 1-4 digits.
	// The 4-letter minimum on the stem rules out "Section4", "Article2",
	// "Iso27001", and similar short-word + digit collocations that
	// commonly appear in tech and legal prose. Score is intentionally
	// lower than the underscored variant; bare "Name#" is more
	// ambiguous than "First_Last#" so a contradictory signal should
	// be able to override it.
	enNamePlusDigitsRE = regexp.MustCompile(
		`\b[A-Z][a-z]{4,30}\d{1,4}\b`,
	)
)

// NewENPersonRecognizer detects English-language PERSON surfaces the
// NER backend tends to miss: honorifics and underscored usernames.
//
// Wrapped in structuralGuardRecognizer so a candidate whose WHOLE surface is
// a machine token (UUID / hex / base64 / snake_case / dotted-path / model-slug
// / locale / semver) is dropped at emit time. The reversible First_Last(digits)
// username shape (enUnderscoredNameRE) is EXEMPT — reSnakeName keeps it out of
// isStructuralSurface — so this guard removes only structural FPs, never a real
// name. Leak-safe by construction.
func NewENPersonRecognizer() analyzer.EntityRecognizer {
	inner := NewPatternRecognizerWithContext(
		"ENPersonRecognizer",
		[]string{"PERSON"},
		[]string{"en"},
		[]namedPattern{
			{re: enHonorificRE, score: 0.82},
			{re: enUnderscoredNameRE, score: 0.85},
			{re: enNamePlusDigitsRE, score: 0.72},
		},
		[]string{
			"name", "patient", "customer", "doctor",
			"contact", "user", "username", "account",
		},
	)
	return structuralGuardRecognizer{inner: inner}
}

// structuralGuardRecognizer decorates an EntityRecognizer, dropping any
// emitted result whose surface is a structural machine token. Used by the
// heuristic PERSON/ORG pattern recognizers so the one isStructuralSurface
// definition governs every name-shaped emitter. ContextProvider is forwarded
// so context-keyword score enhancement is preserved.
type structuralGuardRecognizer struct {
	inner analyzer.EntityRecognizer
}

func (g structuralGuardRecognizer) Name() string                 { return g.inner.Name() }
func (g structuralGuardRecognizer) SupportedEntities() []string  { return g.inner.SupportedEntities() }
func (g structuralGuardRecognizer) SupportedLanguages() []string { return g.inner.SupportedLanguages() }

// ContextKeywords forwards the inner recognizer's context keywords when it
// provides any, so the analyzer's score enhancer still fires.
func (g structuralGuardRecognizer) ContextKeywords() map[string][]string {
	if cp, ok := g.inner.(interface {
		ContextKeywords() map[string][]string
	}); ok {
		return cp.ContextKeywords()
	}
	return nil
}

func (g structuralGuardRecognizer) Analyze(ctx context.Context, text string, langs []string, lang string) ([]analyzer.RecognizerResult, error) {
	res, err := g.inner.Analyze(ctx, text, langs, lang)
	if err != nil || len(res) == 0 {
		return res, err
	}
	out := res[:0]
	for _, r := range res {
		if r.Start >= 0 && r.End <= len(text) && r.Start < r.End && isStructuralSurface(text[r.Start:r.End]) {
			continue
		}
		out = append(out, r)
	}
	return out, nil
}
