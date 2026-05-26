package analyzer

import (
	"math"
	"strings"
)

// ContextProvider is implemented by recognizers that want their findings
// score-boosted when contextually-suggestive keywords appear nearby in the
// surrounding text. Returning a nil/empty map disables the boost for the
// recognizer; equivalent to not implementing the interface at all.
//
// The map is keyed by the entity type the keywords apply to, since a single
// recognizer may emit multiple entity types with different context cues.
type ContextProvider interface {
	ContextKeywords() map[string][]string
}

// ContextEnhancement controls how surrounding-text keywords influence scores.
//
// Defaults match Presidio's ContextAwareEnhancer behavior:
//   - WindowChars=80  ≈ 5–10 words on each side of the finding
//   - Boost=0.35     enough to lift a weak match (e.g. CC at 0.3) above a
//     ScoreThreshold of 0.5 without saturating strong matches
//
// Score is capped at 1.0.
type ContextEnhancement struct {
	WindowChars int
	Boost       float64
}

// DefaultContextEnhancement returns sensible defaults.
func DefaultContextEnhancement() ContextEnhancement {
	return ContextEnhancement{WindowChars: 80, Boost: 0.35}
}

// EnhanceWithContext boosts scores of findings whose surrounding-text window
// contains any of the entity's context keywords. The boost is additive and
// capped at 1.0. Findings without matching keywords are unchanged.
//
// keywordsByEntity maps EntityType -> list of context keywords; built once
// from the registry by collectContextKeywords. text is the original input.
func EnhanceWithContext(text string, results []RecognizerResult, keywordsByEntity map[string][]string, cfg ContextEnhancement) []RecognizerResult {
	if len(results) == 0 || len(keywordsByEntity) == 0 {
		return results
	}
	if cfg.WindowChars <= 0 {
		cfg.WindowChars = 80
	}
	if cfg.Boost <= 0 {
		cfg.Boost = 0.35
	}

	lower := strings.ToLower(text)
	textLen := len(lower)
	for i, r := range results {
		kws := keywordsByEntity[r.EntityType]
		if len(kws) == 0 {
			continue
		}
		// Defensive clamps: a misbehaving recognizer could emit spans
		// outside the original text; never panic over that.
		fStart, fEnd := r.Start, r.End
		if fStart < 0 {
			fStart = 0
		}
		if fEnd > textLen {
			fEnd = textLen
		}
		if fStart > fEnd {
			fStart = fEnd
		}
		start := fStart - cfg.WindowChars
		if start < 0 {
			start = 0
		}
		end := fEnd + cfg.WindowChars
		if end > textLen {
			end = textLen
		}
		// Exclude the finding itself from the context window so that a long
		// match (e.g. an IBAN containing "IBAN" as a substring of nothing)
		// can never self-confirm.
		before := lower[start:fStart]
		after := lower[fEnd:end]
		if hasAnyWord(before, kws) || hasAnyWord(after, kws) {
			results[i].Score = math.Min(r.Score+cfg.Boost, 1.0)
		}
	}
	return results
}

// hasAnyWord reports whether any of the (already-lowercased) keywords appears
// in text with word boundaries on both sides. Pure-byte boundary check,
// adequate for ASCII context keywords, which is all Presidio ships and all
// we need for English/European-language contexts.
func hasAnyWord(text string, keywords []string) bool {
	if text == "" {
		return false
	}
	for _, kw := range keywords {
		kw = strings.ToLower(strings.TrimSpace(kw))
		if kw == "" {
			continue
		}
		if matchWord(text, kw) {
			return true
		}
	}
	return false
}

func matchWord(text, kw string) bool {
	idx := 0
	for idx < len(text) {
		i := strings.Index(text[idx:], kw)
		if i < 0 {
			return false
		}
		pos := idx + i
		if isWordBoundary(text, pos, pos+len(kw)) {
			return true
		}
		idx = pos + 1
	}
	return false
}

func isWordBoundary(text string, start, end int) bool {
	leftOK := start == 0 || !isWordByte(text[start-1])
	rightOK := end == len(text) || !isWordByte(text[end])
	return leftOK && rightOK
}

func isWordByte(b byte) bool {
	return (b >= 'a' && b <= 'z') ||
		(b >= 'A' && b <= 'Z') ||
		(b >= '0' && b <= '9') ||
		b == '_'
}

// CollectContextKeywords walks the registry and merges keywords from every
// recognizer that implements ContextProvider, keyed by entity type.
func CollectContextKeywords(recognizers []EntityRecognizer) map[string][]string {
	out := map[string][]string{}
	for _, rec := range recognizers {
		cp, ok := rec.(ContextProvider)
		if !ok {
			continue
		}
		for entity, kws := range cp.ContextKeywords() {
			out[entity] = append(out[entity], kws...)
		}
	}
	return out
}
