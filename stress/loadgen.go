//go:build stress

package stress

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	vegeta "github.com/tsenart/vegeta/v12/lib"
)

// loadgen.go wraps vegeta for the stress tier. The wrapper is thin
// because vegeta already covers the common ground (constant-rate
// attacker, latency percentiles, status-code histograms, success
// rate, throughput). We add:
//
//   - Per-anonde-endpoint target builders that produce the right
//     POST body + headers and don't get tripped up by Connect's
//     case-insensitive JSON.
//   - A Result wrapper that round-trips through testing.TB for
//     pretty assertions ("Success ≥ 0.99", "P99 < 2s") and dumps
//     a Markdown table into GITHUB_STEP_SUMMARY when running in CI.
//
// Why vegeta over a roll-your-own load gen: anonde's stress matrix
// will grow. vegeta's HDR-backed histogram, attack-rate scheduler,
// and reporter format are battle-tested and let the test code stay
// focused on the anonde-specific assertions.

// Attack describes one load run: a single target (or a small fixed
// set), a constant RPS, and a duration. For more complex scenarios
// (ramp, mixed targets), stack multiple Attack calls in series in
// the test.
type Attack struct {
	Name      string         // Test-facing label; lands in the result + summary.
	Targets   []vegeta.Target // Target(s) to round-robin through.
	Rate      int             // Requests per second per target.
	Duration  time.Duration   // Total attack window.
	Timeout   time.Duration   // Per-request timeout. Zero = vegeta default (30s).
	Workers   uint64          // Concurrent vegeta workers. Zero = vegeta default (10).
	MaxBody   int64           // Truncate result.Body capture at N bytes. Zero = 4 KiB cap.
	OnResult  func(vegeta.Result) // Optional per-result hook (e.g. assert no PII leaked back).
}

// Run executes the attack and returns vegeta's aggregated Metrics
// plus a flat list of any status-code-bucket / network errors. The
// caller asserts thresholds against the Metrics struct.
func (a Attack) Run(t *testing.T) (*vegeta.Metrics, []string) {
	t.Helper()
	if a.Rate <= 0 {
		t.Fatalf("stress[%s]: Rate must be > 0", a.Name)
	}
	if a.Duration <= 0 {
		t.Fatalf("stress[%s]: Duration must be > 0", a.Name)
	}
	if len(a.Targets) == 0 {
		t.Fatalf("stress[%s]: no targets", a.Name)
	}
	timeout := a.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	workers := a.Workers
	if workers == 0 {
		workers = 10
	}
	maxBody := a.MaxBody
	if maxBody == 0 {
		maxBody = 4 << 10
	}

	targeter := vegeta.NewStaticTargeter(a.Targets...)
	attacker := vegeta.NewAttacker(
		vegeta.Timeout(timeout),
		vegeta.Workers(workers),
		vegeta.MaxBody(maxBody),
		// Keep-alives ON: anonde reuses Connect/H2 streams; closing
		// the connection per request would make every result include
		// dial latency, which hides the pipeline cost we're testing.
		vegeta.KeepAlive(true),
	)

	var metrics vegeta.Metrics
	errs := []string{}
	for res := range attacker.Attack(targeter, vegeta.Rate{Freq: a.Rate, Per: time.Second}, a.Duration, a.Name) {
		metrics.Add(res)
		if res.Error != "" {
			errs = append(errs, res.Error)
		}
		if a.OnResult != nil {
			a.OnResult(*res)
		}
	}
	metrics.Close()
	return &metrics, errs
}

// -----------------------------------------------------------------------------
// Target builders
// -----------------------------------------------------------------------------

// TargetCreateAnonymization builds a POST /v1/anonymizations target
// with a JSON body. tenantID is required; idPrefix is optional and,
// when non-empty, lets the server mint deterministic-per-test ids.
//
// content can be any string the analyzer should chew on; for the
// PII-density tests, prefer the synthesized corpus in
// fixtures.go (TODO).
func TargetCreateAnonymization(baseURL, tenantID, idPrefix, contentFormat, content string) vegeta.Target {
	body := map[string]any{
		"tenant_id":      tenantID,
		"content_format": contentFormat,
		"content":        content,
	}
	if idPrefix != "" {
		body["id"] = idPrefix
	}
	raw, _ := json.Marshal(body)
	return vegeta.Target{
		Method: http.MethodPost,
		URL:    baseURL + "/v1/anonymizations",
		Body:   raw,
		Header: http.Header{"Content-Type": []string{"application/json"}},
	}
}

// TargetAnonymizePDF builds a POST /v1/anonymizations/pdf target
// with a raw PDF body. Used by the PDF stress cases.
func TargetAnonymizePDF(baseURL, tenantID string, pdf []byte) vegeta.Target {
	return vegeta.Target{
		Method: http.MethodPost,
		URL:    baseURL + "/v1/anonymizations/pdf",
		Body:   pdf,
		Header: http.Header{
			"Content-Type":    []string{"application/pdf"},
			"X-Anonde-Tenant": []string{tenantID},
		},
	}
}

// TargetHealth builds a GET /v1/health target — used by the
// multi-tenant fairness test as cheap "is the server still serving
// anyone?" probe traffic that competes for the concurrency budget.
func TargetHealth(baseURL string) vegeta.Target {
	return vegeta.Target{Method: http.MethodGet, URL: baseURL + "/v1/health"}
}

// -----------------------------------------------------------------------------
// Assertions + reporters
// -----------------------------------------------------------------------------

// AssertOK is the canonical pass/fail check. Fails the test if the
// success rate or status-code histogram looks wrong. Tests that
// expect non-OK outcomes (e.g. body-cap edge → all 4xx) use the
// raw metrics struct directly.
func AssertOK(t *testing.T, m *vegeta.Metrics, minSuccess float64, maxP99 time.Duration) {
	t.Helper()
	if m.Success < minSuccess {
		t.Errorf("success=%.3f, want >= %.3f. status histogram=%v errors[0]=%q",
			m.Success, minSuccess, m.StatusCodes, firstErr(m.Errors))
	}
	if maxP99 > 0 && m.Latencies.P99 > maxP99 {
		t.Errorf("P99=%v, want <= %v", m.Latencies.P99, maxP99)
	}
}

// firstErr returns the first error string from vegeta's Errors slice,
// or "" — handy because m.Errors is a sorted-by-frequency map flattened
// to []string at Close() time.
func firstErr(es []string) string {
	if len(es) == 0 {
		return ""
	}
	return es[0]
}

// Summarize writes a human-readable, copy-pasteable summary of one
// attack's metrics. Called per-subtest in t.Log; the CI workflow
// post-processes this format into a Markdown table in the job summary.
func Summarize(t *testing.T, attackName, variant string, m *vegeta.Metrics) {
	t.Helper()
	t.Logf(
		"\n[stress] attack=%s variant=%s\n"+
			"  rate    : %.1f req/s actual (%d requests)\n"+
			"  success : %.3f\n"+
			"  latency : p50=%v p95=%v p99=%v max=%v\n"+
			"  status  : %s\n",
		attackName, variant,
		m.Throughput, m.Requests,
		m.Success,
		m.Latencies.P50, m.Latencies.P95, m.Latencies.P99, m.Latencies.Max,
		statusHistogram(m.StatusCodes),
	)
}

func statusHistogram(in map[string]int) string {
	if len(in) == 0 {
		return "(none)"
	}
	parts := make([]string, 0, len(in))
	for code, n := range in {
		parts = append(parts, fmt.Sprintf("%s=%d", code, n))
	}
	return strings.Join(parts, " ")
}
