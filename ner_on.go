//go:build ner

package anonde

import (
	"github.com/anonde-io/anonde/analyzer"
	"github.com/anonde-io/anonde/analyzer/recognizers"
)

// DefaultAnalyzerEngineWithGLiNERConfig wires a Go-native GLiNER
// recognizer into the standard pattern-recognizer registry. GLiNER is
// an open-set NER architecture: the label list is supplied at
// inference time, not baked into the model weights. This constructor
// drives the same model that bench/runners/gliner.py uses through
// the Python sidecar; same prompt format, same canonical-entity
// mapping; but entirely in-process.
//
// Real implementation only; ner_off.go's stub log.Fatalfs.
//
// Typical config: zero-value GLiNERConfig selects
// knowledgator/gliner-pii-base-v1.0 with the default PII label set.
// Override Labels / LabelToEntity for a custom open-set vocabulary.
func DefaultAnalyzerEngineWithGLiNERConfig(cfg recognizers.GLiNERConfig) *analyzer.AnalyzerEngine {
	registry := analyzer.NewRecognizerRegistry()
	registry.Add(recognizers.NewGLiNERRecognizer(cfg))
	registry.Add(patternRecognizers()...)
	return analyzer.NewAnalyzerEngine(registry)
}

// DefaultAnalyzerEngineWithGLiNERFlatConfig wires the flat-decoder
// (token-decoder) GLiNER recognizer into the standard pattern-recognizer
// registry. Mirror of DefaultAnalyzerEngineWithGLiNERConfig; same
// GLiNERConfig, same overall analyzer shape; but the NER slot uses
// NewGLiNERFlatRecognizer for models whose ONNX export takes 4 inputs
// and emits BIO-style start/end/inside logits (e.g.
// knowledgator/gliner-pii-large-v1.0). The span-decoder recognizer used
// by DefaultAnalyzerEngineWithGLiNERConfig cannot load these exports.
//
// Real implementation only; ner_off.go's stub log.Fatalfs.
func DefaultAnalyzerEngineWithGLiNERFlatConfig(cfg recognizers.GLiNERConfig) *analyzer.AnalyzerEngine {
	registry := analyzer.NewRecognizerRegistry()
	registry.Add(recognizers.NewGLiNERFlatRecognizer(cfg))
	registry.Add(patternRecognizers()...)
	return analyzer.NewAnalyzerEngine(registry)
}

// DefaultAnalyzerEngineWithGLiNEREnsemble wires a pre-built
// EnsembleGLiNERRecognizer into the standard pattern-recognizer
// registry. The ensemble itself is constructed by
// recognizers.NewEnsembleGLiNERRecognizer / EnsembleFromEnv, so this
// constructor is intentionally thin; the multi-model stacking logic
// lives in the ensemble file, and this is only the analyzer-engine
// glue.
//
// Real implementation only; ner_off.go's stub log.Fatalfs.
func DefaultAnalyzerEngineWithGLiNEREnsemble(ens *recognizers.EnsembleGLiNERRecognizer) *analyzer.AnalyzerEngine {
	registry := analyzer.NewRecognizerRegistry()
	registry.Add(ens)
	registry.Add(patternRecognizers()...)
	return analyzer.NewAnalyzerEngine(registry)
}

// DefaultAnalyzerEngineWithGLiNERPool wires a pre-built GLiNERPool
// (N parallel span-decoder GLiNER instances) into the standard
// pattern-recognizer registry. Mirror of
// DefaultAnalyzerEngineWithGLiNEREnsemble; the pool is constructed by
// recognizers.NewGLiNERPool, so this constructor is intentionally thin.
//
// The pool's Name() ("GLiNERPool") is registered in
// analyzer/result.go::nerRecognizerNames, so the conflict resolver's
// NER-preferred entity rule applies to pool findings exactly as it
// does to bare GLiNERRecognizer findings.
//
// Real implementation only; ner_off.go's stub log.Fatalfs.
func DefaultAnalyzerEngineWithGLiNERPool(pool *recognizers.GLiNERPool) *analyzer.AnalyzerEngine {
	registry := analyzer.NewRecognizerRegistry()
	registry.Add(pool)
	registry.Add(patternRecognizers()...)
	return analyzer.NewAnalyzerEngine(registry)
}

// DefaultAnalyzerEngineWithGLiNERFlatPool wires a pre-built
// GLiNERFlatPool (N parallel flat-decoder GLiNER instances) into the
// standard pattern-recognizer registry. Mirror of
// DefaultAnalyzerEngineWithGLiNERPool for the flat / token decoder
// path used by `knowledgator/gliner-pii-large-v1.0` and other 4-input
// BIO ONNX exports.
//
// The pool's Name() ("GLiNERFlatPool") is registered in
// analyzer/result.go::nerRecognizerNames, so the conflict resolver's
// NER-preferred entity rule applies to pool findings exactly as it
// does to bare GLiNERFlatRecognizer findings.
//
// Real implementation only; ner_off.go's stub log.Fatalfs.
func DefaultAnalyzerEngineWithGLiNERFlatPool(pool *recognizers.GLiNERFlatPool) *analyzer.AnalyzerEngine {
	registry := analyzer.NewRecognizerRegistry()
	registry.Add(pool)
	registry.Add(patternRecognizers()...)
	return analyzer.NewAnalyzerEngine(registry)
}
