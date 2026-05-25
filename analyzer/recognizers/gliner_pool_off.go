//go:build !hugot

// gliner_pool_off.go is the fail-fast stub for GLiNERPool, used when
// the binary is built WITHOUT the `hugot` tag. The real
// implementation lives in gliner_pool.go and requires CGO +
// onnxruntime + tokenizers; this stub keeps the public surface stable
// so cmd/anonde/main.go can reference the pool symbols regardless of
// build tag, and falls through cleanly to a boot-time error when an
// operator sets GLINER_POOL_SIZE on a patterns-only build.
//
// Mirrors the shape of ner_gliner_ensemble_off.go — sentinel error,
// minimal struct, constructor returns (nil, err) so the caller's
// "log.Fatalf on pool construction failure" path fires loudly instead
// of silently disabling NER.

package recognizers

import (
	"context"
	"errors"

	"github.com/anonde-io/anonde/analyzer"
)

// errPoolDisabled is returned by NewGLiNERPool / Analyze when the
// binary lacks `-tags hugot`. Same shape as errGLiNERDisabled
// (sentinel + errors.Is friendly).
var errPoolDisabled = errors.New("gliner pool: backend not available: " +
	"this binary was built without -tags hugot. " +
	"Rebuild with `go build -tags hugot ./...` to enable the GLiNER pool.")

// GLiNERPool is the no-op fallback used in non-hugot builds. Stores
// the requested size purely for diagnostics; every method errors or
// returns an empty result.
type GLiNERPool struct {
	size int
}

// NewGLiNERPool mirrors the real constructor's signature so
// cmd/anonde/main.go compiles under both build tags. Always returns
// errPoolDisabled so the operator sees a clear "backend not built"
// failure at boot rather than a silently-degraded server.
func NewGLiNERPool(_ GLiNERConfig, size int) (*GLiNERPool, error) {
	return nil, errPoolDisabled
}

// Name mirrors the real implementation so the conflict resolver's
// nerRecognizerNames lookup remains consistent across build tags.
func (p *GLiNERPool) Name() string { return "GLiNERPool" }

// Size returns the size the stub was constructed with — purely for
// diagnostic parity with the real implementation. Always 0 here since
// NewGLiNERPool errors out before storing it.
func (p *GLiNERPool) Size() int { return p.size }

// SupportedEntities returns an empty slice — the stub can't infer.
func (p *GLiNERPool) SupportedEntities() []string { return nil }

// SupportedLanguages returns an empty slice — the stub can't infer.
func (p *GLiNERPool) SupportedLanguages() []string { return nil }

// Analyze always returns errPoolDisabled in the stub.
func (p *GLiNERPool) Analyze(_ context.Context, _ string, _ []string, _ string) ([]analyzer.RecognizerResult, error) {
	return nil, errPoolDisabled
}

// Warmup is a no-op in the stub. The real implementation pre-warms
// every pool instance in parallel; the stub exists only so
// cmd/anonde/main.go's type-switch on *GLiNERPool compiles cleanly
// under the default no-tag build.
func (p *GLiNERPool) Warmup(_ context.Context) error { return nil }

// Destroy is a no-op in the stub.
func (p *GLiNERPool) Destroy() error { return nil }
