//go:build !hugot

// gliner_flat_pool_off.go is the fail-fast stub for GLiNERFlatPool,
// used when the binary is built WITHOUT the `hugot` tag. Mirror of
// gliner_pool_off.go for the flat / token decoder pool. Same
// rationale; keep the public surface stable so cmd/anonde/main.go
// compiles regardless of build tag, and fail loudly at boot rather
// than silently disabling NER.

package recognizers

import (
	"context"
	"errors"

	"github.com/anonde-io/anonde/analyzer"
)

// errFlatPoolDisabled is returned by NewGLiNERFlatPool / Analyze when
// the binary lacks `-tags hugot`. Sentinel + errors.Is friendly.
var errFlatPoolDisabled = errors.New("gliner flat pool: backend not available: " +
	"this binary was built without -tags hugot. " +
	"Rebuild with `go build -tags hugot ./...` to enable the GLiNER flat pool.")

// GLiNERFlatPool is the no-op fallback used in non-hugot builds.
// Stores the requested size purely for diagnostics; every method
// errors or returns an empty result.
type GLiNERFlatPool struct {
	size int
}

// NewGLiNERFlatPool mirrors the real constructor's signature so
// cmd/anonde/main.go compiles under both build tags. Always returns
// errFlatPoolDisabled.
func NewGLiNERFlatPool(_ GLiNERConfig, size int) (*GLiNERFlatPool, error) {
	return nil, errFlatPoolDisabled
}

// Name mirrors the real implementation so the conflict resolver's
// nerRecognizerNames lookup remains consistent across build tags.
func (p *GLiNERFlatPool) Name() string { return "GLiNERFlatPool" }

// Size returns the size the stub was constructed with; purely for
// diagnostic parity with the real implementation. Always 0 here since
// NewGLiNERFlatPool errors out before storing it.
func (p *GLiNERFlatPool) Size() int { return p.size }

// SupportedEntities returns an empty slice; the stub can't infer.
func (p *GLiNERFlatPool) SupportedEntities() []string { return nil }

// SupportedLanguages returns an empty slice; the stub can't infer.
func (p *GLiNERFlatPool) SupportedLanguages() []string { return nil }

// Analyze always returns errFlatPoolDisabled in the stub.
func (p *GLiNERFlatPool) Analyze(_ context.Context, _ string, _ []string, _ string) ([]analyzer.RecognizerResult, error) {
	return nil, errFlatPoolDisabled
}

// Warmup is a no-op in the stub. The real implementation pre-warms
// every pool instance in parallel; the stub exists only so
// cmd/anonde/main.go's type-switch on *GLiNERFlatPool compiles cleanly
// under the default no-tag build.
func (p *GLiNERFlatPool) Warmup(_ context.Context) error { return nil }

// Destroy is a no-op in the stub.
func (p *GLiNERFlatPool) Destroy() error { return nil }
