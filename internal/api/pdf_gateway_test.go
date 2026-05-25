package api

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/anonde-io/anonde/internal/core"
)

// stubRedactor lets the gateway smoke tests exercise the full path
// without pulling in CGO + libonnxruntime + a real PDF. It echoes a
// deterministic redacted-bytes payload and a known stats map so the
// response headers are exact.
type stubRedactor struct {
	out   []byte
	stats core.RedactStats
}

func (s stubRedactor) Redact(_ context.Context, raw []byte) ([]byte, core.RedactStats, error) {
	// Echo the input length as part of the output so the test can prove
	// the request body actually reached the redactor.
	out := append([]byte("REDACTED:"), raw...)
	if s.out != nil {
		out = s.out
	}
	return out, s.stats, nil
}

func TestRESTPDFEndpoint_TenantViaHeader(t *testing.T) {
	svc := newTestService()
	svc.SetPDFRedactor(stubRedactor{
		stats: core.RedactStats{
			EntityCount: 4,
			TypeCount:   2,
			ByType:      map[string]int{"PERSON": 3, "EMAIL_ADDRESS": 1},
		},
	})
	srv := httptest.NewServer(NewHTTPServer(svc).Routes())
	t.Cleanup(srv.Close)

	body := []byte("%PDF-1.4\nfake pdf body")
	req, err := http.NewRequest(http.MethodPost, srv.URL+"/v1/anonymizations/pdf", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", mimeApplicationPDF)
	req.Header.Set(headerTenant, "demo")
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, string(raw))
	}
	if got := resp.Header.Get("Content-Type"); got != mimeApplicationPDF {
		t.Fatalf("Content-Type = %q, want %q", got, mimeApplicationPDF)
	}
	if id := resp.Header.Get("X-Anonde-Id"); !strings.HasPrefix(id, "anon_") {
		t.Fatalf("X-Anonde-Id = %q, want anon_<hex>", id)
	}
	if got := resp.Header.Get("X-Anonde-Tenant"); got != "demo" {
		t.Fatalf("X-Anonde-Tenant = %q, want demo", got)
	}
	if got := resp.Header.Get("X-Anonde-Entities"); got != "4" {
		t.Fatalf("X-Anonde-Entities = %q, want 4", got)
	}
	if got := resp.Header.Get("X-Anonde-Entity-Types"); got != "2" {
		t.Fatalf("X-Anonde-Entity-Types = %q, want 2", got)
	}
	counts := resp.Header.Values("X-Anonde-Entity-Count")
	if len(counts) != 2 {
		t.Fatalf("X-Anonde-Entity-Count count = %d, want 2: %v", len(counts), counts)
	}
	// Order is map-iteration order — just assert membership.
	saw := map[string]bool{}
	for _, c := range counts {
		saw[c] = true
	}
	if !saw["PERSON=3"] || !saw["EMAIL_ADDRESS=1"] {
		t.Fatalf("expected PERSON=3 and EMAIL_ADDRESS=1 in counts: %v", counts)
	}

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if !bytes.HasPrefix(raw, []byte("REDACTED:")) {
		t.Fatalf("body did not flow through redactor; got %q", string(raw))
	}
	if !bytes.Contains(raw, body) {
		t.Fatalf("body did not echo input bytes: got %q want contains %q", string(raw), string(body))
	}
}

func TestRESTPDFEndpoint_TenantViaQuery(t *testing.T) {
	svc := newTestService()
	svc.SetPDFRedactor(stubRedactor{})
	srv := httptest.NewServer(NewHTTPServer(svc).Routes())
	t.Cleanup(srv.Close)

	body := []byte("%PDF-1.4\nq")
	req, err := http.NewRequest(http.MethodPost, srv.URL+"/v1/anonymizations/pdf?tenantId=demo", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", mimeApplicationPDF)
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, string(raw))
	}
	if got := resp.Header.Get("X-Anonde-Tenant"); got != "demo" {
		t.Fatalf("X-Anonde-Tenant = %q, want demo", got)
	}
}

func TestRESTPDFEndpoint_Unconfigured(t *testing.T) {
	// No SetPDFRedactor call — service stays in the unconfigured state.
	svc := newTestService()
	srv := httptest.NewServer(NewHTTPServer(svc).Routes())
	t.Cleanup(srv.Close)

	req, err := http.NewRequest(http.MethodPost, srv.URL+"/v1/anonymizations/pdf", bytes.NewReader([]byte("x")))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", mimeApplicationPDF)
	req.Header.Set(headerTenant, "demo")
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotImplemented {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 501 Not Implemented, got %d: %s", resp.StatusCode, string(raw))
	}
}

func TestRESTPDFEndpoint_MissingTenant(t *testing.T) {
	svc := newTestService()
	svc.SetPDFRedactor(stubRedactor{})
	srv := httptest.NewServer(NewHTTPServer(svc).Routes())
	t.Cleanup(srv.Close)

	req, err := http.NewRequest(http.MethodPost, srv.URL+"/v1/anonymizations/pdf", bytes.NewReader([]byte("x")))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", mimeApplicationPDF)
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 400 Bad Request for missing tenant, got %d: %s", resp.StatusCode, string(raw))
	}
}

func TestRESTPDFEndpoint_RevealRoundTrip(t *testing.T) {
	svc := newTestService()
	original := []byte("%PDF-1.4\noriginal payload")
	svc.SetPDFRedactor(stubRedactor{})
	srv := httptest.NewServer(NewHTTPServer(svc).Routes())
	t.Cleanup(srv.Close)

	// 1) Anonymize: store original under a minted id.
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/v1/anonymizations/pdf", bytes.NewReader(original))
	req.Header.Set("Content-Type", mimeApplicationPDF)
	req.Header.Set(headerTenant, "demo")
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("anonymize: %v", err)
	}
	id := resp.Header.Get("X-Anonde-Id")
	_, _ = io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	if !strings.HasPrefix(id, "anon_") {
		t.Fatalf("anonymize: expected anon_<hex>, got %q", id)
	}

	// 2) Reveal: original bytes come back byte-exact.
	rev, _ := http.NewRequest(http.MethodGet, srv.URL+"/v1/anonymizations/"+id+"/reveal-pdf", nil)
	rev.Header.Set(headerTenant, "demo")
	rev.Header.Set("Accept", mimeApplicationPDF)
	revResp, err := srv.Client().Do(rev)
	if err != nil {
		t.Fatalf("reveal: %v", err)
	}
	defer revResp.Body.Close()
	if revResp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(revResp.Body)
		t.Fatalf("reveal: expected 200, got %d: %s", revResp.StatusCode, string(raw))
	}
	if got := revResp.Header.Get("Content-Type"); got != mimeApplicationPDF {
		t.Fatalf("reveal Content-Type = %q, want %q", got, mimeApplicationPDF)
	}
	if got := revResp.Header.Get("X-Anonde-Id"); got != id {
		t.Fatalf("reveal X-Anonde-Id = %q, want %q", got, id)
	}
	got, err := io.ReadAll(revResp.Body)
	if err != nil {
		t.Fatalf("reveal read: %v", err)
	}
	if !bytes.Equal(got, original) {
		t.Fatalf("reveal body mismatch:\n got: %q\nwant: %q", string(got), string(original))
	}
}
