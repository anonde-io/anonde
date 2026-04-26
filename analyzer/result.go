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

// ContainedIn returns true if r is fully contained within other.
func (r RecognizerResult) ContainedIn(other RecognizerResult) bool {
	return r.Start >= other.Start && r.End <= other.End
}

// Overlaps returns true if r overlaps with other.
func (r RecognizerResult) Overlaps(other RecognizerResult) bool {
	return r.Start < other.End && r.End > other.Start
}

// SortResults sorts results by start position, then by score descending.
func SortResults(results []RecognizerResult) {
	sort.Slice(results, func(i, j int) bool {
		if results[i].Start != results[j].Start {
			return results[i].Start < results[j].Start
		}
		return results[i].Score > results[j].Score
	})
}

// RemoveConflicts removes overlapping results, keeping the highest-scoring one.
func RemoveConflicts(results []RecognizerResult) []RecognizerResult {
	if len(results) == 0 {
		return results
	}
	SortResults(results)
	kept := []RecognizerResult{results[0]}
	for _, r := range results[1:] {
		last := kept[len(kept)-1]
		if !r.Overlaps(last) {
			kept = append(kept, r)
		} else if r.Score > last.Score {
			kept[len(kept)-1] = r
		}
	}
	return kept
}
