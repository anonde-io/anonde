package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestConnect_IngestRejectsOversizedBody verifies the Connect handler
// honors WithReadMaxBytes. Connect maps body-too-large to
// CodeResourceExhausted, which the Connect/JSON spec renders as HTTP
// 429 (Too Many Requests). Not HTTP 413; that's a deliberate Connect
// design choice. Asserting on the status code locks in the contract
// so a future Connect upgrade that changes the mapping fails loudly.
//
// The protojson codec accepts both lowerCamelCase and snake_case on
// input; this test uses camelCase to also pin that contract.
func TestConnect_IngestRejectsOversizedBody(t *testing.T) {
	svc := newTestService()
	srv := NewHTTPServer(svc)
	srv.SetMaxRequestBytes(64)

	ts := httptest.NewServer(srv.Routes())
	defer ts.Close()

	body := `{"tenantId":"acme","id":"d","contentFormat":"text","content":"` +
		strings.Repeat("a", 1024) + `"}`
	resp, err := http.Post(
		ts.URL+"/anonde.v1.Service/CreateAnonymization",
		"application/json",
		strings.NewReader(body),
	)
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d", resp.StatusCode)
	}
}
