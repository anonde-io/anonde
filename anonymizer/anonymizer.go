package anonymizer

import (
	"fmt"
	"sort"

	"github.com/moogacs/anonde/analyzer"
	"github.com/moogacs/anonde/anonymizer/operators"
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
