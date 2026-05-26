//go:build !hugot

// ner_gliner_flat_off.go is the fail-fast stub used when the binary is
// built WITHOUT the `hugot` tag. The real implementation in
// ner_gliner_flat.go pulls in onnxruntime_go + tokenizers, both CGO.
// Default builds skip all of that and surface a clear error message when
// something tries to construct or call a GLiNERFlatRecognizer.
//
// Mirror of ner_gliner_flat.go's public API surface so callers that link
// both build variants see the same type shape. Only Analyze() and
// Destroy() raise.

package recognizers

import (
	"context"
	"errors"
	"sort"

	"github.com/anonde-io/anonde/analyzer"
)

// errGLiNERFlatDisabled is the canned error returned by every Analyze
// call when the hugot tag is absent. It's a sentinel; callers can check
// via errors.Is for clean handling in tests.
var errGLiNERFlatDisabled = errors.New("gliner-flat: backend not available: " +
	"this binary was built without -tags hugot. " +
	"Rebuild with `go build -tags hugot ./...` to enable the GLiNER flat-decoder recognizer.")

// GLiNERFlatRecognizer is the no-op fallback used in non-hugot builds.
// It stores the config for diagnostics but Analyze always errors.
type GLiNERFlatRecognizer struct {
	cfg GLiNERConfig
}

// NewGLiNERFlatRecognizer returns the stub. Construction always
// succeeds; the error surfaces only at Analyze-time so a wired-up engine
// can still boot and report which backend is missing.
func NewGLiNERFlatRecognizer(cfg GLiNERConfig) *GLiNERFlatRecognizer {
	return &GLiNERFlatRecognizer{cfg: cfg}
}

// Name returns the recognizer name. Same as the real implementation so
// the DisableNER suffix check and registry lookups match.
func (r *GLiNERFlatRecognizer) Name() string { return "GLiNERFlatNERRecognizer" }

// SupportedEntities mirrors the real implementation so callers asking
// "does this recognizer cover X?" see the same answer regardless of
// build tag.
func (r *GLiNERFlatRecognizer) SupportedEntities() []string {
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
func (r *GLiNERFlatRecognizer) SupportedLanguages() []string {
	return []string{"en", "de", "es", "fr", "it", "nl", "pt"}
}

// Analyze always returns errGLiNERFlatDisabled in the stub.
func (r *GLiNERFlatRecognizer) Analyze(_ context.Context, _ string, _ []string, _ string) ([]analyzer.RecognizerResult, error) {
	return nil, errGLiNERFlatDisabled
}

// Destroy is a no-op for the stub.
func (r *GLiNERFlatRecognizer) Destroy() error { return nil }
