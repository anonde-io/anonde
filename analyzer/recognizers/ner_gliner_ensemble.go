//go:build hugot

// ner_gliner_ensemble.go implements multi-model GLiNER stacking: run
// several GLiNER members in-process and OR-merge their spans so each
// model's blind spots are covered by the others. The result is one
// EntityRecognizer (RecognizerName "GLiNEREnsembleNERRecognizer") so the
// rest of the pipeline sees a normal NER recognizer.
//
// `-tags hugot` because every member is a real GLiNERRecognizer. Each
// member's Analyze() is internally serialised by its own mutex; members
// run sequentially by default to bound steady-state memory (each holds an
// ONNX session resident). Opt into parallel dispatch via
// ANONDE_NER_STACK_PARALLEL=1 when latency matters more than RAM.

package recognizers

import (
	"context"
	"fmt"
	"log"
	"os"
	"sort"
	"strings"
	"sync"

	"github.com/anonde-io/anonde/analyzer"
)

// envNERStack is the comma-separated list of GLiNER model IDs that
// compose the ensemble. Empty / unset → single-model path (no
// behaviour change from current production).
const envNERStack = "ANONDE_NER_STACK"

// envNERStackParallel is the opt-in flag for parallel member dispatch.
// Default is sequential ("1" → parallel; any other value → sequential).
// Parallel halves wall-clock latency on a 2-member ensemble at the cost
// of holding both ONNX sessions in resident memory simultaneously.
const envNERStackParallel = "ANONDE_NER_STACK_PARALLEL"

// defaultEnsembleOnnxFile is the per-member ONNX file used when the caller
// doesn't supply one. FP32, not INT8: INT8 uniformly depresses recall
// (Σ ALL leak 20.7% FP32 vs 26.6% INT8 across 30 corpora), and pairing two
// depressed-logit members would undo the stacking gain.
const defaultEnsembleOnnxFile = "onnx/model.onnx"

// EnsembleGLiNERRecognizer is an EntityRecognizer that fans Analyze() out
// to N independent GLiNERRecognizer members and OR-merges their spans.
// Each member owns its onnxruntime session; the cost is one resident
// session per member, the win is recall stacking. Construction is cheap;
// Analyze() triggers each member's lazy init on first call.
type EnsembleGLiNERRecognizer struct {
	members []*GLiNERRecognizer

	// initOnce gates the one-shot ensemble-level setup log line.
	initOnce sync.Once

	// parallel selects goroutine-per-member dispatch. Set from
	// ANONDE_NER_STACK_PARALLEL at construction; not mutable after.
	parallel bool

	// memberIDs is the model-ID list, preserved for logs. members[i] is
	// the recognizer for memberIDs[i].
	memberIDs []string
}

// NewEnsembleGLiNERRecognizer constructs an ensemble across the given
// HuggingFace model IDs. Each member shares the supplied threshold + ortLib
// + the FP32 ONNX path, and the default chat-PII label space (so the
// OR-merge compares like with like). threshold == 0 selects the per-member
// default. ortLib is forwarded to every member, though only the first to
// call ort.InitializeEnvironment() decides the process-wide library path.
func NewEnsembleGLiNERRecognizer(modelIDs []string, threshold float64, ortLib string, spanFilter ...SpanFilterConfig) *EnsembleGLiNERRecognizer {
	// Optional span-shape filter applied to every member; variadic so
	// existing callers stay source-compatible.
	var sf SpanFilterConfig
	if len(spanFilter) > 0 {
		sf = spanFilter[0]
	}
	members := make([]*GLiNERRecognizer, 0, len(modelIDs))
	ids := make([]string, 0, len(modelIDs))
	for _, id := range modelIDs {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		cfg := GLiNERConfig{
			ModelName:         id,
			OnnxFilePath:      defaultEnsembleOnnxFile,
			AutoDownload:      true,
			Threshold:         threshold,
			SharedLibraryPath: ortLib,
			SpanFilter:        sf,
			// Labels/LabelToEntity left empty → shared default label space,
			// which is what makes the OR-merge well-defined.
		}
		members = append(members, NewGLiNERRecognizer(cfg))
		ids = append(ids, id)
	}
	parallel := strings.TrimSpace(os.Getenv(envNERStackParallel)) == "1"
	return &EnsembleGLiNERRecognizer{
		members:   members,
		memberIDs: ids,
		parallel:  parallel,
	}
}

// EnsembleFromEnv builds an ensemble from ANONDE_NER_STACK. Returns
// (nil, nil) when unset (caller falls through to the single-model path),
// (ensemble, nil) when it lists ≥1 model ID, and (nil, error) when set but
// empty after trim (e.g. ",,,") — a deployer typo surfaced loudly at boot
// rather than silently booting with no NER.
func EnsembleFromEnv(threshold float64, ortLib string, spanFilter ...SpanFilterConfig) (*EnsembleGLiNERRecognizer, error) {
	raw := strings.TrimSpace(os.Getenv(envNERStack))
	if raw == "" {
		return nil, nil
	}
	parts := strings.Split(raw, ",")
	ids := make([]string, 0, len(parts))
	for _, p := range parts {
		if id := strings.TrimSpace(p); id != "" {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return nil, fmt.Errorf("%s=%q has no usable model IDs after trim", envNERStack, raw)
	}
	r := NewEnsembleGLiNERRecognizer(ids, threshold, ortLib, spanFilter...)
	// Log so deployers can confirm the ensemble is wired; a silent fallback
	// to the single-model path is the highest-cost bug class.
	log.Printf("gliner-ensemble: configured with %d members (parallel=%v): %s",
		len(r.members), r.parallel, strings.Join(r.memberIDs, ", "))
	return r, nil
}

// Name returns the recognizer name. MUST end in "NERRecognizer" so the
// analyzer engine's DisableNER suffix-check fires (mirrors
// GLiNERRecognizer / HugotNERRecognizer naming convention).
func (e *EnsembleGLiNERRecognizer) Name() string {
	return "GLiNEREnsembleNERRecognizer"
}

// SupportedEntities returns the canonical entity types the ensemble emits.
// All members share the default label map, so this forwards to member[0];
// per-member label overrides would require a real union here.
func (e *EnsembleGLiNERRecognizer) SupportedEntities() []string {
	if len(e.members) == 0 {
		return nil
	}
	return e.members[0].SupportedEntities()
}

// SupportedLanguages forwards to member[0]; all members report the same
// language set today. A member with a different set would need a union.
func (e *EnsembleGLiNERRecognizer) SupportedLanguages() []string {
	if len(e.members) == 0 {
		return nil
	}
	return e.members[0].SupportedLanguages()
}

// Analyze runs every member on the same input, OR-merges their spans, and
// returns the union (a span any one member found is kept).
//
// Error handling: one bad model must not kill the batch. Each member runs
// under a deferred recover() and a per-member error is logged and skipped.
// Only "all members errored" escalates — so the CI canary catches a fully
// dead ensemble, while a degraded one still ships partial redaction.
// Sequential by default; ANONDE_NER_STACK_PARALLEL=1 spins one goroutine
// per member.
func (e *EnsembleGLiNERRecognizer) Analyze(ctx context.Context, text string, entities []string, lang string) ([]analyzer.RecognizerResult, error) {
	if len(e.members) == 0 {
		return nil, fmt.Errorf("gliner-ensemble: no members configured (set %s)", envNERStack)
	}

	e.initOnce.Do(func() {
		log.Printf("gliner-ensemble: first Analyze (text_bytes=%d members=%d parallel=%v)",
			len(text), len(e.members), e.parallel)
	})

	type memberOut struct {
		spans []analyzer.RecognizerResult
		err   error
		id    string
	}

	results := make([]memberOut, len(e.members))

	runOne := func(idx int) {
		out := memberOut{id: e.memberIDs[idx]}
		// Per-member panic-recover: a panic in a parallel-path goroutine
		// would otherwise tear the whole process down.
		defer func() {
			if rec := recover(); rec != nil {
				out.err = fmt.Errorf("gliner-ensemble: member %q panic during analyze: %v", e.memberIDs[idx], rec)
				results[idx] = out
			}
		}()
		spans, err := e.members[idx].Analyze(ctx, text, entities, lang)
		out.spans = spans
		out.err = err
		results[idx] = out
	}

	if e.parallel {
		var wg sync.WaitGroup
		wg.Add(len(e.members))
		for i := range e.members {
			i := i
			go func() {
				defer wg.Done()
				runOne(i)
			}()
		}
		wg.Wait()
	} else {
		for i := range e.members {
			runOne(i)
		}
	}

	// Tolerate per-member errors as long as one member succeeded;
	// all-errored escalates so the silent-fallback canary fires.
	merged := make([]analyzer.RecognizerResult, 0, 32)
	successCount := 0
	var lastErr error
	for _, mo := range results {
		if mo.err != nil {
			log.Printf("gliner-ensemble: member %q errored (swallowed, other members continue): %v", mo.id, mo.err)
			lastErr = mo.err
			continue
		}
		successCount++
		merged = append(merged, mo.spans...)
	}
	if successCount == 0 {
		return nil, fmt.Errorf("gliner-ensemble: all %d members failed; last error: %w", len(e.members), lastErr)
	}

	return mergeOverlapping(merged), nil
}

// mergeOverlapping is the OR-merge across ensemble member spans. A
// candidate overlapping an accepted span of the SAME type unions their
// byte bounds and promotes the higher-scoring member's score+name;
// otherwise it is accepted as new. Different-type overlaps are kept
// SEPARATE so the downstream RemoveConflicts resolver sees the
// disagreement and applies the NER-preferred rule. Output is sorted by
// (start, end) for deterministic snapshots. O(n^2) worst case, fine for
// document-sized inputs.
func mergeOverlapping(results []analyzer.RecognizerResult) []analyzer.RecognizerResult {
	if len(results) == 0 {
		return results
	}
	sort.Slice(results, func(i, j int) bool {
		if results[i].Start != results[j].Start {
			return results[i].Start < results[j].Start
		}
		return results[i].End < results[j].End
	})

	accepted := make([]analyzer.RecognizerResult, 0, len(results))
	for _, r := range results {
		mergedIdx := -1
		for i := range accepted {
			s := accepted[i]
			if s.EntityType != r.EntityType {
				continue
			}
			if r.Start < s.End && r.End > s.Start {
				mergedIdx = i
				break
			}
		}
		if mergedIdx < 0 {
			accepted = append(accepted, r)
			continue
		}
		s := &accepted[mergedIdx]
		// Union-bounds: the wider span wins (recall > precision; an
		// under-redacted token leaks PII).
		if r.Start < s.Start {
			s.Start = r.Start
		}
		if r.End > s.End {
			s.End = r.End
		}
		// Promote the higher-scoring member's score+name; downstream
		// RemoveConflicts uses the score as its cross-type tiebreaker.
		if r.Score > s.Score {
			s.Score = r.Score
			s.RecognizerName = r.RecognizerName
		}
	}
	return accepted
}
