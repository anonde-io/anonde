package core

import (
	"context"
	"fmt"

	"github.com/anonde-io/anonde/internal/content"
)

// PDFRedactor performs PDF → redacted PDF on a request. The Service
// holds one (optional) instance, injected at boot via SetPDFRedactor.
// When nil, RedactPDF returns ErrPDFRedactorUnconfigured which the
// transport layer maps to gRPC Unimplemented / HTTP 501.
//
// Lives in core (not internal/api) so the gRPC handlers and the
// in-process gateway both reach the same Service.RedactPDF without
// having to thread the redactor through transport-specific wiring.
type PDFRedactor interface {
	Redact(ctx context.Context, raw []byte) (redacted []byte, stats RedactStats, err error)
}

// RedactStats is the per-request summary the transport layer echoes as
// X-Anonde-Entity-* response headers.
type RedactStats struct {
	EntityCount int
	TypeCount   int
	ByType      map[string]int
}

// ErrPDFRedactorUnconfigured is returned when SetPDFRedactor has not been
// called — the operator didn't opt in via ANONDE_PDF_ENABLED=1. Mapped
// to codes.Unimplemented / HTTP 501 by the transport layer so callers
// see a clear "not configured" signal.
var ErrPDFRedactorUnconfigured = fmt.Errorf("pdf redactor not configured: start the server with ANONDE_PDF_ENABLED=1 (requires the NER image or a -tags hugot build)")

// SetPDFRedactor injects the redactor used by AnonymizePDF. Safe to
// call once during server bootstrap. Leaving it unset disables the PDF
// surface entirely.
func (s *Service) SetPDFRedactor(r PDFRedactor) {
	s.pdfRedactor = r
}

// RedactPDF runs the configured redactor over raw PDF bytes, mints an
// anonymization id, and persists original + redacted bytes so RevealPDF
// can return the original later. The returned id matches the
// `anon_<hex>` shape used by the text endpoints.
//
// Returns ErrPDFRedactorUnconfigured when no redactor is wired — the
// transport layer maps that to HTTP 501.
func (s *Service) RedactPDF(ctx context.Context, tenantID string, raw []byte) (id string, redacted []byte, stats RedactStats, err error) {
	if s.pdfRedactor == nil {
		return "", nil, RedactStats{}, ErrPDFRedactorUnconfigured
	}
	if tenantID == "" {
		return "", nil, RedactStats{}, fmt.Errorf("tenant_id is required")
	}
	if len(raw) == 0 {
		return "", nil, RedactStats{}, fmt.Errorf("empty PDF body")
	}

	redacted, stats, err = s.pdfRedactor.Redact(ctx, raw)
	if err != nil {
		return "", nil, RedactStats{}, fmt.Errorf("redact: %w", err)
	}

	id = newAnonymizationID()
	rec := StoreRecord{
		TenantID:        tenantID,
		ID:              id,
		ContentFormat:   "pdf",
		OriginalBytes:   raw,
		AnonymizedBytes: redacted,
	}
	if saveErr := s.store.Put(ctx, rec); saveErr != nil {
		// Best-effort: the redaction itself succeeded, so we still
		// return the redacted bytes. The caller learns about the save
		// failure via the err return — they can decide whether to
		// surface it (server logs it via the gRPC error path) or
		// silently use the redacted PDF without later /reveal-pdf
		// capability. Mirrors the X-Anonde-Save-Error header behaviour
		// of the previous ad-hoc handler.
		return id, redacted, stats, fmt.Errorf("save: %w", saveErr)
	}
	return id, redacted, stats, nil
}

// GetOriginalPDF returns the original (pre-anonymization) PDF bytes for
// a stored anonymization. NotFound when the record doesn't exist, has
// expired, or was created via the text path (no OriginalBytes).
func (s *Service) GetOriginalPDF(ctx context.Context, tenantID, id string) ([]byte, error) {
	if tenantID == "" || id == "" {
		return nil, fmt.Errorf("tenant_id and id are required")
	}
	rec, err := s.store.Get(ctx, tenantID, id)
	if err != nil {
		return nil, err
	}
	if len(rec.OriginalBytes) == 0 {
		return nil, fmt.Errorf("anonymization %s not found: no original PDF stored (was it created via /v1/anonymizations/pdf?)", id)
	}
	return rec.OriginalBytes, nil
}

// pdfRedactorImpl is the standard implementation: it wraps a
// content.RedactPDFOptions populated at server boot, and forwards each
// request through content.RedactPDFVisual.
type pdfRedactorImpl struct {
	opts content.RedactPDFOptions
}

// NewPDFRedactor returns a PDFRedactor backed by content.RedactPDFVisual.
// Callers supply a fully-populated RedactPDFOptions (engine, analysis
// cfg, optional visual detector, DPI, etc).
func NewPDFRedactor(opts content.RedactPDFOptions) PDFRedactor {
	return &pdfRedactorImpl{opts: opts}
}

func (p *pdfRedactorImpl) Redact(ctx context.Context, raw []byte) ([]byte, RedactStats, error) {
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
