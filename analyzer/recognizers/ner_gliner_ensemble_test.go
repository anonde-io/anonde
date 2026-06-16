//go:build ner

// ner_gliner_ensemble_test.go covers the pure-logic parts of the
// ensemble recognizer: mergeOverlapping (the OR-merge) and
// EnsembleFromEnv's three-way return contract. The Analyze() goroutine
// fan-out is exercised indirectly; full integration coverage that
// requires real ONNX sessions lives in the bench harness, not in unit
// tests, because the per-member init() downloads ~370 MB of model
// weights.

package recognizers

import (
	"os"
	"testing"

	"github.com/anonde-io/anonde/analyzer"
)

// TestMergeOverlapping_UnionBoundsSameType covers the canonical case:
// two members find the same PERSON span at slightly different
// boundaries; merge widens to the union and keeps the higher score /
// recognizer name. Recall > precision: the redactor
// would rather over-cover by a token than leak a surname.
func TestMergeOverlapping_UnionBoundsSameType(t *testing.T) {
	in := []analyzer.RecognizerResult{
		{Start: 10, End: 14, Score: 0.55, EntityType: "PERSON", RecognizerName: "GLiNERRecognizer"},  // "Jane"
		{Start: 10, End: 18, Score: 0.62, EntityType: "PERSON", RecognizerName: "GLiNERRecognizer"},  // "Jane Doe"; wider, higher score
	}
	out := mergeOverlapping(in)
	if len(out) != 1 {
		t.Fatalf("expected 1 merged span, got %d: %+v", len(out), out)
	}
	if out[0].Start != 10 || out[0].End != 18 {
		t.Fatalf("union bounds: want [10,18), got [%d,%d)", out[0].Start, out[0].End)
	}
	if out[0].Score < 0.6199 || out[0].Score > 0.6201 {
		t.Fatalf("kept score: want ~0.62 (higher member), got %v", out[0].Score)
	}
}

// TestMergeOverlapping_DifferentTypesStaySeparate guards the
// type-aware contract: a PERSON span and an ORGANIZATION span covering
// the same byte range must NOT be merged here. The downstream
// RemoveConflicts resolver handles cross-type arbitration via the
// NER-preferred rule; flattening it here would silently lose evidence
// the resolver needs.
func TestMergeOverlapping_DifferentTypesStaySeparate(t *testing.T) {
	in := []analyzer.RecognizerResult{
		{Start: 0, End: 9, Score: 0.5, EntityType: "PERSON", RecognizerName: "GLiNERRecognizer"},
		{Start: 0, End: 9, Score: 0.6, EntityType: "ORGANIZATION", RecognizerName: "GLiNERRecognizer"},
	}
	out := mergeOverlapping(in)
	if len(out) != 2 {
		t.Fatalf("expected 2 spans (no cross-type merge), got %d: %+v", len(out), out)
	}
}

// TestMergeOverlapping_DisjointSpansBothKept covers the "different
// member found a span the other didn't" case; the core recall-stacking
// win. Both spans must survive.
func TestMergeOverlapping_DisjointSpansBothKept(t *testing.T) {
	in := []analyzer.RecognizerResult{
		{Start: 0, End: 5, Score: 0.5, EntityType: "PERSON", RecognizerName: "GLiNERRecognizer"},
		{Start: 100, End: 110, Score: 0.7, EntityType: "PERSON", RecognizerName: "GLiNERRecognizer"},
	}
	out := mergeOverlapping(in)
	if len(out) != 2 {
		t.Fatalf("expected 2 spans (disjoint), got %d: %+v", len(out), out)
	}
}

// TestEnsembleFromEnv_UnsetReturnsNilNil pins the silent-fallback
// contract: when ANONDE_NER_STACK is unset, the caller MUST fall
// through to the existing single-model path. (nil, nil) is the only
// return shape that does this; (nil, err) would Fatalf in main, and
// (non-nil, nil) would override the single-model path.
func TestEnsembleFromEnv_UnsetReturnsNilNil(t *testing.T) {
	t.Setenv("ANONDE_NER_STACK", "")
	ens, err := EnsembleFromEnv(0.40, "")
	if err != nil {
		t.Fatalf("unset env should not error, got %v", err)
	}
	if ens != nil {
		t.Fatalf("unset env should return nil ensemble, got %+v", ens)
	}
}

// TestEnsembleFromEnv_MalformedErrors pins the "deployer typo loud
// failure" contract. ",,," after trim has zero usable IDs.
func TestEnsembleFromEnv_MalformedErrors(t *testing.T) {
	t.Setenv("ANONDE_NER_STACK", ",, ,,")
	ens, err := EnsembleFromEnv(0.40, "")
	if err == nil {
		t.Fatalf("malformed env should error, got ensemble=%+v", ens)
	}
	if ens != nil {
		t.Fatalf("malformed env should not return an ensemble, got %+v", ens)
	}
}

// TestEnsembleFromEnv_HappyPathConstructsMembers verifies the env list
// is parsed and each non-empty entry becomes a member. Doesn't run
// Analyze (no ONNX session creation); just checks the constructor's
// shape.
func TestEnsembleFromEnv_HappyPathConstructsMembers(t *testing.T) {
	t.Setenv("ANONDE_NER_STACK", " knowledgator/gliner-pii-base-v1.0 , knowledgator/gliner-pii-large-v1.0 ")
	ens, err := EnsembleFromEnv(0.40, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ens == nil {
		t.Fatalf("expected ensemble, got nil")
	}
	if len(ens.members) != 2 || len(ens.memberIDs) != 2 {
		t.Fatalf("expected 2 members, got %d / ids=%v", len(ens.members), ens.memberIDs)
	}
	if ens.Name() != "GLiNEREnsembleNERRecognizer" {
		t.Fatalf("Name() must end in NERRecognizer for DisableNER, got %q", ens.Name())
	}
}

// TestEnsembleFromEnv_ParallelEnv pins ANONDE_NER_STACK_PARALLEL=1
// flipping the parallel bit. Any other value keeps sequential as the
// safe default (bounded peak RAM).
func TestEnsembleFromEnv_ParallelEnv(t *testing.T) {
	t.Setenv("ANONDE_NER_STACK", "knowledgator/gliner-pii-base-v1.0")
	t.Setenv("ANONDE_NER_STACK_PARALLEL", "1")
	ens, err := EnsembleFromEnv(0.40, "")
	if err != nil || ens == nil {
		t.Fatalf("EnsembleFromEnv: err=%v ens=%v", err, ens)
	}
	if !ens.parallel {
		t.Fatalf("ANONDE_NER_STACK_PARALLEL=1 should enable parallel dispatch")
	}

	t.Setenv("ANONDE_NER_STACK_PARALLEL", "yes")
	ens2, _ := EnsembleFromEnv(0.40, "")
	if ens2.parallel {
		t.Fatalf("ANONDE_NER_STACK_PARALLEL=%q (not '1') should stay sequential", os.Getenv("ANONDE_NER_STACK_PARALLEL"))
	}
}
