//go:build hugot

package anonde

import (
	"github.com/moogacs/anonde/analyzer"
	"github.com/moogacs/anonde/analyzer/recognizers"
)

// DefaultAnalyzerEngineWithHugot returns an engine that uses a pre-trained
// ONNX transformer model (via hugot) for NER. Inference runs entirely
// in-process with no CGO or external service.
//
// This file is the **real** implementation, compiled only with
// `-tags hugot`. The default build uses hugot_off.go's stub, which
// log.Fatalfs to fail fast on misconfiguration.
//
// modelsDir   — local cache directory (defaults to ~/.cache/anonde/models).
// modelName   — HuggingFace model ID (recognizer-package default applies
//
//	when empty).
//
// autoDownload — fetch the model from HuggingFace Hub on first use.
func DefaultAnalyzerEngineWithHugot(modelsDir, modelName string, autoDownload bool) *analyzer.AnalyzerEngine {
	return DefaultAnalyzerEngineWithHugotConfig(recognizers.HugotNERConfig{
		ModelsDir:    modelsDir,
		ModelName:    modelName,
		AutoDownload: autoDownload,
	})
}

// DefaultAnalyzerEngineWithHugotConfig is the full-control variant of
// DefaultAnalyzerEngineWithHugot. Use this when the model needs a non-default
// OnnxFilePath (e.g. "onnx/model_quantized.onnx") or tuned chunking — the
// short-form constructor exposes only ModelsDir/ModelName/AutoDownload and
// silently misses these knobs.
//
// Typical bench use: probing alternative NER backends (GLiNER, ai4privacy
// variants) where the upstream repo ships multiple ONNX files and the
// default isn't the one you want.
func DefaultAnalyzerEngineWithHugotConfig(cfg recognizers.HugotNERConfig) *analyzer.AnalyzerEngine {
	registry := analyzer.NewRecognizerRegistry()
	registry.Add(recognizers.NewHugotNERRecognizer(cfg))
	registry.Add(patternRecognizers()...)
	return analyzer.NewAnalyzerEngine(registry)
}

// DefaultAnalyzerEngineWithGLiNERConfig wires a Go-native GLiNER
// recognizer into the standard pattern-recognizer registry. GLiNER is
// an open-set NER architecture: the label list is supplied at
// inference time, not baked into the model weights. This constructor
// drives the same model that bench/runner_gliner_pii.py uses through
// the Python sidecar — same prompt format, same canonical-entity
// mapping — but entirely in-process.
//
// Real implementation only; hugot_off.go's stub log.Fatalfs.
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
