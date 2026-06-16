package analyzer

import (
	"context"
	"fmt"
	"strings"
)

// nerVerifyProbe is the tiny PII-dense fixture each NER backend must Analyze
// cleanly at boot. We assert no init/load/inference error, NOT that a specific
// span comes back — one-fixture recall is brittle. A failed-to-load model is
// the silent fallback this guard catches; low recall is a separate tuning problem.
const nerVerifyProbe = "John Smith works at Mercy Hospital."

// isNERRecognizerByName reports whether a name belongs to a model-backed NER
// recognizer. Broader than isNERBasedRecognizer (the DisableNER suffix check):
// it must ALSO catch the pool wrappers (GLiNERPool / GLiNERFlatPool) whose
// Name() does not end in "NERRecognizer".
func isNERRecognizerByName(name string) bool {
	return strings.HasSuffix(name, "NERRecognizer") || strings.HasPrefix(name, "GLiNER")
}

// VerifyNERBackend probes each model-backed NER recognizer DIRECTLY (not via
// engine.Analyze) and fails closed if any cannot Analyze cleanly. It exists to
// catch the silent-fallback bug class: AnalyzerEngine.Analyze drops a
// per-recognizer error as long as one recognizer succeeded, so a GLiNER
// recognizer that cannot load (missing model/ONNX/libonnxruntime/tokenizer)
// becomes a swallowed log line while every doc falls back to patterns-only and
// PERSON/ORG/LOCATION leaks with err == nil. That tolerance is right for a
// transient per-doc failure but wrong for a NER backend that was explicitly
// requested and produces nothing — so callers that selected one (the server's
// analyzerFromEnv, the bench runner's --backend gliner*) call this once at boot.
// Probing directly is what keeps the pattern recognizers' success from masking
// the NER failure. Returns nil for a patterns-only engine, so it is safe to
// call unconditionally.
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
		// Only the error matters; an empty result is legitimate (nothing
		// above threshold on the probe).
		if _, err := rec.Analyze(ctx, nerVerifyProbe, nil, ""); err != nil {
			return fmt.Errorf("NER backend %q failed verification (would silently fall back to patterns-only): %w", rec.Name(), err)
		}
	}
	_ = checked
	return nil
}

// HasNERRecognizer reports whether the engine has any model-backed NER
// recognizer. Lets callers tell VerifyNERBackend's "verified" silence apart
// from "nothing to verify" — e.g. the bench runner must fail if it asked for a
// gliner backend but got zero NER recognizers (a non-ner binary mislabelled
// as a gliner cell).
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
