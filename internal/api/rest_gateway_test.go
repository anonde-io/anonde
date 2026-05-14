package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/anonde-io/anonde/internal/core"
)

// REST gateway smoke tests. The Connect tests already cover the proto
// shape; these tests pin down the gateway-specific behaviour: flat
// /v1/anonymizations routes, HTTP verbs map to the right RPC, error codes
// flow back as the right HTTP statuses, and tenant_id can travel via
// body (POSTs) or query param (DELETE) while we're pre-auth.

func newRESTTestEnv(t *testing.T) (*httptest.Server, *core.Service) {
	t.Helper()
	svc := newTestService()
	api := NewHTTPServer(svc)
	srv := httptest.NewServer(api.Routes())
	t.Cleanup(srv.Close)
	return srv, svc
}

func httpJSON(t *testing.T, srv *httptest.Server, method, path string, body any) (int, map[string]any) {
	t.Helper()
	var reader *bytes.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		reader = bytes.NewReader(b)
	} else {
		reader = bytes.NewReader(nil)
	}
	req, err := http.NewRequestWithContext(context.Background(), method, srv.URL+path, reader)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer resp.Body.Close()
	var out map[string]any
	if resp.ContentLength != 0 {
		if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
			t.Fatalf("decode: %v", err)
		}
	}
	return resp.StatusCode, out
}

func TestREST_IngestRevealDelete_RoundTrip(t *testing.T) {
	srv, _ := newRESTTestEnv(t)

	// POST /v1/anonymizations — tenant_id + doc_id in body.
	status, ing := httpJSON(t, srv, "POST", "/v1/anonymizations", map[string]any{
		"tenant_id":      "acme",
		"id":         "letter-001",
		"content_format": "text",
		"content":       "Email alice@example.com about the case",
	})
	if status != http.StatusOK {
		t.Fatalf("ingest: expected 200, got %d (%v)", status, ing)
	}
	if got := ing["anonymized_content"]; !strings.Contains(asString(got), "<EMAIL_ADDRESS_") {
		t.Fatalf("expected email token in anonymized content, got %v", got)
	}
	if ing["tenant_id"] != "acme" || ing["id"] != "letter-001" {
		t.Fatalf("response didn't echo tenant_id/doc_id; got tenant=%v doc=%v", ing["tenant_id"], ing["id"])
	}

	// POST /v1/anonymizations/{doc_id}/reveal — doc_id from URL, tenant_id in body.
	status, rev := httpJSON(t, srv, "POST", "/v1/anonymizations/letter-001/reveal", map[string]any{
		"tenant_id":      "acme",
		"actor":         "tester",
		"purpose":       "roundtrip",
		"content_format": "text",
		"content":       asString(ing["anonymized_content"]),
	})
	if status != http.StatusOK {
		t.Fatalf("reveal: expected 200, got %d (%v)", status, rev)
	}
	if !strings.Contains(asString(rev["deanonymized_content"]), "alice@example.com") {
		t.Fatalf("reveal didn't restore email, got %v", rev["deanonymized_content"])
	}

	// DELETE /v1/anonymizations/{doc_id}?tenant_id=acme — no body, tenant via query.
	status, del := httpJSON(t, srv, "DELETE", "/v1/anonymizations/letter-001?tenant_id=acme", nil)
	if status != http.StatusOK {
		t.Fatalf("delete: expected 200, got %d (%v)", status, del)
	}
	if del["deleted"] != true {
		t.Fatalf("expected deleted=true, got %v", del["deleted"])
	}
}

// TestREST_RevealAfterDelete_Maps404 verifies that the "not found" path
// surfaces as HTTP 404 via grpc-gateway's codes.NotFound mapping. The
// Connect surface would return 400 for the same error today; this is
// the gateway's value-add for REST clients.
func TestREST_RevealAfterDelete_Maps404(t *testing.T) {
	srv, _ := newRESTTestEnv(t)

	_, _ = httpJSON(t, srv, "POST", "/v1/anonymizations", map[string]any{
		"tenant_id":      "acme",
		"id":         "d",
		"content_format": "text",
		"content":       "Email alice@example.com",
	})
	_, _ = httpJSON(t, srv, "DELETE", "/v1/anonymizations/d?tenant_id=acme", nil)

	status, body := httpJSON(t, srv, "POST", "/v1/anonymizations/d/reveal", map[string]any{
		"tenant_id":      "acme",
		"actor":         "tester",
		"purpose":       "verify",
		"content_format": "text",
		"content":       "anything",
	})
	if status != http.StatusNotFound {
		t.Fatalf("expected 404 after delete, got %d (%v)", status, body)
	}
}

func TestREST_GetVersion(t *testing.T) {
	srv, svc := newRESTTestEnv(t)
	svc.SetVersionInfo(core.VersionInfo{
		AnalyzerBackend: "patterns",
		BuildSHA:        "abc123",
		APIVersion:      "v1",
	})

	// GET, no body.
	status, body := httpJSON(t, srv, "GET", "/v1/version", nil)
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d (%v)", status, body)
	}
	if body["analyzer_backend"] != "patterns" {
		t.Fatalf("unexpected backend: %v", body["analyzer_backend"])
	}
}

func TestREST_Synthesize(t *testing.T) {
	srv, _ := newRESTTestEnv(t)
	status, body := httpJSON(t, srv, "POST", "/v1/synthesize", map[string]any{
		"content_format": "text",
		"content":       "Email alice@example.com",
		"consistent":    true,
	})
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d (%v)", status, body)
	}
	if strings.Contains(asString(body["content"]), "alice@example.com") {
		t.Fatalf("expected email replaced, got %v", body["content"])
	}
}

func asString(v any) string {
	s, _ := v.(string)
	return s
}
