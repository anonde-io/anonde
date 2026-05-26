package api

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// syncBuffer is a minimal goroutine-safe bytes.Buffer adapter. The audit
// heartbeat goroutine and the request goroutine both call Write on the
// slog handler concurrently, so the underlying io.Writer has to be
// race-safe.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.Write(p)
}

func (s *syncBuffer) lines() []map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	raw := s.buf.String()
	var out []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(raw), "\n") {
		if line == "" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			continue
		}
		out = append(out, m)
	}
	return out
}

// installCapturingLogger swaps the package-level audit logger for one
// that writes to buf and restores the original on test cleanup.
func installCapturingLogger(t *testing.T) *syncBuffer {
	t.Helper()
	prev := auditLogger
	buf := &syncBuffer{}
	auditLogger = slog.New(slog.NewJSONHandler(buf, nil))
	t.Cleanup(func() { auditLogger = prev })
	return buf
}

func TestAudit_EmitsStartAndEndWithRequestID(t *testing.T) {
	buf := installCapturingLogger(t)

	h := auditMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	srv := httptest.NewServer(h)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/v1/whatever")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()

	id := resp.Header.Get(requestIDHeader)
	if id == "" {
		t.Fatalf("missing %s header", requestIDHeader)
	}
	if len(id) != 16 {
		t.Errorf("request id %q: want 16 hex chars, got %d", id, len(id))
	}

	lines := buf.lines()
	var sawStart, sawEnd bool
	for _, l := range lines {
		if l["msg"] == "request_start" && l["request_id"] == id {
			sawStart = true
		}
		if l["msg"] == "request_end" && l["request_id"] == id {
			sawEnd = true
			if status, _ := l["status"].(float64); status != 200 {
				t.Errorf("request_end status=%v, want 200", l["status"])
			}
			if b, _ := l["bytes_out"].(float64); b == 0 {
				t.Errorf("request_end bytes_out=0, expected non-zero (handler wrote a body)")
			}
		}
	}
	if !sawStart {
		t.Errorf("no request_start line for id=%s; got lines=%v", id, lines)
	}
	if !sawEnd {
		t.Errorf("no request_end line for id=%s; got lines=%v", id, lines)
	}
}

func TestAudit_SkipsHealthz(t *testing.T) {
	buf := installCapturingLogger(t)

	h := auditMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	srv := httptest.NewServer(h)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/healthz")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()

	if got := resp.Header.Get(requestIDHeader); got != "" {
		t.Errorf("/healthz should not set %s; got %q", requestIDHeader, got)
	}
	if lines := buf.lines(); len(lines) != 0 {
		t.Errorf("/healthz should not emit audit lines; got %v", lines)
	}
}

func TestAudit_HeartbeatFiresForSlowRequests(t *testing.T) {
	// Compress the cadence so the test is fast without losing meaning.
	prevThreshold, prevInterval := auditHeartbeatThreshold, auditHeartbeatInterval
	auditHeartbeatThreshold = 50 * time.Millisecond
	auditHeartbeatInterval = 50 * time.Millisecond
	t.Cleanup(func() {
		auditHeartbeatThreshold = prevThreshold
		auditHeartbeatInterval = prevInterval
	})

	buf := installCapturingLogger(t)

	h := auditMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	srv := httptest.NewServer(h)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/v1/slow")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()

	var heartbeats int
	for _, l := range buf.lines() {
		if l["msg"] == "request_inflight" {
			heartbeats++
		}
	}
	if heartbeats < 1 {
		t.Errorf("expected at least one request_inflight heartbeat, got 0; lines=%v", buf.lines())
	}
}

func TestAudit_RequestIDInContext(t *testing.T) {
	installCapturingLogger(t)

	var captured string
	h := auditMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured = RequestIDFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	}))
	srv := httptest.NewServer(h)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/v1/ctx")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()

	if captured == "" {
		t.Fatalf("RequestIDFromContext returned empty inside handler")
	}
	if got := resp.Header.Get(requestIDHeader); got != captured {
		t.Errorf("context id %q != response header %q", captured, got)
	}
}

// guard against the (unlikely) regression where a handler that never
// calls WriteHeader silently records status=0.
func TestAudit_DefaultStatusIs200(t *testing.T) {
	buf := installCapturingLogger(t)

	h := auditMiddleware(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		// no-op: never write a body or status
	}))
	srv := httptest.NewServer(h)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/v1/empty")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()

	for _, l := range buf.lines() {
		if l["msg"] != "request_end" {
			continue
		}
		if s, _ := l["status"].(float64); s != 200 {
			t.Errorf("default status logged as %v, want 200", l["status"])
		}
		return
	}
	t.Errorf("no request_end line found")
}

// keep the context import used even if the body changes; the
// RequestIDFromContext test above already exercises it but linters
// sometimes complain otherwise.
var _ = context.Background
