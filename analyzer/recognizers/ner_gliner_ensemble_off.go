//go:build !hugot

// ner_gliner_ensemble_off.go is the fail-fast stub for the GLiNER
// ensemble recognizer, used when the binary is built WITHOUT the
// `hugot` tag. The real implementation lives in
// ner_gliner_ensemble.go and requires CGO + onnxruntime + tokenizers;
// this stub keeps the public
// surface stable so cmd/anonde/main.go can reference the ensemble
// symbols regardless of build tag, and falls through cleanly to a
// boot-time error when an operator sets ANONDE_NER_STACK on a
// patterns-only build.

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
// `-tags hugot`. Same shape as errGLiNERDisabled (sentinel + errors.Is
// friendly).
var errEnsembleDisabled = errors.New("gliner-ensemble: backend not available: " +
	"this binary was built without -tags hugot. " +
	"Rebuild with `go build -tags hugot ./...` to enable the GLiNER ensemble.")

// EnsembleGLiNERRecognizer is the no-op fallback used in non-hugot
// builds. Stores the model IDs purely for diagnostics; Analyze always
// errors.
type EnsembleGLiNERRecognizer struct {
	modelIDs []string
}

// NewEnsembleGLiNERRecognizer mirrors the real constructor's signature
// so cmd/anonde/main.go compiles under both build tags. Construction
// always succeeds; the error surfaces only at Analyze-time so a
// wired-up engine still boots and reports which backend is missing.
func NewEnsembleGLiNERRecognizer(modelIDs []string, _ float64, _ string) *EnsembleGLiNERRecognizer {
	ids := make([]string, 0, len(modelIDs))
	for _, id := range modelIDs {
		if id = strings.TrimSpace(id); id != "" {
			ids = append(ids, id)
		}
	}
	return &EnsembleGLiNERRecognizer{modelIDs: ids}
}

// EnsembleFromEnv mirrors the real implementation's three-way return
// contract:
//   - (nil, nil) if ANONDE_NER_STACK is unset → caller falls through to
//     the single-model path with no behaviour change
//   - (nil, error) if ANONDE_NER_STACK is set on a build without
//     `-tags hugot` → boot-time error, surfaces a deployer mistake
//     loudly (a patterns-only image shouldn't be asked to run an
//     ensemble; better to refuse than to silently disable NER)
//   - we never reach the (*EnsembleGLiNERRecognizer, nil) branch in the
//     stub — the backend isn't available
func EnsembleFromEnv(_ float64, _ string) (*EnsembleGLiNERRecognizer, error) {
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

// SupportedEntities returns the default ensemble entity coverage. The
// stub can't actually run, but consumers that interrogate
// SupportedEntities() before Analyze (e.g. registry routing) deserve
// the same shape they'd see on the real path.
func (e *EnsembleGLiNERRecognizer) SupportedEntities() []string {
	// Mirror GLiNERRecognizer stub's behaviour: derive from
	// DefaultLabelToEntity.
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
