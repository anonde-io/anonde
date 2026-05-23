//go:build hugot

// ner_gliner_ensemble.go implements multi-model GLiNER stacking: run
// multiple GLiNER variants in the same process and OR-merge their span
// outputs so each model's blind spots are covered by the others.
//
// The single-model FP32 number is the *lower bound* of an ensemble — each
// member has different blind spots; the union approaches 100% recall.
// This file is the wiring that makes that union shape directly visible
// to the analyzer engine (one EntityRecognizer that returns merged
// spans), so the rest of the pipeline (RemoveConflicts, anonymizer,
// vault) sees a normal NER recognizer with the standard
// `RecognizerName: "GLiNEREnsembleNERRecognizer"` tag.
//
// Naming
// ------
// Name() ends in "NERRecognizer" so the analyzer engine's
// DisableNER suffix-check fires (mirrors GLiNERRecognizer /
// HugotNERRecognizer). The base name is "GLiNEREnsemble" — adding it to
// analyzer.nerRecognizerNames in a follow-up CL lets the
// NER-preferred conflict-resolver path (PERSON / ORG / LOC / AGE /
// PROFESSION / NRP) apply to ensemble findings too. Until that CL lands,
// ensemble spans still resolve correctly because they carry the
// per-member RecognizerName == "GLiNERRecognizer" only when the
// downstream is reading `kept.RecognizerName` after this file's merge
// — see mergeOverlapping below, which preserves a member's name (always
// "GLiNERRecognizer") on the winning span, so isNERRecognizer remains
// true for downstream conflict resolution.
//
// Build tag
// ---------
// `-tags hugot` because every member is a real GLiNERRecognizer, which
// is itself `-tags hugot`. Default builds keep the existing
// fail-fast stub for the single-model path (ner_gliner_off.go) and
// never link this file in.
//
// Concurrency
// -----------
// Each member's Analyze() is internally serialised by its own
// recognizer-level mutex (see ner_gliner.go: `r.mu.Lock()` across the
// entire runChunk). Running two members in parallel is therefore safe
// across members — each owns an independent ONNX session — but pays
// 2× peak RAM for the second session being resident at the same time.
// Default is sequential to bound steady-state memory; opt into parallel
// via ANONDE_NER_STACK_PARALLEL=1 when latency matters more than RAM.

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

// defaultEnsembleOnnxFile is the per-member ONNX file used when the
// caller doesn't supply one. FP32 is the production default — the
// 2026-05 fp32-vs-int8 probe proved INT8 uniformly depresses recall
// (Σ ALL leak 20.7% FP32 vs 26.6% INT8 across 30 corpora). Ensembles
// stack recall: pairing two INT8 models with the same depressed-logit
// fingerprint would undo half the recall gain we get from stacking, so
// the default insists on FP32 per member. Override via
// EnsembleMemberConfig.OnnxFilePath if a member needs a specific path.
const defaultEnsembleOnnxFile = "onnx/model.onnx"

// EnsembleGLiNERRecognizer is an EntityRecognizer that fans Analyze()
// out to N independent GLiNERRecognizer members and OR-merges their
// spans. Each member is a full GLiNERRecognizer with its own onnxruntime
// session — the cost is one resident session per member, the win is
// recall stacking (the matrix Σ ALL leak rate drops well below the
// single-model FP32 number; see the roadmap for projected ranges).
//
// Construction is cheap; Analyze() triggers each member's lazy init on
// first call (matches GLiNERRecognizer's behaviour). If you want all
// members hot before traffic arrives, fire one parallel warmup call
// after construction.
type EnsembleGLiNERRecognizer struct {
	members []*GLiNERRecognizer

	// initOnce gates the one-shot ensemble-level setup log line. Member
	// init() is gated by each member's own sync.Once inside its
	// Analyze() path; this Once exists only so the ensemble-aggregate
	// log ("ensemble Analyze: members=…") fires exactly once even if
	// the first Analyze races against itself.
	initOnce sync.Once

	// parallel toggles between sequential and goroutine-per-member
	// dispatch in Analyze(). Set from ANONDE_NER_STACK_PARALLEL at
	// construction time; mutating after the recognizer is in service
	// is not supported.
	parallel bool

	// memberIDs is the list of model IDs the ensemble was built with,
	// preserved for log lines + diagnostics. members[i] is the
	// recognizer for memberIDs[i].
	memberIDs []string
}

// NewEnsembleGLiNERRecognizer constructs an ensemble across the given
// HuggingFace model IDs. Each member is a GLiNERRecognizer with the
// supplied threshold + ortLib + the FP32 ONNX path
// (`onnx/model.onnx`) — see defaultEnsembleOnnxFile for the rationale.
// Labels / LabelToEntity / MaxWidth / MaxTokens / chunking / MaxChunks
// fall through to GLiNERRecognizer defaults (DefaultPIILabels +
// DefaultLabelToEntity), so all members share the same label space and
// canonical mapping — necessary for the OR-merge to compare like with
// like.
//
// threshold == 0 selects the per-member default (defaultGLiNERThreshold,
// 0.40). Override per deployment via the same GLINER_THRESHOLD env
// var that the single-model path reads.
//
// ortLib is forwarded to every member's SharedLibraryPath. Note: only
// the FIRST member to call ort.InitializeEnvironment() decides the
// actual library path for the whole process — later members' values
// are silently ignored. This matches the documented contract in
// initOrtEnvironment().
//
// Passing zero model IDs is a programmer error — returns a recognizer
// that errors at Analyze-time, so the typo is caught loudly instead of
// silently degrading to "no NER coverage". Callers should funnel through
// EnsembleFromEnv() which validates the env-var format before reaching
// this constructor.
func NewEnsembleGLiNERRecognizer(modelIDs []string, threshold float64, ortLib string) *EnsembleGLiNERRecognizer {
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
			// Labels / LabelToEntity intentionally left empty → defaults.
			// Sharing the label space across members is what makes the
			// OR-merge well-defined.
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

// EnsembleFromEnv reads ANONDE_NER_STACK and returns an ensemble if it
// is set, or (nil, nil) if it is unset so callers can fall through to
// the existing single-model path with no behaviour change.
//
// Returns:
//   - (*EnsembleGLiNERRecognizer, nil) if ANONDE_NER_STACK contains at
//     least one non-empty model ID
//   - (nil, nil) if ANONDE_NER_STACK is unset or empty after trim
//   - (nil, error) if ANONDE_NER_STACK is set but, after trimming each
//     comma-separated entry, contains zero usable model IDs (e.g.
//     ",,," or "  "). This is a deployer-side typo — surface it loudly
//     at boot rather than silently booting with no NER.
//
// The caller is responsible for choosing the analyzer-engine wiring
// (registry.Add(...) etc.) — this constructor only builds the recognizer.
func EnsembleFromEnv(threshold float64, ortLib string) (*EnsembleGLiNERRecognizer, error) {
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
	r := NewEnsembleGLiNERRecognizer(ids, threshold, ortLib)
	// One-shot init log line so deployers can confirm the ensemble is
	// actually wired (and how) — the pii-engineer canary rule: silent
	// fallback to the single-model path is the highest-cost bug class.
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

// SupportedEntities returns the deduplicated set of canonical entity
// types the ensemble can emit. Because every member is constructed with
// empty Labels / LabelToEntity (i.e. defaults), all members share
// DefaultLabelToEntity — so the union is identical to member[0]'s
// SupportedEntities(). We forward to member[0] for clarity (no need to
// recompute a union of identical sets). If a future EnsembleMemberConfig
// API exposes per-member label overrides, this needs to become a real
// union.
func (e *EnsembleGLiNERRecognizer) SupportedEntities() []string {
	if len(e.members) == 0 {
		return nil
	}
	return e.members[0].SupportedEntities()
}

// SupportedLanguages: GLiNER PII models are multilingual; all members
// today report the same language set. We forward to member[0] for the
// same reason as SupportedEntities. If a member with a different
// language set is added (e.g. an English-only specialist), turn this
// into a real union.
func (e *EnsembleGLiNERRecognizer) SupportedLanguages() []string {
	if len(e.members) == 0 {
		return nil
	}
	return e.members[0].SupportedLanguages()
}

// Analyze runs every member on the same input, OR-merges their spans
// via mergeOverlapping, and returns the union. Recall stacking is the
// whole point — a span any one member found is kept; a span only one
// member found is still kept (that's the "blind spot covered" win).
//
// Per-member error handling
// -------------------------
// One bad model must not kill the batch. Each member's call is wrapped
// in a deferred recover() (mirrors the GLiNERRecognizer's own
// Analyze-level recover discipline), and a logged-but-skipped error
// from one member never aborts the others. This is the silent-fallback
// canary's most operationally meaningful corner: if "all members
// errored" we return the error so the canary in CI catches it, but if
// "1 of 2 members errored" we return the surviving spans and a log
// line so a degraded ensemble still ships SOME redaction (recall >
// precision; over-redaction is annoying, under-redaction is a leak).
//
// Concurrency
// -----------
// Sequential by default. ANONDE_NER_STACK_PARALLEL=1 spins one goroutine
// per member. Each member is internally locked so cross-member
// parallelism is the only available parallelism — but that's exactly
// what we want here.
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
		// Per-member panic-recover: one bad model can't kill the batch.
		// Mirrors GLiNERRecognizer.Analyze's own top-level recover —
		// belts-and-braces because a panic inside a goroutine in the
		// parallel path would otherwise tear the whole process down.
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

	// Collect; tolerate per-member errors as long as at least one
	// member produced spans (or completed without error). All-errored
	// is escalated — the silent-fallback bug class is precisely
	// "ensemble is supposed to be running but is actually returning
	// nothing".
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

// mergeOverlapping is the OR-merge across ensemble member spans.
// For each candidate span r:
//
//   - look for an existing accepted span s with the SAME entity type
//     AND overlapping byte range
//   - if found: union the byte bounds (s.Start = min, s.End = max) and
//     promote the higher-scoring member's score + recognizer name onto
//     the surviving span
//   - if not: accept r as a new span
//
// Spans of DIFFERENT types covering the same region are kept SEPARATE.
// The downstream RemoveConflicts resolver picks the winner using the
// NER-preferred rule from docs/ARCHITECTURE.md — letting it stay
// type-aware here means a member that finds "Acme Corp" as ORGANIZATION
// and another that finds the same span as LOCATION doesn't get its
// disagreement silently flattened; the conflict resolver gets to see
// both and apply its rule.
//
// Stability
// ---------
// Output is sorted by (start, end) for deterministic snapshots. Tests
// can rely on the order being stable across runs.
//
// Complexity
// ----------
// O(n^2) worst case (one accepted span list per type, linear scan per
// new span). For typical document sizes the accepted list is small
// (low-tens-of-spans-per-type), so this is fine. If it ever becomes a
// hotspot, partition by type-bucket and use an interval tree per type.
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
			// Overlap test (mirror of RecognizerResult.Overlaps):
			// non-empty intersection on the [Start, End) interval.
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
		// Union-bounds: the wider span wins because recall > precision.
		// A redactor that over-redacts by a token is fine; one that
		// under-redacts by a token leaks PII.
		if r.Start < s.Start {
			s.Start = r.Start
		}
		if r.End > s.End {
			s.End = r.End
		}
		// Promote the higher-scoring member onto the merged span. The
		// score is the "this is PII"-confidence signal; downstream
		// RemoveConflicts uses it as the tiebreaker when this span
		// overlaps a different-type span, so keeping the max keeps the
		// ensemble's strongest signal in the driver's seat.
		if r.Score > s.Score {
			s.Score = r.Score
			s.RecognizerName = r.RecognizerName
		}
	}
	return accepted
}
