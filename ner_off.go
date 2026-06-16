//go:build !ner

package anonde

import (
	"log"

	"github.com/anonde-io/anonde/analyzer"
	"github.com/anonde-io/anonde/analyzer/recognizers"
)

// DefaultAnalyzerEngineWithGLiNERConfig is the fail-fast stub for the
// Go-native GLiNER recognizer. The real implementation needs CGO
// onnxruntime + tokenizers (both pulled in by the `ner` build tag).
//
// Why log.Fatalf and not an error return: this function is invoked once,
// at process startup, when the operator has explicitly set a NER
// ANALYZER_BACKEND. Failing immediately with a clear remediation step is
// better than booting a half-broken server that silently misses PII
// because NER never runs.
func DefaultAnalyzerEngineWithGLiNERConfig(_ recognizers.GLiNERConfig) *analyzer.AnalyzerEngine {
	log.Fatalf("gliner backend not available: this binary was built without -tags ner. " +
		"Rebuild with `go build -tags ner ./...` for the GLiNER variant, " +
		"or use ANALYZER_BACKEND=patterns instead.")
	return nil // unreachable; log.Fatalf calls os.Exit
}

// DefaultAnalyzerEngineWithGLiNERFlatConfig is the fail-fast stub for the
// flat-decoder GLiNER recognizer. Same rationale as
// DefaultAnalyzerEngineWithGLiNERConfig; the real implementation needs
// CGO onnxruntime + tokenizers (both pulled in by the `ner` build tag).
func DefaultAnalyzerEngineWithGLiNERFlatConfig(_ recognizers.GLiNERConfig) *analyzer.AnalyzerEngine {
	log.Fatalf("gliner-flat backend not available: this binary was built without -tags ner. " +
		"Rebuild with `go build -tags ner ./...` for the GLiNER flat-decoder variant, " +
		"or use ANALYZER_BACKEND=patterns instead.")
	return nil
}

// DefaultAnalyzerEngineWithGLiNEREnsemble is the fail-fast stub for the
// GLiNER ensemble recognizer. Same rationale as
// DefaultAnalyzerEngineWithGLiNERConfig; the real implementation needs
// `-tags ner`.
func DefaultAnalyzerEngineWithGLiNEREnsemble(_ *recognizers.EnsembleGLiNERRecognizer) *analyzer.AnalyzerEngine {
	log.Fatalf("gliner-ensemble backend not available: this binary was built without -tags ner. " +
		"Rebuild with `go build -tags ner ./...` for the GLiNER ensemble variant, " +
		"or unset ANONDE_NER_STACK to use the single-model path.")
	return nil
}

// DefaultAnalyzerEngineWithGLiNERPool is the fail-fast stub for the
// span-decoder GLiNER pool. Same rationale as
// DefaultAnalyzerEngineWithGLiNERConfig; the real implementation needs
// `-tags ner`.
func DefaultAnalyzerEngineWithGLiNERPool(_ *recognizers.GLiNERPool) *analyzer.AnalyzerEngine {
	log.Fatalf("gliner pool not available: this binary was built without -tags ner. " +
		"Rebuild with `go build -tags ner ./...` for the GLiNER pool, " +
		"or unset GLINER_POOL_SIZE to use the single-recognizer path.")
	return nil
}

// DefaultAnalyzerEngineWithGLiNERFlatPool is the fail-fast stub for the
// flat-decoder GLiNER pool. Same rationale as
// DefaultAnalyzerEngineWithGLiNERConfig; the real implementation needs
// `-tags ner`.
func DefaultAnalyzerEngineWithGLiNERFlatPool(_ *recognizers.GLiNERFlatPool) *analyzer.AnalyzerEngine {
	log.Fatalf("gliner flat pool not available: this binary was built without -tags ner. " +
		"Rebuild with `go build -tags ner ./...` for the GLiNER flat pool, " +
		"or unset GLINER_POOL_SIZE / ANONDE_GLINER_FLAT_POOL_SIZE to use the single-recognizer path.")
	return nil
}
