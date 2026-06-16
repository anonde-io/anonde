package metrics

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// promRecorder is the Prometheus-backed Recorder. All vectors here
// are owned by the private *prometheus.Registry New() registered them
// against; callers never see the prometheus types.
type promRecorder struct {
	requests       *prometheus.CounterVec
	bytesProcessed *prometheus.CounterVec
	entitiesTotal  *prometheus.CounterVec
	conflicts      *prometheus.CounterVec
	vaultOps       *prometheus.CounterVec
	policyDenials  *prometheus.CounterVec

	requestDuration *prometheus.HistogramVec
	analyzeDuration *prometheus.HistogramVec
	textLength      *prometheus.HistogramVec
	entityScore     *prometheus.HistogramVec
}

// Bucket sets are defined once and reused. The size buckets sweep
// from a single short log line (64 B) up to the default
// MAX_CONTENT_BYTES cap (~1 MiB) so anomalies at both extremes are
// visible without runtime overflow buckets dominating the series.
var (
	textLengthBuckets = []float64{64, 256, 1024, 4096, 16384, 65536, 262144, 1048576}
	entityScoreBuckets = []float64{0.1, 0.2, 0.3, 0.4, 0.5, 0.6, 0.7, 0.8, 0.9, 1.0}
)

func newPromRecorder() *promRecorder {
	return &promRecorder{
		requests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "anonde_requests_total",
			Help: "Total Service method calls, by operation and status.",
		}, []string{"operation", "status"}),

		bytesProcessed: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "anonde_bytes_processed_total",
			Help: "Total bytes flowing through anonde's data plane, by operation and direction (in|out).",
		}, []string{"operation", "direction"}),

		entitiesTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "anonde_entities_detected_total",
			Help: "Surviving PII findings after RemoveConflicts, by entity type and recognizer.",
		}, []string{"entity_type", "recognizer"}),

		conflicts: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "anonde_conflicts_resolved_total",
			Help: "Pair-wise span conflicts resolved during RemoveConflicts, by winner / loser recognizer kind (ner|pattern).",
		}, []string{"winner_kind", "loser_kind"}),

		vaultOps: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "anonde_vault_ops_total",
			Help: "Vault primitive call count, by operation (put|get|delete).",
		}, []string{"operation"}),

		policyDenials: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "anonde_policy_denials_total",
			Help: "Detokenize/reveal denials by the policy authorizer, by short stable reason code.",
		}, []string{"reason"}),

		requestDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "anonde_request_duration_seconds",
			Help:    "End-to-end duration of a Service method call.",
			Buckets: prometheus.DefBuckets,
		}, []string{"operation"}),

		analyzeDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "anonde_analyze_duration_seconds",
			Help:    "Duration of the analyzer.Analyze sub-span, by backend (patterns|gliner).",
			Buckets: prometheus.DefBuckets,
		}, []string{"backend"}),

		textLength: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "anonde_text_length_bytes",
			Help:    "Payload size in bytes per operation/direction.",
			Buckets: textLengthBuckets,
		}, []string{"operation", "direction"}),

		entityScore: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "anonde_entity_score",
			Help:    "Recognizer-emitted score for each surviving finding, by entity type and recognizer.",
			Buckets: entityScoreBuckets,
		}, []string{"entity_type", "recognizer"}),
	}
}

// collectors returns every Prometheus collector this recorder owns,
// in registration order. New() passes the slice straight to
// reg.MustRegister.
func (r *promRecorder) collectors() []prometheus.Collector {
	return []prometheus.Collector{
		r.requests,
		r.bytesProcessed,
		r.entitiesTotal,
		r.conflicts,
		r.vaultOps,
		r.policyDenials,
		r.requestDuration,
		r.analyzeDuration,
		r.textLength,
		r.entityScore,
	}
}

// ─── Recorder methods ─────────────────────────────────────────────

func (r *promRecorder) Request(op string) RequestSpan {
	return &promSpan{r: r, op: op, start: time.Now()}
}

func (r *promRecorder) EntityDetected(entityType, recognizer string, score float64) {
	r.entitiesTotal.WithLabelValues(entityType, recognizer).Inc()
	r.entityScore.WithLabelValues(entityType, recognizer).Observe(score)
}

func (r *promRecorder) ConflictResolved(winnerKind, loserKind string) {
	r.conflicts.WithLabelValues(winnerKind, loserKind).Inc()
}

func (r *promRecorder) VaultOp(op string)            { r.vaultOps.WithLabelValues(op).Inc() }
func (r *promRecorder) PolicyDenied(reason string)   { r.policyDenials.WithLabelValues(reason).Inc() }

// ─── RequestSpan ──────────────────────────────────────────────────

type promSpan struct {
	r          *promRecorder
	op         string
	start      time.Time
	bytesIn    int
	bytesOut   int
	haveIn     bool
	haveOut    bool
}

func (s *promSpan) BytesIn(n int)  { s.bytesIn = n; s.haveIn = true }
func (s *promSpan) BytesOut(n int) { s.bytesOut = n; s.haveOut = true }

func (s *promSpan) AnalyzeDuration(backend string, seconds float64) {
	s.r.analyzeDuration.WithLabelValues(backend).Observe(seconds)
}

func (s *promSpan) Done(status string) {
	dt := time.Since(s.start).Seconds()
	s.r.requestDuration.WithLabelValues(s.op).Observe(dt)
	s.r.requests.WithLabelValues(s.op, status).Inc()
	if s.haveIn {
		s.r.bytesProcessed.WithLabelValues(s.op, "in").Add(float64(s.bytesIn))
		s.r.textLength.WithLabelValues(s.op, "in").Observe(float64(s.bytesIn))
	}
	if s.haveOut {
		s.r.bytesProcessed.WithLabelValues(s.op, "out").Add(float64(s.bytesOut))
		s.r.textLength.WithLabelValues(s.op, "out").Observe(float64(s.bytesOut))
	}
}

// Compile-time interface checks.
var (
	_ Recorder    = (*promRecorder)(nil)
	_ RequestSpan = (*promSpan)(nil)
)
