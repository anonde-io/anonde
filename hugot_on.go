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
//               when empty).
// autoDownload — fetch the model from HuggingFace Hub on first use.
func DefaultAnalyzerEngineWithHugot(modelsDir, modelName string, autoDownload bool) *analyzer.AnalyzerEngine {
	registry := analyzer.NewRecognizerRegistry()
	registry.Add(recognizers.NewHugotNERRecognizer(recognizers.HugotNERConfig{
		ModelsDir:    modelsDir,
		ModelName:    modelName,
		AutoDownload: autoDownload,
	}))
	registry.Add(patternRecognizers()...)
	return analyzer.NewAnalyzerEngine(registry)
}
