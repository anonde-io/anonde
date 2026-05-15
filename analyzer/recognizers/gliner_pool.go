//go:build hugot

// gliner_pool.go is an OPTIONAL, opt-in wrapper that lets a caller run
// up to N GLiNER inferences in parallel.
//
// Why this exists
// ---------------
// `GLiNERRecognizer.Analyze` serialises every call behind `r.mu`. That
// is correct — both the hftokenizer and the *ort.DynamicAdvancedSession
// hold mutable state that two concurrent calls would corrupt — but it
// also means a single recognizer is a strict throughput cap. Under
// concurrent /v1/ingest load the mutex queues requests.
//
// Switching to `sync.RWMutex` doesn't help here: every protected step
// inside Analyze is a mutation (tokenizer option swaps, ONNX slot table
// writes), so there is no "read-only" path to share. The only way to
// get parallel inference is N independent recognizers, each with its
// own tokenizer + session. That is what `GLiNERPool` provides, behind a
// fixed-size channel so the (N+1)th caller blocks instead of allocating
// another session.
//
// Cost
// ----
// Each instance holds the ONNX session resident, ~500 MB for the
// default knowledgator/gliner-pii-base-v1.0 quint8 build. N=4 peaks
// around 2 GB of RSS — it fits on a `shared-cpu-1x:4096MB` Fly machine
// with room for the Go heap, but anything smaller will OOM. Size the
// pool against your VM's memory budget, not your CPU count.
//
// Lifecycle notes
// ---------------
// * Construction is cheap. The N child recognizers are NOT pre-warmed:
//   each one lazy-initialises on its first `Analyze` call, matching
//   `GLiNERRecognizer`'s own behaviour. The first `Analyze`s pay the
//   model-loading cost N times in parallel. If you want all sessions
//   hot before traffic arrives, fire N parallel `Analyze(ctx, "", nil,
//   "")` warmup calls after `NewGLiNERPool`.
// * `Destroy` blocks until every outstanding `Analyze` has returned its
//   instance to the channel. Callers MUST stop dispatching new work
//   before calling `Destroy`, otherwise the drain loop will deadlock
//   against a goroutine that still holds an instance.
//
// Naming caveat
// -------------
// `Name()` returns "GLiNERPool" — it does NOT end in "NERRecognizer",
// which means the analyzer engine's `DisableNER` suffix-check WILL NOT
// suppress the pool. Callers that want per-request NER disable while
// using the pool must enforce it higher in the stack (e.g. skip
// dispatching to the pool when `req.DisableNER` is set).
//
// Integration
// -----------
// This file is opt-in only. It is NOT wired into `analyzer.go` or the
// platform service. Use it when fly logs show concurrent /v1/ingest
// queueing visible as wall-clock latency on `r.mu` and you have memory
// headroom for N sessions. For typical low-QPS deploys a bare
// `GLiNERRecognizer` is the right answer.

package recognizers

import (
	"context"
	"fmt"
	"sync"

	"github.com/anonde-io/anonde/analyzer"
)

// GLiNERPool is an N-instance recognizer pool. Each instance owns its
// own tokenizer + ONNX session, so up to N `Analyze` calls run truly
// in parallel — the (N+1)th caller blocks on the channel until an
// instance is returned.
//
// The zero value is NOT usable; construct via `NewGLiNERPool`.
type GLiNERPool struct {
	// instances is a buffered channel that holds exactly `size`
	// recognizers when fully idle. Acquire = receive, release = send.
	// Using a channel (rather than a sync.Cond / mutex+list) gives us
	// ctx-cancellable acquisition for free via `select`.
	instances chan *GLiNERRecognizer

	// destroyOnce ensures Destroy's drain loop runs at most once even
	// if a caller invokes it from multiple goroutines.
	destroyOnce sync.Once
	destroyErr  error

	// cfg is retained for diagnostics; the per-instance recognizers
	// already hold their own copy.
	cfg GLiNERConfig

	// size is the number of instances the pool was constructed with.
	// Kept separate from `cap(instances)` for readability in Destroy's
	// drain loop, though they are equal by construction.
	size int
}

// NewGLiNERPool constructs a pool of `size` recognizers, each
// configured with `cfg`.
//
// The recognizers are NOT pre-warmed: each one lazily initialises on
// its first `Analyze` call, matching `GLiNERRecognizer`'s own
// behaviour. Pool construction is therefore cheap — the first N
// concurrent `Analyze` calls will each pay the model-loading cost in
// parallel.
//
// `size <= 0` returns an error. `size == 1` is functionally equivalent
// to a bare `GLiNERRecognizer` plus a tiny channel-send/receive
// overhead; if you only need one instance, skip the pool.
func NewGLiNERPool(cfg GLiNERConfig, size int) (*GLiNERPool, error) {
	if size <= 0 {
		return nil, fmt.Errorf("gliner pool: size must be positive, got %d", size)
	}
	p := &GLiNERPool{
		instances: make(chan *GLiNERRecognizer, size),
		cfg:       cfg,
		size:      size,
	}
	for i := 0; i < size; i++ {
		p.instances <- NewGLiNERRecognizer(cfg)
	}
	return p, nil
}

// Name returns "GLiNERPool".
//
// Note: this does NOT end in "NERRecognizer", which means the analyzer
// engine's DisableNER suffix-check will NOT suppress the pool. See the
// package-level godoc for guidance on enforcing DisableNER higher in
// the stack when using the pool.
func (p *GLiNERPool) Name() string { return "GLiNERPool" }

// SupportedEntities returns the canonical entity set every instance
// would return. All N instances share `cfg`, so any single one is a
// valid source of truth.
//
// Acquires + releases one instance from the channel. If every instance
// is currently busy, this blocks until one is free.
func (p *GLiNERPool) SupportedEntities() []string {
	rec := <-p.instances
	defer func() { p.instances <- rec }()
	return rec.SupportedEntities()
}

// SupportedLanguages returns the language set every instance would
// return. Acquires + releases one instance, same blocking behaviour as
// `SupportedEntities`.
func (p *GLiNERPool) SupportedLanguages() []string {
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
// the underlying `GLiNERRecognizer.Analyze` is responsible for its own
// ctx handling.
func (p *GLiNERPool) Analyze(ctx context.Context, text string, entities []string, language string) ([]analyzer.RecognizerResult, error) {
	var rec *GLiNERRecognizer
	select {
	case rec = <-p.instances:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	defer func() { p.instances <- rec }()
	return rec.Analyze(ctx, text, entities, language)
}

// Destroy releases every pool instance's ONNX session. The first
// non-nil error from any instance's Destroy is returned; subsequent
// errors are dropped (matching the "first failure wins" convention
// used elsewhere in this package).
//
// Destroy is idempotent: subsequent calls return the same error
// without re-draining.
//
// Caller contract: stop dispatching new `Analyze` calls BEFORE
// invoking Destroy. The drain loop pulls exactly `size` instances out
// of the channel and will block forever if a goroutine still holds
// one. There is no "force-destroy with outstanding work" mode by
// design — silently destroying a session that another goroutine is
// running through is a crash, not a feature.
func (p *GLiNERPool) Destroy() error {
	p.destroyOnce.Do(func() {
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
