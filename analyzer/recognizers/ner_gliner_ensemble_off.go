//go:build !hugot

// ner_gliner_ensemble_off.go is the fail-fast stub for the GLiNER ensemble
// in non-hugot builds. It keeps the public symbols stable so
// cmd/anonde/main.go compiles regardless of build tag, and turns
// ANONDE_NER_STACK on a patterns-only build into a clear boot-time error.

package recognizers

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/anonde-io/anonde/analyzer"
)

// errEnsembleDisabled is returned by Analyze when the binary lacks
// `-tags hugot`.
var errEnsembleDisabled = errors.New("gliner-ensemble: backend not available: " +
	"this binary was built without -tags hugot. " +
	"Rebuild with `go build -tags hugot ./...` to enable the GLiNER ensemble.")

// EnsembleGLiNERRecognizer is the no-op fallback in non-hugot builds;
// Analyze always errors.
type EnsembleGLiNERRecognizer struct {
	modelIDs []string
}

// NewEnsembleGLiNERRecognizer mirrors the real constructor's signature so
// cmd/anonde/main.go compiles under both build tags. The error surfaces
// only at Analyze-time.
func NewEnsembleGLiNERRecognizer(modelIDs []string, _ float64, _ string, _ ...SpanFilterConfig) *EnsembleGLiNERRecognizer {
	ids := make([]string, 0, len(modelIDs))
	for _, id := range modelIDs {
		if id = strings.TrimSpace(id); id != "" {
			ids = append(ids, id)
		}
	}
	return &EnsembleGLiNERRecognizer{modelIDs: ids}
}

// EnsembleFromEnv mirrors the real return contract: (nil, nil) when
// ANONDE_NER_STACK is unset, and a boot-time error when it is set on a
// build without `-tags hugot` (refuse rather than silently disable NER).
func EnsembleFromEnv(_ float64, _ string, _ ...SpanFilterConfig) (*EnsembleGLiNERRecognizer, error) {
	if strings.TrimSpace(os.Getenv("ANONDE_NER_STACK")) == "" {
		return nil, nil
	}
	return nil, fmt.Errorf("ANONDE_NER_STACK is set but this binary lacks `-tags hugot`: %w", errEnsembleDisabled)
}

// Name mirrors the real implementation so the analyzer engine's
// DisableNER suffix-check still fires consistently across build tags.
func (e *EnsembleGLiNERRecognizer) Name() string {
	return "GLiNEREnsembleNERRecognizer"
}

// SupportedEntities returns the default ensemble entity coverage so
// consumers that interrogate it before Analyze (e.g. registry routing) see
// the same shape as the real path.
func (e *EnsembleGLiNERRecognizer) SupportedEntities() []string {
	seen := make(map[string]struct{}, len(DefaultLabelToEntity))
	out := make([]string, 0, len(DefaultLabelToEntity))
	for _, v := range DefaultLabelToEntity {
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}

// SupportedLanguages mirrors GLiNER's stub language set.
func (e *EnsembleGLiNERRecognizer) SupportedLanguages() []string {
	return []string{"en", "de", "es", "fr", "it", "nl", "pt"}
}

// Analyze always returns errEnsembleDisabled in the stub.
func (e *EnsembleGLiNERRecognizer) Analyze(_ context.Context, _ string, _ []string, _ string) ([]analyzer.RecognizerResult, error) {
	return nil, errEnsembleDisabled
}
