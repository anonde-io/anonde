// Package metrics defines anonde's Prometheus instrumentation surface.
//
// Callers (the analyzer, core.Service, the vault/store collector) talk
// to the Recorder interface, never to prometheus directly. The
// production wiring uses a PRIVATE *prometheus.Registry — anonde
// imported as a library MUST NOT pollute the caller's default
// (global) registry. cmd/anonde mounts this private registry behind
// promhttp.HandlerFor on the optional second listener.
//
// Privacy invariant
// -----------------
//
// Every label value carried by every metric defined here is STATIC
// metadata derived from anonde internals: operation names (ingest,
// reveal, …), entity types (PERSON, EMAIL_ADDRESS, …), recognizer
// names (EmailRecognizer, GLiNERRecognizer, …), winner/loser kinds
// (ner|pattern), status (ok|denied|error), backend names (patterns|
// hugot|gliner|ollama). Tenant IDs, request IDs, tokens, raw cleartext
// or any text-derived value MUST NEVER appear as a label — they would
// (a) blow up cardinality (Prometheus best practice is <100 distinct
// label values per series) and (b) leak PII through /metrics, which is
// a worse exposure than the data plane itself.
//
// If you add a new metric to this package, add the new label set to
// the privacy guardrail test in metrics_test.go (TestPrivacy_NoPIIIn
// Labels). The test scrapes the registry after every Recorder method
// has been called with realistic args and asserts that no PII-shaped
// substring leaks through.
package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
)

// Recorder is the verb-shaped instrumentation surface anonde code
// calls into. Implementations may be a Prometheus-backed real
// recorder (New) or a no-op (NewNoop) for tests and the library
// path where the operator hasn't opted into metrics.
type Recorder interface {
	// Request opens a span over one public Service method call.
	// Each Service entry-point should defer span.Done(status) so
	// duration is captured even on early returns. The operation
	// label is the method name in lowercase ("ingest", "reveal",
	// "detokenize", "delete", "synthesize", "get_version").
	Request(op string) RequestSpan

	// EntityDetected records one surviving finding emitted by the
	// analyzer after RemoveConflicts. Called once per finding —
	// cardinality is bounded by (entity types ~30) × (recognizers ~52).
	EntityDetected(entityType, recognizer string, score float64)

	// ConflictResolved records one pair-wise conflict resolved
	// during RemoveConflicts. winnerKind / loserKind ∈ {"ner",
	// "pattern"}; cardinality is therefore 2 × 2 = 4 series.
	ConflictResolved(winnerKind, loserKind string)

	// VaultOp counts one vault primitive. op ∈ {"put", "get", "delete"}.
	VaultOp(op string)

	// PolicyDenied records one detokenize/reveal denial. reason is
	// a short stable string (e.g. "static_default", "actor_unknown")
	// — NEVER include the request's actor/purpose/tenant.
	PolicyDenied(reason string)
}

// RequestSpan accumulates per-request observations. Done MUST be
// called exactly once per Request — typically via defer.
type RequestSpan interface {
	// BytesIn / BytesOut record the request and response payload
	// sizes in bytes. For meta operations (GetVersion, Delete) that
	// have no payload, callers may skip these and Done will still
	// emit duration + status correctly.
	BytesIn(n int)
	BytesOut(n int)

	// AnalyzeDuration records the analyzer.Analyze sub-span duration.
	// Only Ingest / Synthesize call this; other ops are no-ops here.
	AnalyzeDuration(backend string, seconds float64)

	// Done finalises the span: emits request_duration_seconds and
	// (if BytesIn/BytesOut were called) text_length_bytes; increments
	// requests_total{operation,status}. status ∈ {"ok","denied","error"}.
	Done(status string)
}

// New returns a Prometheus-backed Recorder that registers all metric
// families on the supplied registry. Pass a fresh prometheus.NewRegistry()
// — never the global default — so importing anonde as a library can't
// collide with the caller's metrics.
func New(reg *prometheus.Registry) Recorder {
	r := newPromRecorder()
	reg.MustRegister(r.collectors()...)
	return r
}

// NewNoop returns a Recorder that drops every call on the floor. Use
// in tests, in library-mode embedding, or when METRICS_ENABLED=false.
func NewNoop() Recorder { return noopRecorder{} }

// ─── No-op implementation ─────────────────────────────────────────

type noopRecorder struct{}

func (noopRecorder) Request(string) RequestSpan                 { return noopSpan{} }
func (noopRecorder) EntityDetected(string, string, float64)     {}
func (noopRecorder) ConflictResolved(string, string)            {}
func (noopRecorder) VaultOp(string)                             {}
func (noopRecorder) PolicyDenied(string)                        {}

type noopSpan struct{}

func (noopSpan) BytesIn(int)                       {}
func (noopSpan) BytesOut(int)                      {}
func (noopSpan) AnalyzeDuration(string, float64)   {}
func (noopSpan) Done(string)                       {}

// Compile-time interface assertions.
var (
	_ Recorder    = noopRecorder{}
	_ RequestSpan = noopSpan{}
)
