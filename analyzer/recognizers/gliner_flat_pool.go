//go:build hugot

// gliner_flat_pool.go is an OPTIONAL, opt-in wrapper that lets a caller
// run up to N flat-decoder GLiNER inferences in parallel. Direct mirror
// of gliner_pool.go; same channel-of-instances design, same lifecycle
// contract; but each instance is a `*GLiNERFlatRecognizer` (4-input
// token / BIO decoder) instead of a span-decoder `*GLiNERRecognizer`.
//
// Why this exists
// ---------------
// `GLiNERFlatRecognizer.Analyze` serialises every call behind its mutex
// for the same reason the span recognizer does; the hftokenizer and
// the *ort.DynamicAdvancedSession both hold mutable state that two
// concurrent calls would corrupt. The only way to get parallel
// inference is N independent recognizers, each with its own tokenizer
// + session. `GLiNERFlatPool` provides exactly that behind a
// fixed-size channel so the (N+1)th caller blocks instead of
// allocating another session.
//
// Cost
// ----
// Flat-decoder pools are substantially heavier per slot than span
// pools. The canonical LARGE PII flat export
// (`knowledgator/gliner-pii-large-v1.0`) is FP32 ONNX at ~1.4 GB
// resident per session, vs ~500 MB for the BASE quint8 span model.
// N=2 already peaks around 2.8 GB RSS; that's a comfortable fit on
// a 4 GB VM but anything smaller will OOM. Size the LARGE pool
// against your VM's memory budget; in a stack deployment the BASE
// pool can almost always be larger than the LARGE pool.
//
// Lifecycle notes
// ---------------
// * Construction is cheap. The N child recognizers are NOT pre-warmed:
//   each one lazy-initialises on its first `Analyze` call, matching
//   `GLiNERFlatRecognizer`'s own behaviour. The first `Analyze`s pay
//   the model-loading cost N times in parallel. If you want all
//   sessions hot before traffic arrives, fire N parallel `Analyze(ctx,
//   "", nil, "")` warmup calls after `NewGLiNERFlatPool`.
// * `Destroy` blocks until every outstanding `Analyze` has returned
//   its instance to the channel. Callers MUST stop dispatching new
//   work before calling `Destroy`, otherwise the drain loop will
//   deadlock against a goroutine that still holds an instance.
//
// Naming caveat
// -------------
// `Name()` returns "GLiNERFlatPool"; it does NOT end in
// "NERRecognizer", which means the analyzer engine's `DisableNER`
// suffix-check WILL NOT suppress the pool. Callers that want
// per-request NER disable while using the pool must enforce it higher
// in the stack (e.g. skip dispatching to the pool when
// `req.DisableNER` is set). The conflict resolver knows about the
// pool name explicitly (see analyzer/result.go::nerRecognizerNames).
//
// Integration
// -----------
// This file is opt-in only. Wire via `cmd/anonde/main.go::analyzerFromEnv`
// when `GLINER_POOL_SIZE >= 2` for `gliner-flat`, or
// `ANONDE_GLINER_FLAT_POOL_SIZE >= 2` for the flat slot of
// `gliner-stack`. For typical low-QPS deploys a bare
// `GLiNERFlatRecognizer` is the right answer.

package recognizers

import (
	"context"
	"fmt"
	"sync"

	"github.com/anonde-io/anonde/analyzer"
)

// GLiNERFlatPool is an N-instance flat-decoder GLiNER recognizer pool.
// Each instance owns its own tokenizer + ONNX session, so up to N
// `Analyze` calls run truly in parallel; the (N+1)th caller blocks
// on the channel until an instance is returned.
//
// The zero value is NOT usable; construct via `NewGLiNERFlatPool`.
//
// LARGE flat models are substantially heavier than BASE span models
// (~1.4 GB FP32 vs ~500 MB quint8), so the pool size should reflect
// that; in a stack deployment the flat pool is usually smaller than
// the span pool.
type GLiNERFlatPool struct {
	// instances is a buffered channel that holds exactly `size`
	// recognizers when fully idle. Acquire = receive, release = send.
	// Using a channel (rather than a sync.Cond / mutex+list) gives us
	// ctx-cancellable acquisition for free via `select`.
	instances chan *GLiNERFlatRecognizer

	// recs is a parallel slice holding the same N pointers the channel
	// buffers. It exists for paths that need to iterate over every
	// instance WITHOUT draining the channel (Warmup), so concurrent
	// Analyze() calls can still acquire normally during iteration. The
	// per-recognizer mutex inside GLiNERFlatRecognizer.Analyze handles
	// the "Warmup and an external request hit the same instance" race
	// correctly: they serialise on r.mu, no deadlock.
	recs []*GLiNERFlatRecognizer

	// destroyOnce ensures Destroy's drain loop runs at most once even
	// if a caller invokes it from multiple goroutines.
	destroyOnce sync.Once
	destroyErr  error

	// done is closed by Destroy() before the drain loop starts so
	// any new Analyze acquirer receives ErrPoolClosed instead of
	// blocking forever. Same shape as GLiNERPool.done.
	done chan struct{}

	// cfg is retained for diagnostics; the per-instance recognizers
	// already hold their own copy.
	cfg GLiNERConfig

	// size is the number of instances the pool was constructed with.
	// Kept separate from `cap(instances)` for readability in Destroy's
	// drain loop, though they are equal by construction.
	size int
}

// NewGLiNERFlatPool constructs a pool of `size` flat-decoder
// recognizers, each configured with `cfg`.
//
// The recognizers are NOT pre-warmed: each one lazily initialises on
// its first `Analyze` call, matching `GLiNERFlatRecognizer`'s own
// behaviour. Pool construction is therefore cheap; the first N
// concurrent `Analyze` calls will each pay the model-loading cost in
// parallel.
//
// `size <= 0` returns an error. `size == 1` is functionally equivalent
// to a bare `GLiNERFlatRecognizer` plus a tiny channel-send/receive
// overhead; if you only need one instance, skip the pool. Because
// LARGE flat ONNX exports cost ~1.4 GB per session, sizes above 2 are
// rarely justified outside multi-core hosts with ≥8 GB RAM.
func NewGLiNERFlatPool(cfg GLiNERConfig, size int) (*GLiNERFlatPool, error) {
	if size <= 0 {
		return nil, fmt.Errorf("gliner flat pool: size must be positive, got %d", size)
	}
	p := &GLiNERFlatPool{
		instances: make(chan *GLiNERFlatRecognizer, size),
		recs:      make([]*GLiNERFlatRecognizer, 0, size),
		cfg:       cfg,
		size:      size,
		done:      make(chan struct{}),
	}
	for i := 0; i < size; i++ {
		rec := NewGLiNERFlatRecognizer(cfg)
		p.recs = append(p.recs, rec)
		p.instances <- rec
	}
	return p, nil
}

// Size returns the configured number of instances. Useful for
// diagnostic logging (e.g. warmup) without exposing the internal
// channel directly.
func (p *GLiNERFlatPool) Size() int { return p.size }

// Name returns "GLiNERFlatPool".
//
// Note: this does NOT end in "NERRecognizer", which means the analyzer
// engine's DisableNER suffix-check will NOT suppress the pool. See the
// package-level godoc for guidance on enforcing DisableNER higher in
// the stack when using the pool. The conflict resolver
// (analyzer/result.go::nerRecognizerNames) knows this name
// explicitly so the NER-preferred entity rule still applies.
func (p *GLiNERFlatPool) Name() string { return "GLiNERFlatPool" }

// SupportedEntities returns the canonical entity set every instance
// would return. All N instances share `cfg`, so any single one is a
// valid source of truth.
//
// Acquires + releases one instance from the channel. If every instance
// is currently busy, this blocks until one is free.
func (p *GLiNERFlatPool) SupportedEntities() []string {
	rec := <-p.instances
	defer func() { p.instances <- rec }()
	return rec.SupportedEntities()
}

// SupportedLanguages returns the language set every instance would
// return. Acquires + releases one instance, same blocking behaviour as
// `SupportedEntities`.
func (p *GLiNERFlatPool) SupportedLanguages() []string {
	rec := <-p.instances
	defer func() { p.instances <- rec }()
	return rec.SupportedLanguages()
}

// Analyze acquires an instance from the pool, runs inference, and
// returns the instance to the pool when done.
//
// Respects `ctx` cancellation while waiting in the channel: if every
// instance is busy and `ctx` is cancelled before one frees up, returns
// `ctx.Err()` without running inference. Once an instance is acquired,
// the underlying `GLiNERFlatRecognizer.Analyze` is responsible for its
// own ctx handling.
func (p *GLiNERFlatPool) Analyze(ctx context.Context, text string, entities []string, language string) ([]analyzer.RecognizerResult, error) {
	var rec *GLiNERFlatRecognizer
	select {
	case rec = <-p.instances:
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-p.done:
		// Pool was Destroy()d while we were waiting for an instance.
		return nil, ErrPoolClosed
	}
	defer func() { p.instances <- rec }()
	return rec.Analyze(ctx, text, entities, language)
}

// Warmup forces every flat-pool instance to pay its lazy-init cost up
// front.
//
// It fires `p.size` Analyze() calls in parallel against a trivial
// fixture ("John Smith works at Mercy Hospital."). Each one acquires a
// distinct instance from the pool, which on first use loads the
// tokenizer + ONNX session; the same 5-30 s model-load that the
// FIRST N user requests would otherwise see staggered across them.
// LARGE flat ONNX exports cost ~1.4 GB per session, so on small VMs
// pre-warming may surface OOM at boot rather than mid-request; that's
// the intent. After Warmup returns, every instance has its session
// resident and the first real /v1/ingest sees ~150 ms latency.
//
// SAFE FOR CONCURRENT TRAFFIC
// ---------------------------
// Warmup does NOT acquire from the pool's instance channel. It walks
// the parallel `recs` slice and calls Analyze directly on each
// recognizer, in parallel. Concurrent callers using p.Analyze() can
// still pull from the channel during warmup, so the HTTP server can
// safely start listening before Warmup returns. The per-recognizer
// mutex inside GLiNERFlatRecognizer.Analyze serialises any race
// between the warmup goroutine for instance i and an external request
// that also lands on instance i; the external request just queues
// briefly on r.mu and then runs; it never blocks on the pool itself.
//
// Error handling: the first non-nil error from any instance is
// returned; subsequent errors are dropped (matching the "first
// failure wins" convention used by Destroy). All N goroutines are
// awaited regardless; partial warmup leaves the pool in a usable
// state (the failed instance retries on its next Analyze).
func (p *GLiNERFlatPool) Warmup(ctx context.Context) error {
	var (
		wg       sync.WaitGroup
		errMu    sync.Mutex
		firstErr error
	)
	wg.Add(len(p.recs))
	for _, rec := range p.recs {
		rec := rec
		go func() {
			defer wg.Done()
			_, err := rec.Analyze(ctx, "John Smith works at Mercy Hospital.", nil, "")
			if err != nil {
				errMu.Lock()
				if firstErr == nil {
					firstErr = err
				}
				errMu.Unlock()
			}
		}()
	}
	wg.Wait()
	return firstErr
}

// Destroy releases every pool instance's ONNX session. The first
// non-nil error from any instance's Destroy is returned; subsequent
// errors are dropped (matching the "first failure wins" convention
// used elsewhere in this package).
//
// Destroy is idempotent: subsequent calls return the same error
// without re-draining.
//
// Graceful shutdown: callers no longer need to stop dispatching new
// Analyze calls before invoking Destroy. Closing `p.done` is the
// first thing the function does; after that, any new Analyze
// acquirer receives ErrPoolClosed instead of blocking on the empty
// channel. In-flight Analyzes complete normally and return their
// instance, which the drain loop then picks up. The drain still
// blocks until exactly `size` instances are recovered, so Destroy
// will return only once every session is released and Destroyed.
func (p *GLiNERFlatPool) Destroy() error {
	p.destroyOnce.Do(func() {
		close(p.done)
		var firstErr error
		for i := 0; i < p.size; i++ {
			rec := <-p.instances
			if err := rec.Destroy(); err != nil && firstErr == nil {
				firstErr = err
			}
		}
		p.destroyErr = firstErr
	})
	return p.destroyErr
}
