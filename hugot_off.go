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
		"Rebuild with `go build -tags hugot ./cmd/platform` for the NER variant, " +
		"or use ANALYZER_BACKEND=patterns / ollama instead.")
	return nil // unreachable; log.Fatalf calls os.Exit
}

// DefaultAnalyzerEngineWithHugotConfig is the fail-fast stub for the
// HugotNERConfig-taking variant. Same rationale as DefaultAnalyzerEngineWithHugot.
func DefaultAnalyzerEngineWithHugotConfig(_ recognizers.HugotNERConfig) *analyzer.AnalyzerEngine {
	log.Fatalf("hugot backend not available: this binary was built without -tags hugot. " +
		"Rebuild with `go build -tags hugot ./cmd/platform` for the NER variant, " +
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
