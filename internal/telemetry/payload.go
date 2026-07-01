// Package telemetry implements anonde's opt-out, privacy-respecting
// usage heartbeat.
//
// Privacy invariant
// -----------------
//
// Every field that leaves the process is enumerated in Heartbeat
// below. The CI gate test (TestHeartbeatFieldAllowlist) marshals an
// instance and asserts the JSON keys match a hardcoded allowlist;
// adding a new field requires a deliberate code change. PII, raw
// input/output text, vault contents, tenant IDs, doc IDs, hostnames
// and IP addresses are NEVER collected, even in-memory, so they
// cannot accidentally end up in a future payload field.
//
// The matching design contract in the README is load-bearing; if you
// change what's sent here, update the README section and the privacy
// policy on anonde.io.
package telemetry

// Heartbeat is the wire payload sent to the telemetry endpoint. The
// JSON keys are the wire contract; do not rename them without
// updating the Cloudflare Worker schema and the gate test allowlist.
type Heartbeat struct {
	// InstallID is a per-install random UUID v4 generated on first
	// boot and persisted to disk. Stable across restarts; no PII, no
	// hostname, no IP derivation.
	InstallID string `json:"install_id"`

	// Version is the anonde build version (vcs.revision, when
	// available) so we can correlate heartbeats to a specific commit.
	Version string `json:"version"`

	// BuildTag is the optional Go build-tag label baked in at compile
	// time. "default" for the patterns-only binary, "ner" for the
	// NER variant.
	BuildTag string `json:"build_tag"`

	// OS + Arch identify the runtime platform (e.g. linux/amd64).
	// Used to inform whether ARM NER images are worth the build cost.
	OS   string `json:"os"`
	Arch string `json:"arch"`

	// Backend is the active analyzer backend
	// (patterns|gliner|gliner-flat).
	// Set once at boot; informs which deployment shape the post-launch
	// roadmap should harden first.
	Backend string `json:"backend"`

	// UptimeSeconds is process uptime at the moment the heartbeat is
	// snapshotted. Lets the dashboard distinguish a server that's
	// been up for 30 days from a fresh boot.
	UptimeSeconds int64 `json:"uptime_seconds"`

	// RequestCount is the 24h aggregate Service request count
	// (reset to zero after each heartbeat). Counts all operations:
	// ingest, reveal, detokenize, delete, synthesize, get_version.
	RequestCount uint64 `json:"request_count"`

	// ErrorCount is the 24h aggregate count of Service requests that
	// completed with status="error" (reset after each heartbeat).
	// Policy denials are NOT errors; they're an expected outcome.
	ErrorCount uint64 `json:"error_count"`

	// EntityCounts is a map of entity type → count across the 24h
	// window (reset after each heartbeat). Keys are the static
	// EntityType labels (PERSON, EMAIL_ADDRESS, IBAN_CODE, …); values
	// are integer occurrence counts. NO cleartext, NO scores, NO
	// per-doc breakdowns; just totals.
	EntityCounts map[string]uint64 `json:"entity_counts"`

	// P95LatencyMs is the 95th-percentile end-to-end Service request
	// duration in milliseconds, computed across a fixed-size rolling
	// sample (see Collector.ringSize). 0 when no requests landed in
	// the window.
	P95LatencyMs float64 `json:"p95_latency_ms"`
}

// HeartbeatAllowedFields is the canonical wire-key allowlist. The
// gate test asserts the marshalled Heartbeat keys match this set
// exactly, so any new field added to Heartbeat MUST also be added
// here in the same commit — that's the explicit speed bump the
// telemetry section of the launch plan calls for.
var HeartbeatAllowedFields = map[string]struct{}{
	"install_id":      {},
	"version":         {},
	"build_tag":       {},
	"os":              {},
	"arch":            {},
	"backend":         {},
	"uptime_seconds":  {},
	"request_count":   {},
	"error_count":     {},
	"entity_counts":   {},
	"p95_latency_ms":  {},
}
