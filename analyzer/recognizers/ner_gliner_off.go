//go:build !hugot

// ner_gliner_off.go is the fail-fast stub used when the binary is built
// WITHOUT the `hugot` tag. The real implementation in ner_gliner.go
// pulls in onnxruntime_go + tokenizers, both CGO. Default builds skip
// all of that and surface a clear error message when something tries to
// construct or call a GLiNERRecognizer.
//
// Mirror of ner_gliner.go's public API surface — same Name(), same
// SupportedEntities() shape — so callers that link both build variants
// don't see a type-shape mismatch. Only Analyze() and Destroy() raise.

package recognizers

import (
	"context"
	"errors"
	"sort"

	"github.com/anonde-io/anonde/analyzer"
)

// errGLiNERDisabled is the canned error returned by every Analyze call
// when the hugot tag is absent. It's a sentinel — callers can check via
// errors.Is for clean handling in tests.
var errGLiNERDisabled = errors.New("gliner: backend not available: " +
	"this binary was built without -tags hugot. " +
	"Rebuild with `go build -tags hugot ./...` to enable the GLiNER recognizer.")

// GLiNERRecognizer is the no-op fallback used in non-hugot builds. It
// stores the config for diagnostics but Analyze always errors.
type GLiNERRecognizer struct {
	cfg GLiNERConfig
}

// NewGLiNERRecognizer returns the stub. Construction always succeeds —
// the error surfaces only at Analyze-time so a wired-up engine can
// still boot and report which backend is missing.
func NewGLiNERRecognizer(cfg GLiNERConfig) *GLiNERRecognizer {
	return &GLiNERRecognizer{cfg: cfg}
}

// Name returns the recognizer name. Same as the real implementation so
// the DisableNER suffix check and registry lookups match.
func (r *GLiNERRecognizer) Name() string { return "GLiNERRecognizer" }

// SupportedEntities mirrors the real implementation so callers asking
// "does this recognizer cover X?" see the same answer regardless of
// build tag.
func (r *GLiNERRecognizer) SupportedEntities() []string {
	m := r.cfg.LabelToEntity
	if len(m) == 0 {
		m = DefaultLabelToEntity
	}
	seen := make(map[string]struct{}, len(m))
	out := make([]string, 0, len(m))
	for _, v := range m {
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}

// SupportedLanguages mirrors the real implementation.
func (r *GLiNERRecognizer) SupportedLanguages() []string {
	return []string{"en", "de", "es", "fr", "it", "nl", "pt"}
}

// Analyze always returns errGLiNERDisabled in the stub.
func (r *GLiNERRecognizer) Analyze(_ context.Context, _ string, _ []string, _ string) ([]analyzer.RecognizerResult, error) {
	return nil, errGLiNERDisabled
}

// Destroy is a no-op for the stub.
func (r *GLiNERRecognizer) Destroy() error { return nil }
