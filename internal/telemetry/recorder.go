package telemetry

import (
	"time"

	"github.com/anonde-io/anonde/internal/metrics"
)

// WrapRecorder returns a metrics.Recorder that tees observations to
// both the supplied inner recorder (typically the Prometheus
// recorder, or NoopRecorder when METRICS_ENABLED=false) and the
// telemetry Collector. The wire shape callers see is identical to
// the inner recorder; this is purely additive.
//
// When inner is nil it's replaced with the no-op recorder so callers
// (cmd/anonde/main) don't have to special-case the
// metrics-disabled-but-telemetry-enabled combination.
func WrapRecorder(inner metrics.Recorder, c *Collector) metrics.Recorder {
	if inner == nil {
		inner = metrics.NewNoop()
	}
	if c == nil {
		// Telemetry off but caller still asked to wrap — degrade to
		// the inner recorder unchanged rather than installing a
		// pointless tee.
		return inner
	}
	return &teeRecorder{inner: inner, collector: c}
}

type teeRecorder struct {
	inner     metrics.Recorder
	collector *Collector
}

func (t *teeRecorder) Request(op string) metrics.RequestSpan {
	return &teeSpan{
		inner:     t.inner.Request(op),
		collector: t.collector,
		start:     time.Now(),
	}
}

func (t *teeRecorder) EntityDetected(entityType, recognizer string, score float64) {
	t.inner.EntityDetected(entityType, recognizer, score)
	t.collector.RecordEntity(entityType)
}

func (t *teeRecorder) ConflictResolved(winnerKind, loserKind string) {
	t.inner.ConflictResolved(winnerKind, loserKind)
}

func (t *teeRecorder) VaultOp(op string)          { t.inner.VaultOp(op) }
func (t *teeRecorder) PolicyDenied(reason string) { t.inner.PolicyDenied(reason) }

// teeSpan wraps the inner span so it captures duration + status for
// the telemetry collector at Done() time. We re-measure start
// ourselves (rather than reaching into the inner span) because
// RequestSpan deliberately doesn't expose its start time.
type teeSpan struct {
	inner     metrics.RequestSpan
	collector *Collector
	start     time.Time
}

func (s *teeSpan) BytesIn(n int)  { s.inner.BytesIn(n) }
func (s *teeSpan) BytesOut(n int) { s.inner.BytesOut(n) }

func (s *teeSpan) AnalyzeDuration(backend string, seconds float64) {
	s.inner.AnalyzeDuration(backend, seconds)
}

func (s *teeSpan) Done(status string) {
	s.inner.Done(status)
	s.collector.RecordRequest(time.Since(s.start).Seconds(), status)
}

// Compile-time interface assertions.
var (
	_ metrics.Recorder    = (*teeRecorder)(nil)
	_ metrics.RequestSpan = (*teeSpan)(nil)
)
