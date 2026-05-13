package anonymizer

import (
	"fmt"
	"sort"

	"github.com/anonde-io/anonde/analyzer"
	"github.com/anonde-io/anonde/anonymizer/operators"
)

// AnonymizerResult is the output of anonymizing a text.
type AnonymizerResult struct {
	// Text is the anonymized text.
	Text string
	// Items describes each replacement that was made.
	Items []AnonymizedItem
}

// AnonymizedItem records a single anonymization action.
type AnonymizedItem struct {
	Start        int
	End          int
	EntityType   string
	OperatorName string
	Text         string // replacement text
}

// AnonymizerConfig maps entity types to operators.
// Use "*" as the key for a default operator that applies to all unmatched entities.
type AnonymizerConfig map[string]Operator

// AnonymizerEngine applies anonymization operators to analyzer results.
type AnonymizerEngine struct{}

// NewAnonymizerEngine returns a new engine.
func NewAnonymizerEngine() *AnonymizerEngine { return &AnonymizerEngine{} }

// Anonymize replaces detected PII in text using the configured operators.
// Results are de-overlapped before processing: higher score wins, then larger span.
//
// Adjacent same-type spans separated only by ASCII whitespace are merged
// into one before tokenization. The motivating case: NER models that emit
// FIRSTNAME and LASTNAME as separate PERSON spans for "Priya Nair", which
// without the merge would render as "<PERSON_001> <PERSON_002>". After
// merging the output is a single "<PERSON_001>" token covering the whole
// name. Merging is deliberately at this layer (not in the recognizer)
// because bench corpora often annotate name components as separate gold
// spans — merging at the recognizer level would tank exact-match metrics.
func (e *AnonymizerEngine) Anonymize(text string, results []analyzer.RecognizerResult, cfg AnonymizerConfig) (*AnonymizerResult, error) {
	if cfg == nil {
		cfg = AnonymizerConfig{}
	}
	originalBytes := []byte(text)

	// Sort by start, deduplicate overlapping spans.
	sorted := make([]analyzer.RecognizerResult, len(results))
	copy(sorted, results)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].Start != sorted[j].Start {
			return sorted[i].Start < sorted[j].Start
		}
		return sorted[i].Score > sorted[j].Score
	})
	sorted = analyzer.RemoveConflicts(sorted)
	sorted = MergeAdjacentSameType(sorted, text)

	// Process right-to-left so offsets stay valid.
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Start > sorted[j].Start })

	out := make([]byte, len(originalBytes))
	copy(out, originalBytes)
	items := make([]AnonymizedItem, 0, len(sorted))

	for _, r := range sorted {
		if r.Start < 0 || r.End < 0 || r.Start > r.End || r.End > len(originalBytes) {
			return nil, fmt.Errorf(
				"invalid recognizer span start=%d end=%d text_bytes=%d entity=%q",
				r.Start, r.End, len(originalBytes), r.EntityType,
			)
		}
		// Sorted replacements are applied right-to-left, so spans should still
		// be valid in the current output buffer.
		if r.Start > len(out) || r.End > len(out) {
			return nil, fmt.Errorf(
				"recognizer span out of output bounds start=%d end=%d output_bytes=%d entity=%q",
				r.Start, r.End, len(out), r.EntityType,
			)
		}

		op := cfg[r.EntityType]
		if op == nil {
			op = cfg["*"]
		}
		if op == nil {
			op = &operators.Replace{}
		}

		original := string(originalBytes[r.Start:r.End])
		replacement, err := op.Anonymize(original, r.EntityType)
		if err != nil {
			return nil, fmt.Errorf("operator %s on %s: %w", op.Name(), r.EntityType, err)
		}

		out = append(out[:r.Start], append([]byte(replacement), out[r.End:]...)...)
		items = append(items, AnonymizedItem{
			Start:        r.Start,
			End:          r.Start + len(replacement),
			EntityType:   r.EntityType,
			OperatorName: op.Name(),
			Text:         replacement,
		})
	}

	// Reverse items back to left-to-right order.
	for i, j := 0, len(items)-1; i < j; i, j = i+1, j-1 {
		items[i], items[j] = items[j], items[i]
	}

	return &AnonymizerResult{Text: string(out), Items: items}, nil
}

// MergeAdjacentSameType folds same-type spans separated only by ASCII
// whitespace into a single span. Walks left-to-right after a sort by
// start; chains of three or more adjacent components collapse correctly.
// The merged span keeps the higher score and the first span's
// RecognizerName (so token operators that key on that field remain
// stable).
func MergeAdjacentSameType(in []analyzer.RecognizerResult, text string) []analyzer.RecognizerResult {
	if len(in) < 2 {
		return in
	}
	sort.Slice(in, func(i, j int) bool { return in[i].Start < in[j].Start })
	out := in[:0]
	out = append(out, in[0])
	for _, r := range in[1:] {
		last := &out[len(out)-1]
		if r.EntityType == last.EntityType && r.Start > last.End && r.Start <= len(text) && onlyAsciiWhitespace(text, last.End, r.Start) {
			last.End = r.End
			if r.Score > last.Score {
				last.Score = r.Score
			}
			continue
		}
		out = append(out, r)
	}
	return out
}

// onlyAsciiWhitespace reports whether text[a:b] is non-empty and contains
// only ASCII whitespace bytes (space, tab, newline, CR).
func onlyAsciiWhitespace(text string, a, b int) bool {
	if a >= b || a < 0 || b > len(text) {
		return false
	}
	for i := a; i < b; i++ {
		switch text[i] {
		case ' ', '\t', '\n', '\r':
			continue
		default:
			return false
		}
	}
	return true
}
