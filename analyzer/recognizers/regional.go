package recognizers

import (
	"context"
	"regexp"
	"strings"

	"github.com/anonde-io/anonde/analyzer"
)

// validatedRecognizer is the workhorse for region-specific PII recognizers.
// Each instance holds:
//
//   - one or more regex variants with distinct base scores ("strong" /
//     "medium" / "weak" — matching Presidio's pattern naming convention),
//   - an optional Validate hook to bump (or reject) findings via a checksum
//     or format check,
//   - context keywords used by analyzer.ContextEnhancer to boost confidence
//     when suggestive words appear nearby.
//
// New regional recognizers should just declare data via NewValidatedRecognizer
// rather than implement EntityRecognizer from scratch — keeps each file thin
// and the behavior auditable in one place.
type validatedRecognizer struct {
	name      string
	entity    string
	languages []string
	patterns  []scoredPattern
	context   []string

	// Normalize strips formatting (separators, dots, dashes, spaces) before
	// passing the candidate to Validate. Optional.
	Normalize func(string) string
	// Validate returns (passes, score). When passes==true the result score
	// becomes max(patternScore, score). When passes==false and score==0 the
	// finding is dropped entirely; otherwise the lower score is used.
	Validate func(string) (bool, float64)
}

type scoredPattern struct {
	re    *regexp.Regexp
	score float64
	// spanGroup, when > 0, names a regex capture group (1-indexed) whose
	// indices the recognizer reports as the entity span instead of the
	// full match. Useful when the regex needs lookaround context (e.g. a
	// "phone:" prefix) but the entity itself is just the digits.
	spanGroup int
}

// NewValidatedRecognizer builds a regional recognizer.
//
// `patterns` is a list of (regex source, base score) pairs. They're compiled
// at construction time; a malformed regex panics — appropriate for package
// initialization, since all recognizers live behind constructor functions.
func NewValidatedRecognizer(
	name, entity string,
	languages []string,
	patterns [][2]any,
	contextKeywords []string,
) *validatedRecognizer {
	if len(languages) == 0 {
		languages = []string{"*"}
	}
	compiled := make([]scoredPattern, 0, len(patterns))
	for _, p := range patterns {
		src, _ := p[0].(string)
		score, _ := p[1].(float64)
		compiled = append(compiled, scoredPattern{
			re:    regexp.MustCompile(src),
			score: score,
		})
	}
	return &validatedRecognizer{
		name:      name,
		entity:    entity,
		languages: languages,
		patterns:  compiled,
		context:   contextKeywords,
	}
}

func (r *validatedRecognizer) Name() string                 { return r.name }
func (r *validatedRecognizer) SupportedEntities() []string  { return []string{r.entity} }
func (r *validatedRecognizer) SupportedLanguages() []string { return r.languages }

// ContextKeywords implements analyzer.ContextProvider.
func (r *validatedRecognizer) ContextKeywords() map[string][]string {
	if len(r.context) == 0 {
		return nil
	}
	return map[string][]string{r.entity: r.context}
}

func (r *validatedRecognizer) Analyze(_ context.Context, text string, _ []string, _ string) ([]analyzer.RecognizerResult, error) {
	if text == "" {
		return nil, nil
	}
	// De-dup overlapping spans from multiple variants — keep the highest
	// pattern score on identical (start,end). Conflict resolution at the
	// engine level handles broader overlaps.
	type span struct{ s, e int }
	best := map[span]float64{}
	for _, pat := range r.patterns {
		if pat.spanGroup > 0 {
			// Use the configured capture group's indices as the entity span.
			for _, m := range pat.re.FindAllStringSubmatchIndex(text, -1) {
				gi := 2 * pat.spanGroup
				if gi+1 >= len(m) || m[gi] < 0 {
					continue
				}
				key := span{m[gi], m[gi+1]}
				if cur, ok := best[key]; !ok || pat.score > cur {
					best[key] = pat.score
				}
			}
			continue
		}
		for _, m := range pat.re.FindAllStringIndex(text, -1) {
			key := span{m[0], m[1]}
			if cur, ok := best[key]; !ok || pat.score > cur {
				best[key] = pat.score
			}
		}
	}
	var out []analyzer.RecognizerResult
	for sp, base := range best {
		match := text[sp.s:sp.e]
		score := base
		if r.Validate != nil {
			candidate := match
			if r.Normalize != nil {
				candidate = r.Normalize(match)
			}
			pass, vScore := r.Validate(candidate)
			if !pass {
				if vScore == 0 {
					continue // drop unvalidatable hits when no fallback score
				}
				if vScore < score {
					score = vScore
				}
			} else if vScore > score {
				score = vScore
			}
		}
		out = append(out, analyzer.RecognizerResult{
			Start:          sp.s,
			End:            sp.e,
			Score:          score,
			EntityType:     r.entity,
			RecognizerName: r.name,
		})
	}
	return out, nil
}

// stripSeparators removes spaces, dots, dashes, and slashes — common formatters
// across the regional ID schemes we support.
func stripSeparators(s string) string {
	rep := strings.NewReplacer(" ", "", "-", "", ".", "", "/", "")
	return rep.Replace(s)
}
