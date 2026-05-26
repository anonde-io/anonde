// Package e2e is the merge-gate end-to-end test for the anonde HTTP
// service. It boots the real cmd/anonde binary on random ports, drives
// every public REST endpoint, and asserts that:
//
//   - Functional shape — every endpoint returns the documented JSON
//     fields and follows the round-trip invariant (anonymize → reveal
//     recovers cleartext byte-exactly).
//   - Observability — Prometheus counters reflect the traffic the test
//     just generated. A handler that silently no-ops still returns 200;
//     this layer catches that.
//   - Edge cases — boundary inputs (oversized bodies, malformed JSON,
//     missing tenants, unknown content formats, non-existent ids,
//     unicode, recursion) return the right HTTP status without
//     panicking or hanging.
//
// Runs in patterns-only mode (no CGO, no libonnxruntime, no tesseract)
// so it lands on every CI runner without special setup. The PDF
// raw-bytes endpoint (POST /v1/anonymizations/pdf) is exercised in its
// "not configured" mode (HTTP 501), which validates the opt-in path.
// Full visual-PDF coverage lives in the stress tier.
//
// Run:  make e2e   (or: go test ./e2e/... -count=1 -v)
package e2e

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// E2E is the single top-level entry point. Subtests share the booted
// server so the test reflects realistic accumulated state (Prometheus
// counters increment monotonically across subtests). Run with -run to
// target a single case during dev.
func TestE2E(t *testing.T) {
	srv := bootServer(t)
	defer srv.Stop()

	// Functional surface — every documented endpoint.
	t.Run("health", srv.testHealth)
	t.Run("version", srv.testVersion)
	t.Run("text_round_trip", srv.testTextRoundTrip)
	t.Run("json_round_trip", srv.testJSONRoundTrip)
	t.Run("ndjson_round_trip", srv.testNDJSONRoundTrip)
	t.Run("synthesize", srv.testSynthesize)
	t.Run("pdf_text_layer_via_create", srv.testPDFTextLayerViaCreate)
	t.Run("pdf_raw_endpoint_unconfigured", srv.testPDFRawEndpointUnconfigured)
	t.Run("delete_idempotent", srv.testDeleteIdempotent)

	// Edge cases — wrong inputs, boundaries.
	t.Run("edge_missing_tenant", srv.testEdgeMissingTenant)
	t.Run("edge_malformed_json", srv.testEdgeMalformedJSON)
	t.Run("edge_unknown_format", srv.testEdgeUnknownFormat)
	t.Run("edge_reveal_unknown_id", srv.testEdgeRevealUnknownID)
	t.Run("edge_oversized_body", srv.testEdgeOversizedBody)
	t.Run("edge_unicode_content", srv.testEdgeUnicodeContent)
	t.Run("edge_deeply_nested_json", srv.testEdgeDeeplyNestedJSON)
	t.Run("edge_tenant_isolation", srv.testEdgeTenantIsolation)

	// Observability — runs LAST so all counter increments above are
	// visible at scrape time.
	t.Run("metrics_reflect_traffic", srv.testMetricsReflectTraffic)
}

// -----------------------------------------------------------------------------
// Harness
// -----------------------------------------------------------------------------

type server struct {
	t          *testing.T
	binary     string
	httpAddr   string // 127.0.0.1:NNNN
	metricsURL string // http://127.0.0.1:MMMM/metrics
	cmd        *exec.Cmd
	stderr     *bytes.Buffer
	httpClient *http.Client
}

func bootServer(t *testing.T) *server {
	t.Helper()

	repoRoot := repoRoot(t)

	// Build once into a tempfile so subsequent test runs don't recompile.
	// `go build` here keeps the harness portable — no Makefile dependency.
	binDir := t.TempDir()
	binary := filepath.Join(binDir, "anonde")
	build := exec.Command("go", "build", "-o", binary, "./cmd/anonde")
	build.Dir = repoRoot
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build anonde binary: %v\n%s", err, out)
	}

	httpPort := freeTCPPort(t)
	metricsPort := freeTCPPort(t)
	httpAddr := fmt.Sprintf("127.0.0.1:%d", httpPort)
	metricsAddr := fmt.Sprintf("127.0.0.1:%d", metricsPort)

	stderr := &bytes.Buffer{}
	cmd := exec.Command(binary)
	cmd.Dir = repoRoot
	// Patterns backend keeps the test portable: no CGO, no ORT, no
	// tesseract. The text endpoints fully exercise the analyzer +
	// anonymizer + vault + store pipeline. Metrics binding is mandatory
	// — without it the /metrics scrape in the observability subtest
	// returns 404.
	cmd.Env = append(os.Environ(),
		"ANALYZER_BACKEND=patterns",
		"ANONDE_ADDR=:"+strconv.Itoa(httpPort),
		"METRICS_BIND="+metricsAddr,
		// Small body cap so the oversized-body edge case fires
		// without sending an actual 10 MiB body through the test
		// loopback. 64 KiB is plenty for the rest of the suite.
		"MAX_CONTENT_BYTES=65536",
		// Disable warmup — speeds the boot, the analyzer still
		// init-lazies on the first request.
		"WARMUP_ON_START=",
	)
	cmd.Stderr = stderr
	cmd.Stdout = io.Discard
	// Own process group so we can SIGTERM the whole tree on teardown
	// (covers the case where the binary spawns a child — today it
	// doesn't, but the safety net is cheap).
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	if err := cmd.Start(); err != nil {
		t.Fatalf("start anonde binary: %v", err)
	}

	srv := &server{
		t:          t,
		binary:     binary,
		httpAddr:   httpAddr,
		metricsURL: "http://" + metricsAddr + "/metrics",
		cmd:        cmd,
		stderr:     stderr,
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}

	if err := srv.waitReady(20 * time.Second); err != nil {
		t.Fatalf("server did not become ready: %v\nstderr:\n%s", err, stderr.String())
	}
	return srv
}

func (s *server) Stop() {
	if s.cmd == nil || s.cmd.Process == nil {
		return
	}
	_ = syscall.Kill(-s.cmd.Process.Pid, syscall.SIGTERM)
	done := make(chan error, 1)
	go func() { done <- s.cmd.Wait() }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		_ = syscall.Kill(-s.cmd.Process.Pid, syscall.SIGKILL)
		<-done
	}
	// If the binary panicked, surface it. Otherwise the test would
	// pass while the server died silently on the first request.
	if strings.Contains(s.stderr.String(), "panic:") {
		s.t.Errorf("server panicked during run:\n%s", s.stderr.String())
	}
}

func (s *server) waitReady(timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		resp, err := s.httpClient.Get("http://" + s.httpAddr + "/v1/health")
		if err == nil {
			_, _ = io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			if resp.StatusCode == 200 {
				return nil
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	return errors.New("timeout waiting for /v1/health")
}

func freeTCPPort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("probe free port: %v", err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	_ = l.Close()
	return port
}

func repoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	// The test runs from <repo>/e2e — climb one level.
	return filepath.Dir(wd)
}

// -----------------------------------------------------------------------------
// HTTP helpers
// -----------------------------------------------------------------------------

func (s *server) post(t *testing.T, path string, headers map[string]string, body []byte) *http.Response {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, "http://"+s.httpAddr+path, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := s.httpClient.Do(req)
	if err != nil {
		t.Fatalf("do %s %s: %v", http.MethodPost, path, err)
	}
	return resp
}

func (s *server) postJSON(t *testing.T, path string, body any) (int, map[string]any) {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	resp := s.post(t, path, nil, raw)
	defer resp.Body.Close()
	out, _ := readJSON(resp)
	return resp.StatusCode, out
}

func (s *server) get(t *testing.T, path string) *http.Response {
	t.Helper()
	resp, err := s.httpClient.Get("http://" + s.httpAddr + path)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	return resp
}

func (s *server) delete(t *testing.T, path string) (int, map[string]any) {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodDelete, "http://"+s.httpAddr+path, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	resp, err := s.httpClient.Do(req)
	if err != nil {
		t.Fatalf("DELETE %s: %v", path, err)
	}
	defer resp.Body.Close()
	out, _ := readJSON(resp)
	return resp.StatusCode, out
}

func readJSON(resp *http.Response) (map[string]any, error) {
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if len(body) == 0 {
		return nil, nil
	}
	var out map[string]any
	if err := json.Unmarshal(body, &out); err != nil {
		// Return the raw body for visibility in the eventual error
		// message — JSON we couldn't parse is more diagnostic as text.
		return map[string]any{"_raw": string(body)}, err
	}
	return out, nil
}

// -----------------------------------------------------------------------------
// Functional subtests
// -----------------------------------------------------------------------------

func (s *server) testHealth(t *testing.T) {
	resp := s.get(t, "/v1/health")
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status=%d, want 200", resp.StatusCode)
	}
}

func (s *server) testVersion(t *testing.T) {
	resp := s.get(t, "/v1/version")
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status=%d, want 200", resp.StatusCode)
	}
	out, _ := readJSON(resp)
	if got, _ := out["analyzer_backend"].(string); got != "patterns" {
		t.Fatalf("analyzer_backend = %q, want patterns", got)
	}
}

func (s *server) testTextRoundTrip(t *testing.T) {
	original := "Hi, I'm Sarah Chen at sarah.chen@acme.example. Card 4111-1111-1111-1111."
	status, body := s.postJSON(t, "/v1/anonymizations", map[string]any{
		"tenant_id":      "e2e",
		"id":             "text-1",
		"content_format": "text",
		"content":        original,
	})
	if status != 200 {
		t.Fatalf("anonymize status=%d body=%v", status, body)
	}
	anon, ok := body["anonymized_content"].(string)
	if !ok || anon == "" {
		t.Fatalf("missing anonymized_content: %v", body)
	}
	// The analyzer must replace at least the email + card with tokens.
	if strings.Contains(anon, "sarah.chen@acme.example") {
		t.Errorf("email leaked into anonymized output: %q", anon)
	}
	if strings.Contains(anon, "4111-1111-1111-1111") {
		t.Errorf("credit card leaked into anonymized output: %q", anon)
	}

	status, body = s.postJSON(t, "/v1/anonymizations/text-1/reveal", map[string]any{
		"tenant_id":      "e2e",
		"actor":          "test",
		"purpose":        "e2e",
		"content_format": "text",
		"content":        anon,
	})
	if status != 200 {
		t.Fatalf("reveal status=%d body=%v", status, body)
	}
	if got, _ := body["deanonymized_content"].(string); got != original {
		t.Fatalf("reveal did not recover original byte-exactly:\n got: %q\nwant: %q", got, original)
	}
}

func (s *server) testJSONRoundTrip(t *testing.T) {
	content := `{"patient":"Hans Müller","email":"hans@klinik.de"}`
	status, body := s.postJSON(t, "/v1/anonymizations", map[string]any{
		"tenant_id":      "e2e",
		"id":             "json-1",
		"content_format": "json",
		"content":        content,
	})
	if status != 200 {
		t.Fatalf("anonymize status=%d body=%v", status, body)
	}
	anon, _ := body["anonymized_content"].(string)
	if strings.Contains(anon, "hans@klinik.de") {
		t.Errorf("email leaked into anonymized JSON output: %q", anon)
	}

	status, body = s.postJSON(t, "/v1/anonymizations/json-1/reveal", map[string]any{
		"tenant_id":      "e2e",
		"actor":          "test",
		"purpose":        "e2e",
		"content_format": "json",
		"content":        anon,
	})
	if status != 200 {
		t.Fatalf("reveal status=%d body=%v", status, body)
	}
	got, _ := body["deanonymized_content"].(string)
	if !strings.Contains(got, "hans@klinik.de") {
		t.Errorf("reveal did not restore email: %q", got)
	}
}

func (s *server) testNDJSONRoundTrip(t *testing.T) {
	content := `{"name":"Alice"}` + "\n" + `{"name":"Bob","email":"bob@example.com"}`
	status, body := s.postJSON(t, "/v1/anonymizations", map[string]any{
		"tenant_id":      "e2e",
		"id":             "ndjson-1",
		"content_format": "ndjson",
		"content":        content,
	})
	if status != 200 {
		t.Fatalf("anonymize status=%d body=%v", status, body)
	}
	anon, _ := body["anonymized_content"].(string)
	if strings.Contains(anon, "bob@example.com") {
		t.Errorf("email leaked into ndjson output: %q", anon)
	}
}

func (s *server) testSynthesize(t *testing.T) {
	status, body := s.postJSON(t, "/v1/synthesize", map[string]any{
		"content":        "Email me at synth@example.com.",
		"content_format": "text",
	})
	if status != 200 {
		t.Fatalf("synthesize status=%d body=%v", status, body)
	}
	out, _ := body["content"].(string)
	if out == "" {
		t.Fatalf("missing synthesized content: %v", body)
	}
	if strings.Contains(out, "synth@example.com") {
		t.Errorf("synthesize leaked original email: %q", out)
	}
}

func (s *server) testPDFTextLayerViaCreate(t *testing.T) {
	// content_format=pdf path through POST /v1/anonymizations. Uses a
	// minimal PDF that has an extractable text layer so no OCR fires;
	// keeps the test patterns-only-compatible.
	pdfPath := filepath.Join(repoRoot(t), "internal", "content", "testdata", "pii_sample.pdf")
	raw, err := os.ReadFile(pdfPath)
	if err != nil {
		t.Skipf("text-layer PDF fixture missing (%s): %v", pdfPath, err)
	}
	encoded := base64.StdEncoding.EncodeToString(raw)
	status, body := s.postJSON(t, "/v1/anonymizations", map[string]any{
		"tenant_id":      "e2e",
		"id":             "pdf-1",
		"content_format": "pdf",
		"content":        encoded,
	})
	if status != 200 {
		t.Fatalf("anonymize pdf status=%d body=%v", status, body)
	}
	if anon, _ := body["anonymized_content"].(string); !strings.Contains(anon, "<") {
		t.Errorf("expected at least one token in anonymized PDF text, got: %q", anon)
	}
}

func (s *server) testPDFRawEndpointUnconfigured(t *testing.T) {
	// The raw-bytes endpoint needs a redactor wired via
	// ANONDE_PDF_ENABLED=1. The patterns-only test boot doesn't set
	// that, so we expect HTTP 501 — proves the opt-in error path
	// works without dragging CGO + ORT into the merge gate.
	resp := s.post(t, "/v1/anonymizations/pdf",
		map[string]string{
			"Content-Type":    "application/pdf",
			"X-Anonde-Tenant": "e2e",
		},
		[]byte("%PDF-1.4\nfake"))
	defer resp.Body.Close()
	if resp.StatusCode != 501 {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d, want 501 (unconfigured): %s", resp.StatusCode, body)
	}
}

func (s *server) testDeleteIdempotent(t *testing.T) {
	// First delete on a known id from testTextRoundTrip → deleted=true.
	status, body := s.delete(t, "/v1/anonymizations/text-1?tenantId=e2e")
	if status != 200 {
		t.Fatalf("first delete status=%d body=%v", status, body)
	}
	if got, _ := body["deleted"].(bool); !got {
		t.Errorf("first delete: deleted=%v, want true", got)
	}
	// Second delete on the same id → 200, deleted=false.
	status, body = s.delete(t, "/v1/anonymizations/text-1?tenantId=e2e")
	if status != 200 {
		t.Fatalf("second delete status=%d body=%v", status, body)
	}
	if got, _ := body["deleted"].(bool); got {
		t.Errorf("second delete: deleted=%v, want false (idempotent)", got)
	}
}

// -----------------------------------------------------------------------------
// Edge cases
// -----------------------------------------------------------------------------

func (s *server) testEdgeMissingTenant(t *testing.T) {
	status, body := s.postJSON(t, "/v1/anonymizations", map[string]any{
		"content_format": "text",
		"content":        "anything",
	})
	if status != 400 {
		t.Fatalf("status=%d, want 400 (missing tenant): %v", status, body)
	}
}

func (s *server) testEdgeMalformedJSON(t *testing.T) {
	// content_format=json with un-parseable content. Service must
	// reject with 4xx, not 5xx, and must not panic.
	status, body := s.postJSON(t, "/v1/anonymizations", map[string]any{
		"tenant_id":      "e2e",
		"content_format": "json",
		"content":        `{not json`,
	})
	if status < 400 || status >= 500 {
		t.Fatalf("status=%d, want 4xx for malformed JSON: %v", status, body)
	}
}

func (s *server) testEdgeUnknownFormat(t *testing.T) {
	status, body := s.postJSON(t, "/v1/anonymizations", map[string]any{
		"tenant_id":      "e2e",
		"content_format": "bogus",
		"content":        "anything",
	})
	if status < 400 || status >= 500 {
		t.Fatalf("status=%d, want 4xx for unknown format: %v", status, body)
	}
}

func (s *server) testEdgeRevealUnknownID(t *testing.T) {
	status, body := s.postJSON(t, "/v1/anonymizations/does-not-exist/reveal", map[string]any{
		"tenant_id":      "e2e",
		"actor":          "test",
		"purpose":        "e2e",
		"content_format": "text",
		"content":        "anything",
	})
	if status != 404 {
		t.Fatalf("status=%d, want 404 for unknown id: %v", status, body)
	}
}

func (s *server) testEdgeOversizedBody(t *testing.T) {
	// MAX_CONTENT_BYTES is 64 KiB on the test server. Send 128 KiB.
	// The gateway now wraps the gateway subtree in limitBody so REST
	// matches Connect's enforcement — a 4xx is mandatory. A 2xx here
	// would be a regression of the body-cap fix landed alongside this
	// assertion.
	big := strings.Repeat("a", 128*1024)
	status, _ := s.postJSON(t, "/v1/anonymizations", map[string]any{
		"tenant_id":      "e2e",
		"content_format": "text",
		"content":        big,
	})
	if status < 400 || status >= 500 {
		t.Fatalf("oversized body status=%d, want 4xx (body cap enforced on REST gateway)", status)
	}
}

func (s *server) testEdgeUnicodeContent(t *testing.T) {
	// Emoji + RTL + zero-width joiner + accented Latin. The analyzer
	// must process these without UTF-8 panics. We don't assert what
	// gets tokenised — recognizers are emoji-agnostic; we only assert
	// the request completes cleanly.
	content := "Hi 👋 from Naïve Café — العربية ‏שלום‍ test."
	status, body := s.postJSON(t, "/v1/anonymizations", map[string]any{
		"tenant_id":      "e2e",
		"id":             "unicode-1",
		"content_format": "text",
		"content":        content,
	})
	if status != 200 {
		t.Fatalf("unicode anonymize status=%d body=%v", status, body)
	}
}

func (s *server) testEdgeDeeplyNestedJSON(t *testing.T) {
	// 200 levels of nesting. Go's encoding/json is bounded by stack;
	// 200 is well under the panic line on a typical 8 MB stack, so
	// this exercises the "valid but deeply structured" case. Asserts
	// the JSON walker doesn't blow up.
	var b strings.Builder
	for i := 0; i < 200; i++ {
		b.WriteString(`{"x":`)
	}
	b.WriteString(`"alice@example.com"`)
	for i := 0; i < 200; i++ {
		b.WriteString(`}`)
	}
	status, body := s.postJSON(t, "/v1/anonymizations", map[string]any{
		"tenant_id":      "e2e",
		"id":             "nested-1",
		"content_format": "json",
		"content":        b.String(),
	})
	if status >= 500 {
		t.Fatalf("nested JSON status=%d (5xx — recursion bomb?) body=%v", status, body)
	}
}

func (s *server) testEdgeTenantIsolation(t *testing.T) {
	// Tenant A anonymizes a doc; tenant B must not be able to reveal
	// it (NotFound, not a permissions denial — the lookup is scoped
	// by (tenant, id) and the id silently doesn't exist for B).
	status, body := s.postJSON(t, "/v1/anonymizations", map[string]any{
		"tenant_id":      "tenant-a",
		"id":             "secret-1",
		"content_format": "text",
		"content":        "Email: alice@example.com",
	})
	if status != 200 {
		t.Fatalf("tenant-a anonymize status=%d body=%v", status, body)
	}
	anon, _ := body["anonymized_content"].(string)

	status, _ = s.postJSON(t, "/v1/anonymizations/secret-1/reveal", map[string]any{
		"tenant_id":      "tenant-b",
		"actor":          "test",
		"purpose":        "e2e",
		"content_format": "text",
		"content":        anon,
	})
	if status != 404 {
		t.Fatalf("cross-tenant reveal status=%d, want 404", status)
	}
}

// -----------------------------------------------------------------------------
// Observability
// -----------------------------------------------------------------------------

func (s *server) testMetricsReflectTraffic(t *testing.T) {
	resp, err := s.httpClient.Get(s.metricsURL)
	if err != nil {
		t.Fatalf("scrape /metrics: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("metrics status=%d, want 200", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read metrics body: %v", err)
	}
	m := parsePromText(string(body))

	// Assertions: every counter we expect to have ticked at least once
	// given the traffic from the subtests above. The contract here is
	// "the pipeline actually fired" — not exact counts, because subtest
	// ordering can shift.
	assertCounterPositive(t, m, "anonde_requests_total")
	assertCounterPositive(t, m, "anonde_entities_detected_total")
	assertCounterPositive(t, m, "anonde_vault_ops_total")
	assertCounterPositive(t, m, "anonde_request_duration_seconds_count")

	// Negative assertion: nothing should be denying tenants in this
	// test (all subtests use a static allow-all policy). A non-zero
	// value here would indicate either a bug in the policy plumbing
	// or a real authorization regression.
	if v := m.sum("anonde_policy_denials_total"); v != 0 {
		t.Errorf("anonde_policy_denials_total = %g, want 0", v)
	}

	// Cross-check: there should be at least one "ok" status across
	// the requests counter. A test where every request returned an
	// error would still tick anonde_requests_total — this distinguishes
	// "errors-only" from "real traffic".
	if v := m.sumLabel("anonde_requests_total", "status", "ok"); v == 0 {
		t.Errorf("anonde_requests_total{status=\"ok\"} = 0, want >0 (every test errored?)")
	}
}

// promMetrics is a flattened view of a Prometheus text scrape. Each key
// is the series name; each value is the list of (labels, value) tuples.
type promMetrics map[string][]promSample

type promSample struct {
	labels map[string]string
	value  float64
}

func (m promMetrics) sum(name string) float64 {
	var s float64
	for _, samp := range m[name] {
		s += samp.value
	}
	return s
}

func (m promMetrics) sumLabel(name, labelKey, labelValue string) float64 {
	var s float64
	for _, samp := range m[name] {
		if samp.labels[labelKey] == labelValue {
			s += samp.value
		}
	}
	return s
}

// parsePromText handles the subset of the Prometheus text exposition
// format we care about: `name{labels} value [timestamp]` lines, plus
// commented `# HELP` / `# TYPE` lines we just skip. Good enough for
// the assertions above — we're reading the registry we wrote, not an
// adversarial source.
var promLineRE = regexp.MustCompile(`^([a-zA-Z_:][a-zA-Z0-9_:]*)(\{[^}]*\})?\s+(-?[0-9.eE+\-]+|NaN|\+Inf|-Inf)`)
var promLabelRE = regexp.MustCompile(`([a-zA-Z_][a-zA-Z0-9_]*)="((?:[^"\\]|\\.)*)"`)

func parsePromText(s string) promMetrics {
	out := promMetrics{}
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		m := promLineRE.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		name := m[1]
		labels := map[string]string{}
		if m[2] != "" {
			for _, lm := range promLabelRE.FindAllStringSubmatch(m[2], -1) {
				labels[lm[1]] = lm[2]
			}
		}
		v, err := strconv.ParseFloat(m[3], 64)
		if err != nil {
			continue
		}
		out[name] = append(out[name], promSample{labels: labels, value: v})
	}
	return out
}

func assertCounterPositive(t *testing.T, m promMetrics, name string) {
	t.Helper()
	if v := m.sum(name); v <= 0 {
		t.Errorf("%s sum = %g, want >0", name, v)
	}
}
