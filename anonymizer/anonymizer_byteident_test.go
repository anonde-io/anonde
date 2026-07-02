package anonymizer_test

import (
	"fmt"
	"math/rand"
	"sort"
	"strings"
	"testing"

	"github.com/anonde-io/anonde/analyzer"
	"github.com/anonde-io/anonde/anonymizer"
	"github.com/anonde-io/anonde/anonymizer/operators"
)

// anonymizeReference is a byte-for-byte copy of the ORIGINAL right-to-left
// Anonymize implementation. It is the oracle the single-pass rewrite must
// match exactly (both Text and Items). Keep it frozen: it exists only to
// prove the optimized pass preserves behaviour.
func anonymizeReference(text string, results []analyzer.RecognizerResult, cfg anonymizer.AnonymizerConfig) (*anonymizer.AnonymizerResult, error) {
	originalBytes := []byte(text)

	sorted := make([]analyzer.RecognizerResult, len(results))
	copy(sorted, results)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].Start != sorted[j].Start {
			return sorted[i].Start < sorted[j].Start
		}
		return sorted[i].Score > sorted[j].Score
	})
	sorted = analyzer.RemoveConflicts(sorted)
	sorted = anonymizer.MergeAdjacentSameType(sorted, text)

	// Process right-to-left so offsets stay valid.
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Start > sorted[j].Start })

	out := make([]byte, len(originalBytes))
	copy(out, originalBytes)
	items := make([]anonymizer.AnonymizedItem, 0, len(sorted))

	for _, r := range sorted {
		if r.Start < 0 || r.End < 0 || r.Start > r.End || r.End > len(originalBytes) {
			return nil, fmt.Errorf(
				"invalid recognizer span start=%d end=%d text_bytes=%d entity=%q",
				r.Start, r.End, len(originalBytes), r.EntityType,
			)
		}
		if r.Start > len(out) || r.End > len(out) {
			return nil, fmt.Errorf(
				"recognizer span out of output bounds start=%d end=%d output_bytes=%d entity=%q",
				r.Start, r.End, len(out), r.EntityType,
			)
		}

		original := string(originalBytes[r.Start:r.End])

		if cfg.DetectOnlyTypes[r.EntityType] {
			continue
		}
		if len(cfg.AllowList) > 0 {
			if cfg.AllowList[strings.ToLower(strings.TrimSpace(original))] {
				continue
			}
		}

		op := cfg.Operators[r.EntityType]
		if op == nil {
			op = cfg.Operators["*"]
		}
		if op == nil {
			op = &operators.Replace{}
		}
		if do, ok := op.(anonymizer.DetectOnly); ok && do.IsDetectOnly() {
			continue
		}

		replacement, err := op.Anonymize(original, r.EntityType)
		if err != nil {
			return nil, fmt.Errorf("operator %s on %s: %w", op.Name(), r.EntityType, err)
		}

		out = append(out[:r.Start], append([]byte(replacement), out[r.End:]...)...)
		items = append(items, anonymizer.AnonymizedItem{
			Start:        r.Start,
			End:          r.Start + len(replacement),
			EntityType:   r.EntityType,
			OperatorName: op.Name(),
			Text:         replacement,
		})
	}

	for i, j := 0, len(items)-1; i < j; i, j = i+1, j-1 {
		items[i], items[j] = items[j], items[i]
	}

	return &anonymizer.AnonymizerResult{Text: string(out), Items: items}, nil
}

func itemsEqual(a, b []anonymizer.AnonymizedItem) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestAnonymize_OffsetSemantics pins the exact (quirky) offset contract the
// single-pass rewrite must preserve: item.Start is the ORIGINAL start and
// item.End is Start+len(replacement) — NOT final-output coordinates. Two
// findings with differing replacement lengths make the distinction visible.
func TestAnonymize_OffsetSemantics(t *testing.T) {
	eng := anonymizer.NewAnonymizerEngine()
	// "aXbYc": X at [1:2] -> "LONG" (len 4), Y at [3:4] -> "Z" (len 1).
	out, err := eng.Anonymize("aXbYc", results(
		r(1, 2, "X", 0.9),
		r(3, 4, "Y", 0.8),
	), anonymizer.Config(anonymizer.OperatorMap{
		"X": &operators.Replace{NewValue: "LONG"},
		"Y": &operators.Replace{NewValue: "Z"},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if out.Text != "aLONGbZc" {
		t.Fatalf("text: got %q want %q", out.Text, "aLONGbZc")
	}
	if len(out.Items) != 2 {
		t.Fatalf("want 2 items, got %d", len(out.Items))
	}
	// Left-to-right order, original-anchored offsets.
	want := []anonymizer.AnonymizedItem{
		{Start: 1, End: 5, EntityType: "X", OperatorName: "replace", Text: "LONG"},
		{Start: 3, End: 4, EntityType: "Y", OperatorName: "replace", Text: "Z"},
	}
	if !itemsEqual(out.Items, want) {
		t.Fatalf("items mismatch:\n got %+v\nwant %+v", out.Items, want)
	}
}

// TestAnonymize_ByteIdenticalToReference fuzzes many random documents and span
// sets through both the production Anonymize and the frozen right-to-left
// oracle, asserting the anonymized Text AND every returned item are identical.
// This is the load-bearing guard for the single-pass rewrite: recall is sacred.
func TestAnonymize_ByteIdenticalToReference(t *testing.T) {
	eng := anonymizer.NewAnonymizerEngine()
	rng := rand.New(rand.NewSource(0xA11DE))

	// Rune pool mixes ASCII, whitespace and multibyte runes so byte offsets
	// exercise multibyte boundaries.
	pool := []rune("abcXY 09\t\n" + "é—’日本")
	entityTypes := []string{"A", "B", "PERSON", "URL", "DATE_TIME"}

	makeOp := func(pick int, salt int) anonymizer.Operator {
		switch pick {
		case 0:
			return &operators.Replace{NewValue: fmt.Sprintf("<R%d>", salt)}
		case 1:
			return &operators.Redact{}
		case 2:
			return &operators.Mask{CharsToMask: 1 + salt%4, FromEnd: salt%2 == 0}
		default:
			return &operators.Hash{HashType: operators.HashSHA256}
		}
	}

	const iterations = 4000
	for iter := 0; iter < iterations; iter++ {
		// Build random text as runes; record byte offset of each rune.
		nRunes := rng.Intn(120)
		runes := make([]rune, nRunes)
		for i := range runes {
			runes[i] = pool[rng.Intn(len(pool))]
		}
		text := string(runes)
		// byteAt[i] = byte offset of rune index i; byteAt[nRunes] = len(text).
		byteAt := make([]int, nRunes+1)
		off := 0
		for i, rn := range runes {
			byteAt[i] = off
			off += len(string(rn))
		}
		byteAt[nRunes] = len(text)

		// Non-overlapping spans on rune boundaries, positive length, distinct
		// descending scores (so any adjacency merge / sort is deterministic).
		var spans []analyzer.RecognizerResult
		pos := 0
		score := 0.99
		for pos < nRunes {
			pos += rng.Intn(4) // optional gap
			if pos >= nRunes {
				break
			}
			span := 1 + rng.Intn(6)
			end := pos + span
			if end > nRunes {
				end = nRunes
			}
			spans = append(spans, analyzer.RecognizerResult{
				Start:          byteAt[pos],
				End:            byteAt[end],
				EntityType:     entityTypes[rng.Intn(len(entityTypes))],
				Score:          score,
				RecognizerName: "fuzz",
			})
			score -= 0.001
			pos = end
		}

		// Build a config: per-type operators, sometimes a "*" default, plus a
		// random DetectOnlyTypes subset and AllowList subset.
		ops := anonymizer.OperatorMap{}
		for i, et := range entityTypes {
			if rng.Intn(4) == 0 {
				continue // leave to the "*" default / built-in fallback
			}
			ops[et] = makeOp(rng.Intn(4), i+iter)
		}
		if rng.Intn(2) == 0 {
			ops["*"] = makeOp(rng.Intn(4), 99)
		}
		cfg := anonymizer.AnonymizerConfig{Operators: ops}
		if rng.Intn(3) == 0 {
			cfg.DetectOnlyTypes = map[string]bool{entityTypes[rng.Intn(len(entityTypes))]: true}
		}
		if rng.Intn(3) == 0 && len(text) > 0 {
			// Allow a random existing surface verbatim.
			s := byteAt[rng.Intn(nRunes)]
			e := byteAt[min(nRunes, rng.Intn(nRunes)+1)]
			if e > s {
				surface := strings.ToLower(strings.TrimSpace(text[s:e]))
				if surface != "" {
					cfg.AllowList = map[string]bool{surface: true}
				}
			}
		}

		gotResult, gotErr := eng.Anonymize(text, spans, cfg)
		wantResult, wantErr := anonymizeReference(text, spans, cfg)

		if (gotErr == nil) != (wantErr == nil) {
			t.Fatalf("iter %d: err mismatch got=%v want=%v\ntext=%q spans=%+v", iter, gotErr, wantErr, text, spans)
		}
		if gotErr != nil {
			continue
		}
		if gotResult.Text != wantResult.Text {
			t.Fatalf("iter %d: TEXT mismatch\n got %q\nwant %q\ntext=%q spans=%+v", iter, gotResult.Text, wantResult.Text, text, spans)
		}
		if !itemsEqual(gotResult.Items, wantResult.Items) {
			t.Fatalf("iter %d: ITEMS mismatch\n got %+v\nwant %+v\ntext=%q spans=%+v", iter, gotResult.Items, wantResult.Items, text, spans)
		}
	}
}
