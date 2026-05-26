package telemetry

import (
	"math"
	"sort"
	"sync"
	"time"
)

// latencyRingSize bounds the in-memory sample used to compute the
// 24h p95. A fixed-size ring buffer is overwritten in arrival order;
// 1024 float64s (8 KiB) is enough for the dashboard signal we want
// and orders-of-magnitude cheaper than a full t-digest. High-traffic
// deployments effectively report a p95 of the most recent ~1024
// requests rather than the strict 24h window, which is fine — the
// dashboard cares about "is something getting slower", not "what was
// the SLA boundary at 11:42 UTC".
const latencyRingSize = 1024

// Collector accumulates the in-memory state a Heartbeat snapshots.
// One Collector is created in cmd/anonde/main at boot and lives for
// the process lifetime. Safe for concurrent use; the data plane
// calls Record* on every request, so the lock must stay tight.
type Collector struct {
	mu sync.Mutex

	startedAt time.Time

	requestCount uint64
	errorCount   uint64
	entityCounts map[string]uint64

	// Ring buffer of request durations in seconds. Older samples are
	// overwritten by newer ones; we sort a copy at snapshot time to
	// compute the p95.
	latency    [latencyRingSize]float64
	latencyN   int  // number of samples currently held (≤ latencyRingSize)
	latencyIdx int  // next write position (modulo latencyRingSize)
	latencyAll bool // true once we've wrapped around at least once
}

// NewCollector returns an initialised Collector with startedAt set
// to time.Now. Heartbeats are constructed by combining the
// Collector's snapshot with the static StaticInfo bundle.
func NewCollector() *Collector {
	return &Collector{
		startedAt:    time.Now(),
		entityCounts: map[string]uint64{},
	}
}

// RecordRequest is called once per Service request from the metrics
// adapter (recorder.go). status is the same status string the
// metrics Recorder uses: "ok" | "denied" | "error". Only "error"
// counts against ErrorCount; denials are an expected outcome, not a
// failure.
func (c *Collector) RecordRequest(durationSeconds float64, status string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.requestCount++
	if status == "error" {
		c.errorCount++
	}
	c.latency[c.latencyIdx] = durationSeconds
	c.latencyIdx = (c.latencyIdx + 1) % latencyRingSize
	if c.latencyAll {
		// no-op: buffer already full
	} else {
		c.latencyN++
		if c.latencyN == latencyRingSize {
			c.latencyAll = true
		}
	}
}

// RecordEntity is called once per surviving finding (i.e. after
// RemoveConflicts). We only care about the entity type — recognizer
// names and scores are deliberately dropped because telemetry doesn't
// need that granularity and the smaller the value space, the lower
// the risk of a future schema-change accidentally widening it.
func (c *Collector) RecordEntity(entityType string) {
	if entityType == "" {
		return
	}
	c.mu.Lock()
	c.entityCounts[entityType]++
	c.mu.Unlock()
}

// snapshot returns a Heartbeat partial filled with the dynamic
// fields the Collector owns. The caller (sender) merges in the
// static StaticInfo bundle before sending. Snapshotting RESETS the
// counters; the next heartbeat starts from zero. The Collector
// effectively defines its own 24h window via the call site cadence.
func (c *Collector) snapshot(now time.Time) Heartbeat {
	c.mu.Lock()
	defer c.mu.Unlock()

	hb := Heartbeat{
		UptimeSeconds: int64(now.Sub(c.startedAt).Seconds()),
		RequestCount:  c.requestCount,
		ErrorCount:    c.errorCount,
		EntityCounts:  c.entityCounts,
		P95LatencyMs:  c.p95Locked(),
	}

	// Reset for the next window. EntityCounts is replaced (not
	// cleared in place) so the old map can be freely held by the
	// in-flight send goroutine without contention.
	c.requestCount = 0
	c.errorCount = 0
	c.entityCounts = map[string]uint64{}
	c.latencyN = 0
	c.latencyIdx = 0
	c.latencyAll = false

	return hb
}

// p95Locked computes the 95th percentile latency in milliseconds
// from the current ring buffer contents. Caller MUST hold c.mu.
// Returns 0 when no samples are present.
func (c *Collector) p95Locked() float64 {
	n := c.latencyN
	if n == 0 {
		return 0
	}
	// Sort a copy; the buffer order is meaningless for percentile
	// math but mutating the ring in place would lose the ring
	// semantics for callers that ever want a different percentile.
	buf := make([]float64, n)
	copy(buf, c.latency[:n])
	sort.Float64s(buf)
	// nearest-rank method: ceil(0.95 * n) - 1, clamped.
	idx := int(math.Ceil(0.95*float64(n))) - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= n {
		idx = n - 1
	}
	return buf[idx] * 1000.0
}
