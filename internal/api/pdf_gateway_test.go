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
// response headers are exact, and (optionally) captures the per-request
// opts so tests can assert the proto fields plumb through.
type stubRedactor struct {
	out   []byte
	stats core.RedactStats
	// lastOpts is set on every Redact call; tests read it after the
	// HTTP roundtrip to verify request-binding. Not safe for concurrent
	// requests; fine in test scope.
	lastOpts *core.RedactOptions
}

func (s *stubRedactor) Redact(_ context.Context, raw []byte, opts core.RedactOptions) ([]byte, core.RedactStats, error) {
	cp := opts
	s.lastOpts = &cp
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
	svc.SetPDFRedactor(&stubRedactor{
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
	// Order is map-iteration order; just assert membership.
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
	svc.SetPDFRedactor(&stubRedactor{})
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
	// No SetPDFRedactor call; service stays in the unconfigured state.
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
	svc.SetPDFRedactor(&stubRedactor{})
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
	svc.SetPDFRedactor(&stubRedactor{})
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

	// 2) Reveal: original bytes come back byte-exact. Intentionally NO
	// Accept header set; proves the dedicated GET handler returns raw
	// PDF bytes regardless of Accept, which used to fail with
	// Accept: */* under the gateway's default JSON marshaler.
	rev, _ := http.NewRequest(http.MethodGet, srv.URL+"/v1/anonymizations/"+id+"/reveal-pdf", nil)
	rev.Header.Set(headerTenant, "demo")
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

// TestRevealPDF_TenantViaQuery confirms the dedicated GET handler honors
// the ?tenant= query param when no X-Anonde-Tenant header is sent,
// matching the gateway's tenant-binding behaviour for the POST.
func TestRevealPDF_TenantViaQuery(t *testing.T) {
	svc := newTestService()
	original := []byte("%PDF-1.4\nq-tenant")
	svc.SetPDFRedactor(&stubRedactor{out: original})
	srv := httptest.NewServer(NewHTTPServer(svc).Routes())
	t.Cleanup(srv.Close)

	post, _ := http.NewRequest(http.MethodPost, srv.URL+"/v1/anonymizations/pdf?tenantId=demo", bytes.NewReader(original))
	post.Header.Set("Content-Type", mimeApplicationPDF)
	pr, err := srv.Client().Do(post)
	if err != nil {
		t.Fatalf("anonymize: %v", err)
	}
	id := pr.Header.Get("X-Anonde-Id")
	_, _ = io.Copy(io.Discard, pr.Body)
	pr.Body.Close()

	get, _ := http.NewRequest(http.MethodGet, srv.URL+"/v1/anonymizations/"+id+"/reveal-pdf?tenant=demo", nil)
	resp, err := srv.Client().Do(get)
	if err != nil {
		t.Fatalf("reveal: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, string(raw))
	}
	got, _ := io.ReadAll(resp.Body)
	if !bytes.Equal(got, original) {
		t.Fatalf("reveal body mismatch: got %q want %q", got, original)
	}
}

// TestRevealPDF_MissingTenant covers the explicit 400 when neither
// header nor query param carries the tenant.
func TestRevealPDF_MissingTenant(t *testing.T) {
	svc := newTestService()
	svc.SetPDFRedactor(&stubRedactor{})
	srv := httptest.NewServer(NewHTTPServer(svc).Routes())
	t.Cleanup(srv.Close)

	resp, err := srv.Client().Get(srv.URL + "/v1/anonymizations/anon_x/reveal-pdf")
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

// TestRESTPDFEndpoint_QueryParamsPlumbThrough proves the new per-request
// fields (mode, dpi, box_padding, entities, score_threshold, ocr_langs,
// disable_visual_heuristic) bind from URL query string and land in the
// core.RedactOptions struct the redactor sees. Smoke test of the
// grpc-gateway wire-binding for the extended AnonymizePDFRequest.
func TestRESTPDFEndpoint_QueryParamsPlumbThrough(t *testing.T) {
	svc := newTestService()
	stub := &stubRedactor{}
	svc.SetPDFRedactor(stub)
	srv := httptest.NewServer(NewHTTPServer(svc).Routes())
	t.Cleanup(srv.Close)

	url := srv.URL + "/v1/anonymizations/pdf" +
		"?mode=visual" +
		"&dpi=300" +
		"&box_padding=4" +
		"&disable_visual_heuristic=true" +
		"&ocr_langs=eng%2Bron" +
		"&score_threshold=0.5" +
		"&score_threshold_set=true" +
		"&entities=PERSON&entities=LOCATION" +
		"&disable_ner=true" +
		"&operator=redact"
	req, _ := http.NewRequest(http.MethodPost, url, bytes.NewReader([]byte("%PDF-1.4\nx")))
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

	got := stub.lastOpts
	if got == nil {
		t.Fatalf("redactor.lastOpts is nil; redactor was never called")
	}
	if got.Mode != "visual" {
		t.Errorf("Mode = %q, want visual", got.Mode)
	}
	if got.DPI != 300 {
		t.Errorf("DPI = %d, want 300", got.DPI)
	}
	if got.BoxPadding != 4 {
		t.Errorf("BoxPadding = %d, want 4", got.BoxPadding)
	}
	if !got.DisableVisualHeuristic {
		t.Errorf("DisableVisualHeuristic = false, want true")
	}
	if got.OCRLangs != "eng+ron" {
		t.Errorf("OCRLangs = %q, want eng+ron", got.OCRLangs)
	}
	if got.ScoreThreshold != 0.5 || !got.ScoreThresholdSet {
		t.Errorf("ScoreThreshold = %v (set=%v), want 0.5 (set=true)", got.ScoreThreshold, got.ScoreThresholdSet)
	}
	if !got.DisableNER {
		t.Errorf("DisableNER = false, want true")
	}
	if got.Operator != "redact" {
		t.Errorf("Operator = %q, want redact", got.Operator)
	}
	if len(got.Entities) != 2 || got.Entities[0] != "PERSON" || got.Entities[1] != "LOCATION" {
		t.Errorf("Entities = %v, want [PERSON LOCATION]", got.Entities)
	}
}
