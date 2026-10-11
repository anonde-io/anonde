package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// getHealthz hits /healthz through the real route table and returns the
// status code plus the decoded {"status": ...} body.
func getHealthz(t *testing.T, srv *HTTPServer) (int, string) {
	t.Helper()
	ts := httptest.NewServer(srv.Routes())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/healthz")
	if err != nil {
		t.Fatalf("get /healthz: %v", err)
	}
	defer resp.Body.Close()

	var body map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode /healthz body: %v", err)
	}
	return resp.StatusCode, body["status"]
}

// A server nobody calls SetReady on must answer 200, so existing callers
// see no behaviour change from the gate.
func TestHealthz_ReadyByDefault(t *testing.T) {
	code, status := getHealthz(t, NewHTTPServer(newTestService()))
	if code != http.StatusOK {
		t.Errorf("status = %d, want %d", code, http.StatusOK)
	}
	if status != "ok" {
		t.Errorf("body status = %q, want %q", status, "ok")
	}
}

// SetReady(false) is what cmd/anonde holds while its model-load gates run;
// it must read as 503 so health-gated routing skips the instance.
func TestHealthz_NotReadyReports503(t *testing.T) {
	srv := NewHTTPServer(newTestService())
	srv.SetReady(false)

	code, status := getHealthz(t, srv)
	if code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want %d", code, http.StatusServiceUnavailable)
	}
	if status != "starting" {
		t.Errorf("body status = %q, want %q", status, "starting")
	}
}

func TestHealthz_ReadyAgainAfterGatesPass(t *testing.T) {
	srv := NewHTTPServer(newTestService())
	srv.SetReady(false)
	srv.SetReady(true)

	code, status := getHealthz(t, srv)
	if code != http.StatusOK {
		t.Errorf("status = %d, want %d", code, http.StatusOK)
	}
	if status != "ok" {
		t.Errorf("body status = %q, want %q", status, "ok")
	}
}

// The gate is flipped from a goroutine while requests are in flight.
func TestHealthz_SetReadyConcurrent(t *testing.T) {
	srv := NewHTTPServer(newTestService())
	ts := httptest.NewServer(srv.Routes())
	defer ts.Close()

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 200; i++ {
			srv.SetReady(i%2 == 0)
		}
	}()
	for i := 0; i < 200; i++ {
		resp, err := http.Get(ts.URL + "/healthz")
		if err != nil {
			t.Fatalf("get /healthz: %v", err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusServiceUnavailable {
			t.Fatalf("unexpected status %d", resp.StatusCode)
		}
	}
	<-done
}
