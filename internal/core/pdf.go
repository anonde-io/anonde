package core

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/anonde-io/anonde"
	"github.com/anonde-io/anonde/analyzer"
	"github.com/anonde-io/anonde/anonymizer"
	"github.com/anonde-io/anonde/anonymizer/operators"
	"github.com/anonde-io/anonde/internal/content"
)

// RedactOptions are the per-request knobs that override the boot-time
// defaults wired by NewPDFRedactor. Zero values fall back to the server
// default for that knob.
//
// Mirrors the flag surface of the (now-retired) cmd/anonymize-pdf CLI so
// every operator-facing knob is reachable over HTTP/gRPC/Connect.
type RedactOptions struct {
	// Mode picks the redaction strategy. "" or "visual" = visual (boxes
	// drawn on page rasters). "text" re-renders a text PDF with mask
	// substitutions instead.
	Mode string
	// Operator (text mode only): "mask" (default) or "redact".
	Operator string
	// MaskChar (text mode, mask operator): single character used as the
	// replacement. Default "#".
	MaskChar string
	// OCRLangs overrides ANONDE_OCR_LANGS for this request. Empty falls
	// back to the server-wide env default.
	OCRLangs string
	// Entities allow-list. Empty = every recognizer fires.
	Entities []string
	// ScoreThreshold overrides the boot-time analyzer threshold when
	// ScoreThresholdSet is true. Mirrors the AnalyzerOptions pattern on
	// the text endpoint so callers can distinguish "field present and 0"
	// (include everything) from "field absent" (use default).
	ScoreThreshold    float64
	ScoreThresholdSet bool
	// DPI for rasterisation (visual mode). 0 = server default (200).
	DPI int
	// BoxPadding pixels around each PII word box (visual mode). 0 =
	// server default (2).
	BoxPadding int
	// DisableVisualHeuristic turns off the ink-density heuristic that
	// catches signatures/stamps/logos. Zero value keeps the heuristic on
	// (matching the server default).
	DisableVisualHeuristic bool
	// DisableNER skips NER recognizers in the analyzer pipeline for this
	// request. Useful when the caller knows the document only contains
	// structured PII the patterns cover.
	DisableNER bool
}

// PDFRedactor performs PDF → redacted PDF on a request. The Service
// holds one (optional) instance, injected at boot via SetPDFRedactor.
// When nil, RedactPDF returns ErrPDFRedactorUnconfigured which the
// transport layer maps to gRPC Unimplemented / HTTP 501.
//
// Lives in core (not internal/api) so the gRPC handlers and the
// in-process gateway both reach the same Service.RedactPDF without
// having to thread the redactor through transport-specific wiring.
type PDFRedactor interface {
	Redact(ctx context.Context, raw []byte, opts RedactOptions) (redacted []byte, stats RedactStats, err error)
}

// RedactStats is the per-request summary the transport layer echoes as
// X-Anonde-Entity-* response headers.
type RedactStats struct {
	EntityCount int
	TypeCount   int
	ByType      map[string]int
}

// ErrPDFRedactorUnconfigured is returned when SetPDFRedactor has not been
// called; the operator didn't opt in via ANONDE_PDF_ENABLED=1. Mapped
// to codes.Unimplemented / HTTP 501 by the transport layer so callers
// see a clear "not configured" signal.
var ErrPDFRedactorUnconfigured = fmt.Errorf("pdf redactor not configured: start the server with ANONDE_PDF_ENABLED=1 (requires the NER image or a -tags ner build)")

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
// Returns ErrPDFRedactorUnconfigured when no redactor is wired; the
// transport layer maps that to HTTP 501.
func (s *Service) RedactPDF(ctx context.Context, tenantID string, raw []byte, opts RedactOptions) (id string, redacted []byte, stats RedactStats, err error) {
	if s.pdfRedactor == nil {
		return "", nil, RedactStats{}, ErrPDFRedactorUnconfigured
	}
	if tenantID == "" {
		return "", nil, RedactStats{}, fmt.Errorf("tenant_id is required")
	}
	if len(raw) == 0 {
		return "", nil, RedactStats{}, fmt.Errorf("empty PDF body")
	}

	redacted, stats, err = s.pdfRedactor.Redact(ctx, raw, opts)
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
		// failure via the err return; they can decide whether to
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

// pdfRedactorImpl is the standard implementation. It holds the
// boot-time defaults (engine, analysis cfg, vision detector, DPI,
// padding, heuristic on/off) and merges per-request RedactOptions on
// top before delegating to either the visual or text code path.
type pdfRedactorImpl struct {
	defaults content.RedactPDFOptions
}

// NewPDFRedactor returns a PDFRedactor backed by the content package.
// Callers supply a fully-populated RedactPDFOptions (engine, analysis
// cfg, optional visual detector, DPI, etc) that acts as the boot-time
// default; each request can override individual fields via
// RedactOptions.
func NewPDFRedactor(opts content.RedactPDFOptions) PDFRedactor {
	return &pdfRedactorImpl{defaults: opts}
}

func (p *pdfRedactorImpl) Redact(ctx context.Context, raw []byte, req RedactOptions) ([]byte, RedactStats, error) {
	switch strings.ToLower(strings.TrimSpace(req.Mode)) {
	case "", "visual":
		return p.redactVisual(ctx, raw, req)
	case "text":
		return p.redactText(ctx, raw, req)
	default:
		return nil, RedactStats{}, fmt.Errorf("unknown mode %q (use \"visual\" or \"text\")", req.Mode)
	}
}

// redactVisual clones the boot-time RedactPDFOptions, overlays the
// per-request knobs, and runs the visual pipeline.
func (p *pdfRedactorImpl) redactVisual(ctx context.Context, raw []byte, req RedactOptions) ([]byte, RedactStats, error) {
	opts := p.defaults
	opts.AnalysisCfg = mergeAnalysisCfg(p.defaults.AnalysisCfg, req)
	if req.DPI > 0 {
		opts.DPI = req.DPI
	}
	if req.BoxPadding > 0 {
		opts.BoxPadding = req.BoxPadding
	}
	if req.DisableVisualHeuristic {
		opts.VisualHeuristic = false
	}
	if req.OCRLangs != "" {
		opts.OCRLangs = req.OCRLangs
	}

	out, findings, err := content.RedactPDFVisual(ctx, raw, opts)
	if err != nil {
		return nil, RedactStats{}, err
	}
	return out, statsFromFindings(findings), nil
}

// redactText extracts the document text, runs the analyzer, applies
// the anonymizer, and re-renders the result as a fresh text PDF. The
// `operator` knob picks between Mask (default, prints `mask_char`) and
// Redact (<REDACTED> tokens).
func (p *pdfRedactorImpl) redactText(ctx context.Context, raw []byte, req RedactOptions) ([]byte, RedactStats, error) {
	if p.defaults.Engine == nil {
		return nil, RedactStats{}, fmt.Errorf("text mode: analyzer engine not configured")
	}
	// Text mode goes through ExtractAnalyzable's OCR fallback which
	// honors ANONDE_OCR_LANGS only; per-request OCRLangs has no effect
	// here. Callers who need per-request languages should use visual
	// mode, which threads opts.OCRLangs through directly.
	b64 := base64.StdEncoding.EncodeToString(raw)
	extracted, err := content.ExtractAnalyzable(b64, content.FormatPDF)
	if err != nil {
		return nil, RedactStats{}, fmt.Errorf("text mode: extract pdf text: %w", err)
	}
	if strings.TrimSpace(extracted) == "" {
		return nil, RedactStats{}, fmt.Errorf("text mode: no text extracted (no text layer and OCR unavailable; install pdftoppm + tesseract)")
	}

	cfg := mergeAnalysisCfg(p.defaults.AnalysisCfg, req)
	if cfg.Language == "" {
		cfg.Language = content.DetectLanguage(extracted)
	}
	findings, err := p.defaults.Engine.Analyze(ctx, extracted, cfg)
	if err != nil {
		return nil, RedactStats{}, fmt.Errorf("text mode: analyze: %w", err)
	}
	findings = anonymizer.MergeAdjacentSameType(findings, extracted)

	op, err := buildTextModeOperator(req.Operator, req.MaskChar)
	if err != nil {
		return nil, RedactStats{}, err
	}
	res, err := anonde.DefaultAnonymizerEngine().Anonymize(extracted, findings, anonymizer.AnonymizerConfig{"*": op})
	if err != nil {
		return nil, RedactStats{}, fmt.Errorf("text mode: anonymize: %w", err)
	}
	out, err := content.RenderTextAsPDF(res.Text)
	if err != nil {
		return nil, RedactStats{}, fmt.Errorf("text mode: render pdf: %w", err)
	}
	return out, statsFromFindings(findings), nil
}

// mergeAnalysisCfg clones the boot-time AnalysisConfig and overlays the
// per-request analyzer knobs. Always sets RemoveConflicts=true because
// the conflict resolver is load-bearing for the bench leak rate.
func mergeAnalysisCfg(base analyzer.AnalysisConfig, req RedactOptions) analyzer.AnalysisConfig {
	cfg := base
	cfg.RemoveConflicts = true
	if req.ScoreThresholdSet {
		cfg.ScoreThreshold = req.ScoreThreshold
	}
	if len(req.Entities) > 0 {
		cfg.Entities = req.Entities
	}
	if req.DisableNER {
		cfg.DisableNER = true
	}
	return cfg
}

func buildTextModeOperator(name, maskChar string) (anonymizer.Operator, error) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "", "mask":
		ch := "#"
		if maskChar != "" {
			ch = maskChar
		}
		return &operators.Mask{MaskingChar: ch}, nil
	case "redact":
		return &operators.Redact{}, nil
	default:
		return nil, fmt.Errorf("unsupported operator %q (use \"mask\" or \"redact\")", name)
	}
}

func statsFromFindings(findings []analyzer.RecognizerResult) RedactStats {
	byType := map[string]int{}
	for _, f := range findings {
		byType[f.EntityType]++
	}
	return RedactStats{
		EntityCount: len(findings),
		TypeCount:   len(byType),
		ByType:      byType,
	}
}
