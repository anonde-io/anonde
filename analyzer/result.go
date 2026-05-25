package analyzer

import "sort"

// RecognizerResult represents a detected PII entity span.
type RecognizerResult struct {
	Start          int
	End            int
	Score          float64
	EntityType     string
	RecognizerName string
}

// nerRecognizerNames is the set of recognizer names that produce
// contextual NER findings (open-set, ML-derived) as opposed to regex /
// checksum / heuristic pattern findings (or pool / ensemble wrappers
// around one of those). Used by RemoveConflicts to prefer NER for
// unstructured entity types regardless of raw score — pattern scores
// are deterministic constants (0.85 / 1.0) and would otherwise always
// beat NER's sigmoid output (typically 0.40 – 0.85), even when the
// NER span is the more accurate one. Keep in sync with the recognizers
// package — if a new NER recognizer (or a pool wrapping one) ships,
// add its Name() string here.
var nerRecognizerNames = map[string]bool{
	"GLiNERRecognizer":            true,
	"GLiNERFlatRecognizer":        true,
	"GLiNERFlatNERRecognizer":     true,
	"GLiNEREnsembleNERRecognizer": true,
	"GLiNERPool":                  true,
	"GLiNERFlatPool":              true,
	"HugotNERRecognizer":          true,
	"OllamaNERRecognizer":         true,
}

// nerPreferredEntities is the set of entity types where NER is more
// reliable than regex/heuristic patterns when both fire on the same
// span. Structured types not in this set (IBAN, PHONE_NUMBER, DATE_TIME,
// EMAIL_ADDRESS, URL, credit cards, postal codes, …) still resolve by
// score, which preserves the regex precision win on shapes patterns
// match exactly.
var nerPreferredEntities = map[string]bool{
	"PERSON":       true,
	"ORGANIZATION": true,
	"LOCATION":     true,
	"AGE":          true,
	"PROFESSION":   true,
	"NRP":          true,
}

// isNERRecognizer reports whether r came from an NER recognizer.
func isNERRecognizer(r RecognizerResult) bool {
	return nerRecognizerNames[r.RecognizerName]
}

// prefersNERFor reports whether the entity type is one where we prefer
// NER findings over pattern findings when they conflict.
func prefersNERFor(entityType string) bool {
	return nerPreferredEntities[entityType]
}

// ContainedIn returns true if r is fully contained within other.
func (r RecognizerResult) ContainedIn(other RecognizerResult) bool {
	return r.Start >= other.Start && r.End <= other.End
}

// Overlaps returns true if r overlaps with other.
func (r RecognizerResult) Overlaps(other RecognizerResult) bool {
	return r.Start < other.End && r.End > other.Start
}

// SortResults sorts results by start position, then by score descending,
// then by length descending.
//
// The length tiebreaker is what lets RemoveConflicts merge a full date
// like "12.08.2025" with two same-score partials emitted on overlapping
// offsets ("12.08." + "2025") — the longer span sorts first and the
// shorter overlapping ones get dropped. Without the tiebreaker, sort
// order on tied scores is non-deterministic and the shorter span can
// win, leaving two adjacent fragments in the output.
func SortResults(results []RecognizerResult) {
	sort.Slice(results, func(i, j int) bool {
		if results[i].Start != results[j].Start {
			return results[i].Start < results[j].Start
		}
		if results[i].Score != results[j].Score {
			return results[i].Score > results[j].Score
		}
		return (results[i].End - results[i].Start) > (results[j].End - results[j].Start)
	})
}

// RemoveConflicts removes overlapping results.
//
// Resolution rule:
//  1. For entity types in nerPreferredEntities (PERSON, ORGANIZATION,
//     LOCATION, AGE, PROFESSION, NRP) — when an NER finding overlaps a
//     pattern finding, the NER finding wins regardless of score. Pattern
//     scores for these types come from heuristic recognizers like
//     DEAnomalyRecognizer (anomaly-based PERSON detection on German
//     clinical text) that produce deterministic constants (0.85, 1.0);
//     NER sigmoid outputs (0.40 – 0.85) would always lose under pure
//     score comparison, wasting the NER's contextual judgement.
//  2. Otherwise (structured entity types like IBAN, PHONE, DATE, …, or
//     two findings of the same recognizer class) — keep the
//     higher-scoring span. This preserves the regex+checksum precision
//     win on shapes patterns match exactly.
//
// Note: the resolver only compares against the LAST kept finding in the
// scan, not every prior. With the sort by (start, score desc, length
// desc) this is the documented anonde behavior — flagged here so future
// maintainers don't expect optimal-cover behavior.
func RemoveConflicts(results []RecognizerResult) []RecognizerResult {
	return RemoveConflictsWithCallback(results, nil)
}

// RemoveConflictsWithCallback is RemoveConflicts with an optional
// per-conflict observer. The callback fires once per overlapping
// pair the resolver examines, passing the winner (kept) and loser
// (discarded) findings in that order. nil cb is identical to
// RemoveConflicts.
//
// Wired through the analyzer engine's metrics Recorder so
// anonde_conflicts_resolved_total tracks NER-vs-pattern arbitration
// in production; tests pass nil and ignore the surface.
func RemoveConflictsWithCallback(results []RecognizerResult, cb func(winner, loser RecognizerResult)) []RecognizerResult {
	if len(results) == 0 {
		return results
	}
	SortResults(results)
	kept := []RecognizerResult{results[0]}
	for _, r := range results[1:] {
		last := kept[len(kept)-1]
		if !r.Overlaps(last) {
			kept = append(kept, r)
			continue
		}
		if shouldReplace(last, r) {
			if cb != nil {
				cb(r, last)
			}
			kept[len(kept)-1] = r
		} else if cb != nil {
			cb(last, r)
		}
	}
	return kept
}

// shouldReplace decides whether `candidate` should displace `kept` when
// they overlap. Implements the NER-preference rule from RemoveConflicts.
func shouldReplace(kept, candidate RecognizerResult) bool {
	// Both findings target the same entity type AND it's a type where
	// we prefer NER — NER wins over pattern, otherwise score decides.
	if kept.EntityType == candidate.EntityType && prefersNERFor(kept.EntityType) {
		keptNER := isNERRecognizer(kept)
		candNER := isNERRecognizer(candidate)
		if candNER && !keptNER {
			return true
		}
		if keptNER && !candNER {
			return false
		}
		// same class on both sides — fall through to score.
	}
	return candidate.Score > kept.Score
}
