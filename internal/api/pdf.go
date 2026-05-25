// PDF redaction HTTP endpoints — POST /v1/anonymizations/pdf and
// GET /v1/anonymizations/{id}/reveal-pdf.
//
// The POST handler accepts a raw application/pdf body (or a
// multipart/form-data with a "file" field) and returns the redacted
// PDF bytes as application/pdf. It also mints an anonymization id
// and persists the original + redacted bytes in the store so
// /v1/anonymizations/{id}/reveal-pdf can return the exact original.
//
// Mirrors the cmd/anonymize-pdf CLI behaviour in-process so HTTP
// clients get identical output for the same input.
//
// Wired in http.go's Routes(). The handler is a no-op (returns 501)
// until the operator calls HTTPServer.SetPDFRedactor — that
// dependency injection point keeps the api package free of analyzer
// + ONNX deps so the patterns-only Dockerfile.anonde still builds
// without `-tags hugot`.

package api

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/anonde-io/anonde/internal/content"
	"github.com/anonde-io/anonde/internal/core"
)

// PDFRedactor performs PDF → redacted PDF on a request. Implementations
// wrap the engine + analysis config + optional visual detector that
// the operator has chosen at boot time.
type PDFRedactor interface {
	RedactPDF(ctx context.Context, raw []byte) ([]byte, RedactStats, error)
}

// RedactStats is a lightweight summary the handler echoes as response
// headers so callers can log entity counts without a second request.
type RedactStats struct {
	EntityCount int
	TypeCount   int
	ByType      map[string]int
}

// SetPDFRedactor injects the redactor used by POST /v1/anonymizations/pdf.
// Without this call the endpoint returns 501 Not Implemented with a
// hint pointing at ANONDE_PDF_ENABLED=1.
func (s *HTTPServer) SetPDFRedactor(r PDFRedactor) {
	s.pdfRedactor = r
}

func (s *HTTPServer) anonymizePDF(w http.ResponseWriter, r *http.Request) {
	if s.pdfRedactor == nil {
		http.Error(w,
			"PDF redaction not configured on this server. "+
				"Start with ANONDE_PDF_ENABLED=1 and ANALYZER_BACKEND=gliner (requires -tags hugot build).",
			http.StatusNotImplemented)
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	tenantID := tenantFromRequest(r)
	if tenantID == "" {
		http.Error(w,
			"missing tenant id — send `X-Anonde-Tenant: <id>` header or `?tenant=<id>` query",
			http.StatusBadRequest)
		return
	}

	raw, err := readPDFBody(r)
	if err != nil {
		http.Error(w, "read body: "+err.Error(), http.StatusBadRequest)
		return
	}
	if len(raw) == 0 {
		http.Error(w, "empty PDF body", http.StatusBadRequest)
		return
	}

	redacted, stats, err := s.pdfRedactor.RedactPDF(r.Context(), raw)
	if err != nil {
		http.Error(w, "redact: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Persist the original + redacted bytes so `GET /v1/anonymizations/
	// {id}/reveal-pdf` can return the original artefact. Honors the
	// server's TTL (ANONDE_STORE_TTL); 0 = retained until DELETE.
	id := s.svc.NewAnonymizationID()
	saveErr := s.svc.SaveRecord(r.Context(), core.StoreRecord{
		TenantID:        tenantID,
		ID:              id,
		ContentFormat:   "pdf",
		OriginalBytes:   raw,
		AnonymizedBytes: redacted,
	})
	if saveErr != nil {
		// Best-effort: log on the response header so the operator
		// sees it, but still return the redacted PDF — the
		// redaction itself succeeded. Reveal will 404.
		w.Header().Set("X-Anonde-Save-Error", saveErr.Error())
	}

	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Length", strconv.Itoa(len(redacted)))
	w.Header().Set("X-Anonde-Id", id)
	w.Header().Set("X-Anonde-Tenant", tenantID)
	w.Header().Set("X-Anonde-Entities", strconv.Itoa(stats.EntityCount))
	w.Header().Set("X-Anonde-Entity-Types", strconv.Itoa(stats.TypeCount))
	for t, n := range stats.ByType {
		w.Header().Add("X-Anonde-Entity-Count", fmt.Sprintf("%s=%d", t, n))
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(redacted)
}

// revealPDF returns the original (pre-anonymization) PDF bytes for an
// anonymization id stored via the PDF endpoint. Returns 404 when the
// record doesn't exist, has expired, or was stored without
// OriginalBytes (i.e. it was a text-format anonymization).
func (s *HTTPServer) revealPDF(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		http.Error(w, "missing id in path", http.StatusBadRequest)
		return
	}
	tenantID := tenantFromRequest(r)
	if tenantID == "" {
		http.Error(w,
			"missing tenant id — send `X-Anonde-Tenant: <id>` header or `?tenant=<id>` query",
			http.StatusBadRequest)
		return
	}
	rec, err := s.svc.GetRecord(r.Context(), tenantID, id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	if len(rec.OriginalBytes) == 0 {
		http.Error(w,
			"anonymization "+id+" has no original PDF stored (was it created via /v1/anonymizations/pdf?)",
			http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Length", strconv.Itoa(len(rec.OriginalBytes)))
	w.Header().Set("X-Anonde-Id", id)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(rec.OriginalBytes)
}

// tenantFromRequest accepts the tenant id from either the
// `X-Anonde-Tenant` header (preferred — survives proxies that strip
// query strings from logs) or the `?tenant=` query param (for quick
// curl tests). Returns "" if neither is set.
func tenantFromRequest(r *http.Request) string {
	if v := strings.TrimSpace(r.Header.Get("X-Anonde-Tenant")); v != "" {
		return v
	}
	return strings.TrimSpace(r.URL.Query().Get("tenant"))
}

// readPDFBody accepts either a raw `application/pdf` body or a
// multipart/form-data with a "file" field — the two shapes the
// commercial PII gateways (Private AI, Limina) typically expose, so
// callers can swap base URLs without rewriting their integration.
func readPDFBody(r *http.Request) ([]byte, error) {
	ct := r.Header.Get("Content-Type")
	if ct == "application/pdf" || ct == "application/octet-stream" {
		return io.ReadAll(r.Body)
	}
	// multipart: parse and grab the first file field named "file".
	if err := r.ParseMultipartForm(64 << 20); err != nil {
		// Fall back to raw read — caller might have sent without an
		// explicit Content-Type header.
		var buf bytes.Buffer
		if _, copyErr := io.Copy(&buf, r.Body); copyErr != nil {
			return nil, copyErr
		}
		if buf.Len() > 0 {
			return buf.Bytes(), nil
		}
		return nil, err
	}
	fhs := r.MultipartForm.File["file"]
	if len(fhs) == 0 {
		return nil, fmt.Errorf(`no "file" field in multipart form`)
	}
	f, err := fhs[0].Open()
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(f)
}

// pdfRedactorImpl is the standard implementation: it wraps a
// content.RedactPDFOptions populated at server boot, and forwards each
// request through content.RedactPDFVisual. Lives here (not under
// cmd/anonde) so test wiring can construct it without touching the
// server bootstrap.
type pdfRedactorImpl struct {
	opts content.RedactPDFOptions
}

// NewPDFRedactor returns a PDFRedactor backed by content.RedactPDFVisual.
// Callers supply a fully-populated RedactPDFOptions (engine, analysis
// cfg, optional visual detector, DPI, etc).
func NewPDFRedactor(opts content.RedactPDFOptions) PDFRedactor {
	return &pdfRedactorImpl{opts: opts}
}

func (p *pdfRedactorImpl) RedactPDF(ctx context.Context, raw []byte) ([]byte, RedactStats, error) {
	out, findings, err := content.RedactPDFVisual(ctx, raw, p.opts)
	if err != nil {
		return nil, RedactStats{}, err
	}
	byType := map[string]int{}
	for _, f := range findings {
		byType[f.EntityType]++
	}
	return out, RedactStats{
		EntityCount: len(findings),
		TypeCount:   len(byType),
		ByType:      byType,
	}, nil
}
