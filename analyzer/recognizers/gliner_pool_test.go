//go:build ner

// gliner_pool_test.go covers only the construction-time behaviour of
// `GLiNERPool`; argument validation, Name, and Destroy on a pool that
// never had Analyze called. The model-loading and inference paths are
// intentionally NOT exercised here because CI may not have the
// knowledgator/gliner-pii-base-v1.0 weights cached on disk, and we
// don't want this test to either download (slow, network-dependent)
// or be skipped (silent coverage gap).

package recognizers_test

import (
	"testing"

	"github.com/anonde-io/anonde/analyzer/recognizers"
)

func TestGLiNERPool_Construction(t *testing.T) {
	if _, err := recognizers.NewGLiNERPool(recognizers.GLiNERConfig{}, 0); err == nil {
		t.Fatal("expected error for size=0")
	}
	if _, err := recognizers.NewGLiNERPool(recognizers.GLiNERConfig{}, -1); err == nil {
		t.Fatal("expected error for size=-1")
	}

	// AutoDownload:false keeps the test offline-safe; the instances
	// are never Analyzed, so no model files are touched, but if a
	// regression ever made construction trigger an init we'd at least
	// get a clear "model not found" error instead of a silent download.
	p, err := recognizers.NewGLiNERPool(recognizers.GLiNERConfig{AutoDownload: false}, 2)
	if err != nil {
		t.Fatalf("size=2: %v", err)
	}
	if got := p.Name(); got != "GLiNERPool" {
		t.Errorf("Name() = %q, want GLiNERPool", got)
	}

	// Destroy on a pool whose instances never ran Analyze must not
	// panic. Real GLiNERRecognizer.Destroy() is a no-op when r.session
	// is nil (which it is for a recognizer that was never Analyzed),
	// so we expect a nil error here, but tolerate non-nil in case a
	// future refactor changes that contract; the load-bearing
	// assertion is "no panic".
	if err := p.Destroy(); err != nil {
		t.Logf("Destroy() returned: %v (acceptable for never-used pool)", err)
	}

	// Idempotency: a second Destroy must also not panic or block.
	// The drain loop is guarded by destroyOnce, so the second call
	// returns the cached error without re-reading the channel (which
	// would deadlock; the channel is empty after the first drain).
	if err := p.Destroy(); err != nil {
		t.Logf("Destroy() second call returned: %v", err)
	}
}
