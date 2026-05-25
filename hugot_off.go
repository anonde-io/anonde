//go:build !hugot

package anonde

import (
	"log"

	"github.com/anonde-io/anonde/analyzer"
	"github.com/anonde-io/anonde/analyzer/recognizers"
)

// DefaultAnalyzerEngineWithHugot is a fail-fast stub used when the binary
// is built **without** the `hugot` tag. The real hugot backend, its ONNX
// runtime, and the tokenizer wrappers are not compiled in — calling this
// terminates the process with an actionable message rather than returning
// a partially functional engine.
//
// Why log.Fatalf and not an error return: this function is invoked once,
// at process startup, when the operator has explicitly set
// ANALYZER_BACKEND=hugot. Failing immediately with a clear remediation
// step is better than booting a half-broken server that silently misses
// PII because NER never runs.
func DefaultAnalyzerEngineWithHugot(_, _ string, _ bool) *analyzer.AnalyzerEngine {
	log.Fatalf("hugot backend not available: this binary was built without -tags hugot. " +
		"Rebuild with `go build -tags hugot ./cmd/anonde` for the NER variant, " +
		"or use ANALYZER_BACKEND=patterns / ollama instead.")
	return nil // unreachable; log.Fatalf calls os.Exit
}

// DefaultAnalyzerEngineWithHugotConfig is the fail-fast stub for the
// HugotNERConfig-taking variant. Same rationale as DefaultAnalyzerEngineWithHugot.
func DefaultAnalyzerEngineWithHugotConfig(_ recognizers.HugotNERConfig) *analyzer.AnalyzerEngine {
	log.Fatalf("hugot backend not available: this binary was built without -tags hugot. " +
		"Rebuild with `go build -tags hugot ./cmd/anonde` for the NER variant, " +
		"or use ANALYZER_BACKEND=patterns / ollama instead.")
	return nil
}

// DefaultAnalyzerEngineWithGLiNERConfig is the fail-fast stub for the
// Go-native GLiNER recognizer. Same rationale as
// DefaultAnalyzerEngineWithHugot — the real implementation needs CGO
// onnxruntime + tokenizers (both pulled in by the `hugot` build tag).
func DefaultAnalyzerEngineWithGLiNERConfig(_ recognizers.GLiNERConfig) *analyzer.AnalyzerEngine {
	log.Fatalf("gliner backend not available: this binary was built without -tags hugot. " +
		"Rebuild with `go build -tags hugot ./...` for the GLiNER variant, " +
		"or use ANALYZER_BACKEND=patterns / ollama / hugot instead.")
	return nil
}

// DefaultAnalyzerEngineWithGLiNERFlatConfig is the fail-fast stub for the
// flat-decoder GLiNER recognizer. Same rationale as
// DefaultAnalyzerEngineWithGLiNERConfig — the real implementation needs
// CGO onnxruntime + tokenizers (both pulled in by the `hugot` build tag).
func DefaultAnalyzerEngineWithGLiNERFlatConfig(_ recognizers.GLiNERConfig) *analyzer.AnalyzerEngine {
	log.Fatalf("gliner-flat backend not available: this binary was built without -tags hugot. " +
		"Rebuild with `go build -tags hugot ./...` for the GLiNER flat-decoder variant, " +
		"or use ANALYZER_BACKEND=patterns / ollama / hugot instead.")
	return nil
}

// DefaultAnalyzerEngineWithGLiNEREnsemble is the fail-fast stub for the
// GLiNER ensemble recognizer. Same rationale as
// DefaultAnalyzerEngineWithGLiNERConfig — the real implementation needs
// `-tags hugot`.
func DefaultAnalyzerEngineWithGLiNEREnsemble(_ *recognizers.EnsembleGLiNERRecognizer) *analyzer.AnalyzerEngine {
	log.Fatalf("gliner-ensemble backend not available: this binary was built without -tags hugot. " +
		"Rebuild with `go build -tags hugot ./...` for the GLiNER ensemble variant, " +
		"or unset ANONDE_NER_STACK to use the single-model path.")
	return nil
}

// DefaultAnalyzerEngineWithGLiNERPool is the fail-fast stub for the
// span-decoder GLiNER pool. Same rationale as
// DefaultAnalyzerEngineWithGLiNERConfig — the real implementation needs
// `-tags hugot`.
func DefaultAnalyzerEngineWithGLiNERPool(_ *recognizers.GLiNERPool) *analyzer.AnalyzerEngine {
	log.Fatalf("gliner pool not available: this binary was built without -tags hugot. " +
		"Rebuild with `go build -tags hugot ./...` for the GLiNER pool, " +
		"or unset GLINER_POOL_SIZE to use the single-recognizer path.")
	return nil
}

// DefaultAnalyzerEngineWithGLiNERFlatPool is the fail-fast stub for the
// flat-decoder GLiNER pool. Same rationale as
// DefaultAnalyzerEngineWithGLiNERConfig — the real implementation needs
// `-tags hugot`.
func DefaultAnalyzerEngineWithGLiNERFlatPool(_ *recognizers.GLiNERFlatPool) *analyzer.AnalyzerEngine {
	log.Fatalf("gliner flat pool not available: this binary was built without -tags hugot. " +
		"Rebuild with `go build -tags hugot ./...` for the GLiNER flat pool, " +
		"or unset GLINER_POOL_SIZE / ANONDE_GLINER_FLAT_POOL_SIZE to use the single-recognizer path.")
	return nil
}
