package analyzer

import (
	"context"
	"fmt"
	"strings"
)

// nerVerifyProbe is the trivial fixture every NER backend must produce a
// clean (error-free) Analyze on at boot. It is deliberately PII-dense
// ("John Smith" PERSON, "Mercy Hospital" ORG/LOCATION) and tiny so the
// check costs one inference, not a corpus pass. We do NOT assert that a
// specific span comes back; model recall on one fixture is brittle and
// not the property under test. The property is: "the NER recognizer ran
// without an init / load / inference error". A loaded-but-low-recall
// model is a tuning problem; a failed-to-load model is the silent
// fallback this guard exists to catch.
const nerVerifyProbe = "John Smith works at Mercy Hospital."

// isNERRecognizerByName reports whether a recognizer name belongs to a
// model-backed NER recognizer. It is broader than isNERBasedRecognizer
// (which keys the DisableNER suffix check) because it must ALSO catch
// the pool wrappers (GLiNERPool / GLiNERFlatPool) whose Name() does not
// end in "NERRecognizer". Used only by VerifyNERBackend, which needs to
// find the NER slot regardless of whether it's a bare recognizer, an
// ensemble, or a pool.
func isNERRecognizerByName(name string) bool {
	return strings.HasSuffix(name, "NERRecognizer") || strings.HasPrefix(name, "GLiNER")
}

// VerifyNERBackend runs one trivial Analyze against EACH model-backed NER
// recognizer in the engine's registry, in isolation, and returns an error
// if any of them fails to produce output cleanly.
//
// WHY THIS EXISTS (the silent-fallback bug class)
// ------------------------------------------------
// AnalyzerEngine.Analyze is deliberately tolerant: a per-recognizer error
// is logged ("analyzer: recognizer error (swallowed)") and dropped as long
// as at least one recognizer succeeded. With ~51 pattern recognizers that
// always succeed, a GLiNER recognizer that CANNOT LOAD (missing model file,
// unopenable ONNX session, missing libonnxruntime, missing tokenizer) turns
// into a log line nobody gates on while every document silently falls back
// to patterns-only. PERSON / ORG / LOCATION PII then leaks with err == nil,
// in CI and in production.
//
// That tolerance is correct for a TRANSIENT per-doc failure (one bad chunk
// must not erase pattern findings). It is wrong for a NER backend that was
// explicitly requested but is not actually producing spans at all. This
// function is the boot-time guard for the second case: callers that
// explicitly selected a NER backend (the server's analyzerFromEnv, the
// bench runner's --backend gliner*) call it once and fail closed.
//
// It probes each NER recognizer DIRECTLY (not via engine.Analyze) precisely
// so the pattern recognizers' success cannot mask the NER failure.
//
// Returns nil when the registry contains no model-backed NER recognizer
// (a patterns-only engine), so it is safe to call unconditionally.
func VerifyNERBackend(ctx context.Context, e *AnalyzerEngine) error {
	if e == nil || e.Registry == nil {
		return nil
	}
	var checked int
	for _, rec := range e.Registry.All() {
		if !isNERRecognizerByName(rec.Name()) {
			continue
		}
		checked++
		// entities=nil → "all supported", language="" → recognizer
		// default. We only care that Analyze returns without error;
		// the result slice may legitimately be empty if the model
		// finds nothing above threshold on the probe.
		if _, err := rec.Analyze(ctx, nerVerifyProbe, nil, ""); err != nil {
			return fmt.Errorf("NER backend %q failed verification (would silently fall back to patterns-only): %w", rec.Name(), err)
		}
	}
	_ = checked
	return nil
}

// HasNERRecognizer reports whether the engine has at least one
// model-backed NER recognizer registered. Callers use it to decide
// whether VerifyNERBackend's silence means "verified" vs "nothing to
// verify"; e.g. the bench runner must fail if it asked for a gliner
// backend but the registry came back with zero NER recognizers (a
// non-hugot binary mislabelled as a gliner cell).
func HasNERRecognizer(e *AnalyzerEngine) bool {
	if e == nil || e.Registry == nil {
		return false
	}
	for _, rec := range e.Registry.All() {
		if isNERRecognizerByName(rec.Name()) {
			return true
		}
	}
	return false
}
