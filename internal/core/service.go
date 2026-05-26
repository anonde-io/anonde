package core

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/anonde-io/anonde/analyzer"
	"github.com/anonde-io/anonde/anonymizer"
	"github.com/anonde-io/anonde/anonymizer/operators"
	"github.com/anonde-io/anonde/internal/content"
	"github.com/anonde-io/anonde/internal/metrics"
)

// Service coordinates recognition, tokenization and controlled reveal.
type Service struct {
	analyzer     *analyzer.AnalyzerEngine
	anonymize    *anonymizer.AnonymizerEngine
	vault        Vault
	store        Store
	policy       PolicyAuthorizer
	metrics      metrics.Recorder
	defaultScore float64
	defaultLang  string
	// tokenSeqMu + tokenSeqByTenant back the per-tenant monotonic
	// counter used by mintToken (see tokens.go). They look unused
	// from service.go because every read/write lives in tokens.go;
	// don't delete on a "dead field" audit.
	tokenSeqMu       sync.Mutex
	tokenSeqByTenant map[string]int
	versionInfo      VersionInfo
	// pdfRedactor backs Service.RedactPDF. nil = the binary endpoint
	// returns ErrPDFRedactorUnconfigured (HTTP 501). Wired by cmd/anonde
	// when ANONDE_PDF_ENABLED=1.
	pdfRedactor PDFRedactor
}

var ErrPolicyDenied = errors.New("policy denied")

// NewService wires the orchestration spine. The metrics Recorder is
// the last parameter so callers that don't care can pass
// metrics.NewNoop(); that's also what the no-instrumentation library
// path uses. Breaking constructor changes are still acceptable pre-1.0.
func NewService(
	analyzerEngine *analyzer.AnalyzerEngine,
	anonymizerEngine *anonymizer.AnonymizerEngine,
	vault Vault,
	store Store,
	policy PolicyAuthorizer,
	recorder metrics.Recorder,
) *Service {
	if recorder == nil {
		recorder = metrics.NewNoop()
	}
	return &Service{
		analyzer:         analyzerEngine,
		anonymize:        anonymizerEngine,
		vault:            vault,
		store:            store,
		policy:           policy,
		metrics:          recorder,
		defaultScore:     0.3,
		defaultLang:      "en",
		tokenSeqByTenant: map[string]int{},
	}
}

// SetVersionInfo records the build metadata GetVersion returns. Called
// by cmd/anonde after backend selection; the service has no other
// way to know which backend wraps its analyzer.
func (s *Service) SetVersionInfo(info VersionInfo) {
	s.versionInfo = info
}

// statusFromErr maps a Service-method error into the canonical
// metric label value. Three buckets only; anything else would blow
// up cardinality. The denied bucket is reserved for the policy
// surface; all other errors (validation, vault I/O, anonymizer
// failure, …) fold into "error".
func statusFromErr(err error) string {
	switch {
	case err == nil:
		return "ok"
	case errors.Is(err, ErrPolicyDenied):
		return "denied"
	default:
		return "error"
	}
}

// GetVersion returns the stamped VersionInfo. Always nil error; the
// signature matches the RPC shape for forward-compat with a future
// backend that genuinely needs to probe state.
func (s *Service) GetVersion(_ context.Context) (VersionInfo, error) {
	span := s.metrics.Request("get_version")
	defer span.Done("ok")
	return s.versionInfo, nil
}

// SaveRecord persists a fully-formed StoreRecord. Used by the binary-
// format endpoints (currently POST /v1/anonymizations/pdf) that need to
// store original + redacted bytes so reveal can return the original.
// The token-based text path uses Ingest which writes the record
// internally; SaveRecord is the escape hatch for surfaces that
// pre-compute the record themselves.
func (s *Service) SaveRecord(ctx context.Context, rec StoreRecord) error {
	if rec.TenantID == "" || rec.ID == "" {
		return fmt.Errorf("tenant_id and id are required")
	}
	return s.store.Put(ctx, rec)
}

// GetRecord returns the stored anonymization for (tenant, id); the
// raw StoreRecord including OriginalBytes / AnonymizedBytes for
// binary formats. Errors when the record is missing or expired.
func (s *Service) GetRecord(ctx context.Context, tenantID, id string) (StoreRecord, error) {
	if tenantID == "" || id == "" {
		return StoreRecord{}, fmt.Errorf("tenant_id and id are required")
	}
	return s.store.Get(ctx, tenantID, id)
}

// NewAnonymizationID exposes the internal Stripe-style id minter so
// surfaces like the PDF endpoint that bypass Ingest can still mint
// consistent ids.
func (s *Service) NewAnonymizationID() string {
	return newAnonymizationID()
}

// DeleteAnonymization removes the stored anonymization for (tenant, id)
// and every vault entry it references. Idempotent: a missing record
// returns Deleted=false, nil error. Token vault errors are surfaced so
// the caller can detect partial-cleanup states.
func (s *Service) DeleteAnonymization(ctx context.Context, tenantID, id string) (_ DeleteResult, err error) {
	span := s.metrics.Request("delete")
	defer func() { span.Done(statusFromErr(err)) }()
	if tenantID == "" || id == "" {
		return DeleteResult{}, fmt.Errorf("tenant_id and id are required")
	}

	record, err := s.store.Get(ctx, tenantID, id)
	if err != nil {
		// Missing record → nothing to do. The Vault may technically still
		// hold dangling entries from an interrupted earlier ingest, but
		// without a record we have no way to enumerate them; that's
		// acceptable today since the in-memory store dies with the
		// process. Persisted stores will need a reverse-index.
		return DeleteResult{}, nil
	}

	seen := make(map[string]struct{}, len(record.Tokens))
	deleted := 0
	for _, tokenRef := range record.Tokens {
		if _, dup := seen[tokenRef.Token]; dup {
			continue
		}
		seen[tokenRef.Token] = struct{}{}
		s.metrics.VaultOp("delete")
		if err := s.vault.Delete(ctx, tenantID, tokenRef.Token); err != nil {
			return DeleteResult{Deleted: false, TokensDeleted: deleted},
				fmt.Errorf("delete vault entry %q: %w", tokenRef.Token, err)
		}
		deleted++
	}

	existed, err := s.store.Delete(ctx, tenantID, id)
	if err != nil {
		return DeleteResult{Deleted: false, TokensDeleted: deleted},
			fmt.Errorf("delete store record: %w", err)
	}
	return DeleteResult{Deleted: existed, TokensDeleted: deleted}, nil
}

func (s *Service) Synthesize(ctx context.Context, req SynthesizeRequest) (_ *SynthesizeResponse, err error) {
	span := s.metrics.Request("synthesize")
	span.BytesIn(len(req.Content))
	defer func() { span.Done(statusFromErr(err)) }()
	if req.Content == "" {
		return nil, fmt.Errorf("content is required")
	}
	format := content.NormalizeFormat(req.ContentFormat)
	if format == "" {
		return nil, fmt.Errorf("unsupported content_format %q", req.ContentFormat)
	}
	if format == content.FormatAuto {
		format = content.ResolveAutoFormat(req.Content)
	}

	analyzableContent, err := content.ExtractAnalyzable(req.Content, format)
	if err != nil {
		return nil, err
	}

	resolvedLang := req.Language
	if resolvedLang == "" {
		resolvedLang = content.DetectLanguage(analyzableContent)
	}
	if resolvedLang == "" {
		resolvedLang = s.defaultLang
	}
	analysisCfg := analyzer.AnalysisConfig{
		Language:        resolvedLang,
		ScoreThreshold:  req.ScoreThreshold,
		RemoveConflicts: true,
		Entities:        req.Entities,
		DisableNER:      req.DisableNER,
	}
	if req.ScoreThreshold == 0 {
		analysisCfg.ScoreThreshold = s.defaultScore
	}

	syn := &operators.Synthesize{
		Consistent:     req.Consistent,
		DocumentScoped: req.DocScoped,
	}
	anonCfg := anonymizer.AnonymizerConfig{"*": syn}
	allFindings := make([]analyzer.RecognizerResult, 0, 8)

	synText := func(input string) (string, error) {
		input = content.SanitizeUTF8(content.StripANSI(input))
		if strings.TrimSpace(input) == "" {
			return input, nil
		}
		anlStart := time.Now()
		findings, err := s.analyzer.Analyze(ctx, input, analysisCfg)
		span.AnalyzeDuration(s.versionInfo.AnalyzerBackend, time.Since(anlStart).Seconds())
		if err != nil {
			return "", fmt.Errorf("analyze: %w", err)
		}
		allFindings = append(allFindings, findings...)
		if len(findings) == 0 {
			return input, nil
		}
		out, err := s.anonymize.Anonymize(input, findings, anonCfg)
		if err != nil {
			return "", fmt.Errorf("synthesize: %w", err)
		}
		return out.Text, nil
	}
	jsonLeafFn := func(value string) (string, error) { return synText(value) }
	jsonDocFn := func(value string) (string, error) {
		return content.TransformJSONStringLeaves(value, jsonLeafFn)
	}

	var synthesized string
	switch format {
	case content.FormatText, content.FormatPDF:
		synthesized, err = synText(analyzableContent)
	case content.FormatJSON:
		synthesized, err = content.TransformJSONStringLeaves(analyzableContent, jsonLeafFn)
	case content.FormatNDJSON:
		synthesized, err = content.TransformLines(analyzableContent, true, jsonDocFn, synText)
	case content.FormatLogs:
		synthesized, err = content.TransformLines(analyzableContent, false, jsonDocFn, synText)
	default:
		return nil, fmt.Errorf("unsupported content_format %q", req.ContentFormat)
	}
	if err != nil {
		return nil, err
	}

	span.BytesOut(len(synthesized))
	return &SynthesizeResponse{
		Content:  synthesized,
		Findings: allFindings,
	}, nil
}

func (s *Service) Ingest(ctx context.Context, req IngestRequest) (_ *IngestResponse, err error) {
	span := s.metrics.Request("ingest")
	span.BytesIn(len(req.Content))
	defer func() { span.Done(statusFromErr(err)) }()
	if req.TenantID == "" || req.Content == "" {
		return nil, fmt.Errorf("tenant_id and content are required")
	}
	// Caller-supplied ID is the round-trip key (replayable from logs);
	// empty means the caller doesn't care, so mint a Stripe-style
	// `anon_<hex>` ID and return it. Either way the response always
	// echoes the final id so the client can reveal/delete later.
	id := strings.TrimSpace(req.ID)
	if id == "" {
		id = newAnonymizationID()
	}
	format := content.NormalizeFormat(req.ContentFormat)
	if format == "" {
		return nil, fmt.Errorf("unsupported content_format %q", req.ContentFormat)
	}
	if format == content.FormatAuto {
		format = content.ResolveAutoFormat(req.Content)
	}

	analyzableContent, err := content.ExtractAnalyzable(req.Content, format)
	if err != nil {
		return nil, err
	}

	// Per-ingest accumulators are shared across all calls to anonymizeText
	// (for line-based formats this means per-line findings/tokens accumulate
	// into a single response).
	tokens := make([]TokenRef, 0, 16)
	findings := make([]analyzer.RecognizerResult, 0, 16)
	// Per-doc cleartext->token mapping; serves both as cache (so the same
	// cleartext within one doc gets one token) and as the reveal source.
	docTokenByKey := map[string]string{} // key = entityType+"\x00"+cleartext

	// Language resolution: explicit request value wins. Otherwise auto-
	// detect from the document's analyzable text. Fall back to the
	// configured default only when detection returns "" (very short or
	// stopword-free input).
	resolvedLang := req.Language
	if resolvedLang == "" {
		resolvedLang = content.DetectLanguage(analyzableContent)
	}
	if resolvedLang == "" {
		resolvedLang = s.defaultLang
	}
	analysisCfg := analyzer.AnalysisConfig{
		Language:        resolvedLang,
		ScoreThreshold:  req.ScoreThreshold,
		RemoveConflicts: true,
		Entities:        req.Entities,
		DisableNER:      req.DisableNER,
	}
	if req.ScoreThreshold == 0 {
		analysisCfg.ScoreThreshold = s.defaultScore
	}

	anonymizeText := func(input string) (string, []analyzer.RecognizerResult, error) {
		// All raw input is sanitized: invalid UTF-8 broken before the
		// recognizers and ANSI escapes stripped. For text/json/pdf paths the
		// caller hasn't done this; for ndjson/logs the line splitter has,
		// idempotent calls are cheap.
		input = content.SanitizeUTF8(content.StripANSI(input))
		if strings.TrimSpace(input) == "" {
			return input, nil, nil
		}
		anlStart := time.Now()
		localFindings, err := s.analyzer.Analyze(ctx, input, analysisCfg)
		span.AnalyzeDuration(s.versionInfo.AnalyzerBackend, time.Since(anlStart).Seconds())
		if err != nil {
			return "", nil, fmt.Errorf("analyze content: %w", err)
		}
		if len(localFindings) == 0 {
			return input, localFindings, nil
		}
		// Pre-merge same-type adjacent spans (e.g. GLiNER returns
		// "Elena" and "Rossi" as separate PERSON findings; the
		// anonymizer would merge them anyway, but doing it here keeps
		// the cleartext we register in byCleartext below in lock-step
		// with what the anonymizer ends up requesting from the
		// tokenize operator. Without this, post-merge lookups miss
		// and produce "no token mapped for X" errors.
		localFindings = anonymizer.MergeAdjacentSameType(localFindings, input)

		cfg := anonymizer.AnonymizerConfig{}
		entityOperators := map[string]*tokenOperator{}
		for _, finding := range localFindings {
			if finding.Start < 0 || finding.End < 0 || finding.Start > finding.End || finding.End > len(input) {
				return "", nil, fmt.Errorf(
					"invalid finding span start=%d end=%d text_bytes=%d entity=%q",
					finding.Start, finding.End, len(input), finding.EntityType,
				)
			}
			cleartext := input[finding.Start:finding.End]
			entityOp := entityOperators[finding.EntityType]
			if entityOp == nil {
				entityOp = &tokenOperator{byCleartext: map[string]string{}}
				entityOperators[finding.EntityType] = entityOp
				cfg[finding.EntityType] = entityOp
			}

			cacheKey := finding.EntityType + "\x00" + cleartext
			token, hit := docTokenByKey[cacheKey]
			if !hit {
				token = s.mintToken(req.TenantID, finding.EntityType)
				docTokenByKey[cacheKey] = token
				s.metrics.VaultOp("put")
				if err := s.vault.Put(ctx, req.TenantID, VaultEntry{
					Token:      token,
					EntityType: finding.EntityType,
					Cleartext:  cleartext,
				}); err != nil {
					return "", nil, fmt.Errorf("store vault mapping: %w", err)
				}
			}
			entityOp.byCleartext[cleartext] = token
			tokens = append(tokens, TokenRef{
				Token:      token,
				EntityType: finding.EntityType,
				Start:      finding.Start,
				End:        finding.End,
			})
		}
		result, err := s.anonymize.Anonymize(input, localFindings, cfg)
		if err != nil {
			return "", nil, fmt.Errorf("anonymize content: %w", err)
		}
		return result.Text, localFindings, nil
	}

	// jsonLeafFn handles a single JSON document (or an NDJSON line) by
	// recursing through string leaves. textFn handles a plain text segment.
	jsonLeafFn := func(value string) (string, error) {
		out, localFindings, err := anonymizeText(value)
		if err != nil {
			return "", err
		}
		findings = append(findings, localFindings...)
		return out, nil
	}
	jsonDocFn := func(value string) (string, error) {
		return content.TransformJSONStringLeaves(value, jsonLeafFn)
	}
	textFn := func(value string) (string, error) {
		out, localFindings, err := anonymizeText(value)
		if err != nil {
			return "", err
		}
		findings = append(findings, localFindings...)
		return out, nil
	}

	anonymizedContent := analyzableContent
	switch format {
	case content.FormatText, content.FormatPDF:
		out, localFindings, err := anonymizeText(analyzableContent)
		if err != nil {
			return nil, err
		}
		anonymizedContent = out
		findings = append(findings, localFindings...)
	case content.FormatJSON:
		out, err := content.TransformJSONStringLeaves(analyzableContent, jsonLeafFn)
		if err != nil {
			return nil, err
		}
		anonymizedContent = out
	case content.FormatNDJSON:
		out, err := content.TransformLines(analyzableContent, true, jsonDocFn, textFn)
		if err != nil {
			return nil, err
		}
		anonymizedContent = out
	case content.FormatLogs:
		out, err := content.TransformLines(analyzableContent, false, jsonDocFn, textFn)
		if err != nil {
			return nil, err
		}
		anonymizedContent = out
	default:
		return nil, fmt.Errorf("unsupported content_format %q", req.ContentFormat)
	}

	record := StoreRecord{
		TenantID:          req.TenantID,
		ID:                id,
		ContentFormat:     format,
		AnonymizedContent: anonymizedContent,
		Tokens:            tokens,
	}
	if err := s.store.Put(ctx, record); err != nil {
		return nil, fmt.Errorf("store anonymization: %w", err)
	}

	span.BytesOut(len(anonymizedContent))
	return &IngestResponse{
		TenantID:           req.TenantID,
		ID:                 id,
		AnonymizedContent:  anonymizedContent,
		DetectedEntitySize: len(findings),
		Findings:           findings,
		Tokens:             tokens,
	}, nil
}

func (s *Service) Detokenize(ctx context.Context, req DetokenizeRequest) (_ *DetokenizeResponse, err error) {
	span := s.metrics.Request("detokenize")
	defer func() { span.Done(statusFromErr(err)) }()
	if req.TenantID == "" || req.ID == "" || req.Actor == "" || req.Purpose == "" {
		return nil, fmt.Errorf("tenant_id, id, actor and purpose are required")
	}
	if len(req.Tokens) == 0 {
		return nil, fmt.Errorf("at least one token is required")
	}

	if err := s.policy.AllowDetokenize(ctx, req); err != nil {
		s.metrics.PolicyDenied("authorizer_denied")
		return nil, fmt.Errorf("%w: %v", ErrPolicyDenied, err)
	}

	record, err := s.store.Get(ctx, req.TenantID, req.ID)
	if err != nil {
		return nil, fmt.Errorf("load anonymization: %w", err)
	}

	allowed := make(map[string]struct{}, len(record.Tokens))
	for _, tokenRef := range record.Tokens {
		allowed[tokenRef.Token] = struct{}{}
	}

	resolved := make(map[string]string, len(req.Tokens))
	for _, token := range req.Tokens {
		if _, ok := allowed[token]; !ok {
			return nil, fmt.Errorf("token %q not linked to anonymization %q", token, req.ID)
		}
		s.metrics.VaultOp("get")
		entry, err := s.vault.Get(ctx, req.TenantID, token)
		if err != nil {
			return nil, fmt.Errorf("lookup token %q: %w", token, err)
		}
		resolved[token] = entry.Cleartext
	}

	return &DetokenizeResponse{
		TenantID: req.TenantID,
		ID:       req.ID,
		Resolved: resolved,
	}, nil
}

func (s *Service) Reveal(ctx context.Context, req RevealRequest) (_ *RevealResponse, err error) {
	span := s.metrics.Request("reveal")
	span.BytesIn(len(req.Content))
	defer func() { span.Done(statusFromErr(err)) }()
	if req.TenantID == "" || req.ID == "" || req.Actor == "" || req.Purpose == "" || req.Content == "" {
		return nil, fmt.Errorf("tenant_id, id, actor, purpose and content are required")
	}

	record, err := s.store.Get(ctx, req.TenantID, req.ID)
	if err != nil {
		return nil, fmt.Errorf("load anonymization: %w", err)
	}

	tokenSet := make(map[string]struct{}, len(record.Tokens))
	orderedTokens := make([]string, 0, len(record.Tokens))
	for _, tokenRef := range record.Tokens {
		if _, seen := tokenSet[tokenRef.Token]; seen {
			continue
		}
		tokenSet[tokenRef.Token] = struct{}{}
		orderedTokens = append(orderedTokens, tokenRef.Token)
	}

	if len(orderedTokens) == 0 {
		span.BytesOut(len(req.Content))
		return &RevealResponse{
			TenantID:            req.TenantID,
			ID:                  req.ID,
			DeanonymizedContent: req.Content,
			Resolved:            map[string]string{},
		}, nil
	}

	detok, err := s.Detokenize(ctx, DetokenizeRequest{
		TenantID: req.TenantID,
		ID:       req.ID,
		Actor:    req.Actor,
		Purpose:  req.Purpose,
		Tokens:   orderedTokens,
	})
	if err != nil {
		return nil, err
	}

	requestedFormat := content.NormalizeFormat(req.ContentFormat)
	if requestedFormat == "" {
		requestedFormat = record.ContentFormat
	}
	if requestedFormat == content.FormatAuto {
		requestedFormat = content.ResolveAutoFormat(req.Content)
	}
	if requestedFormat == "" {
		requestedFormat = content.FormatText
	}

	replacer, err := buildTokenReplacer(orderedTokens, detok.Resolved)
	if err != nil {
		return nil, err
	}

	out := req.Content
	switch requestedFormat {
	case content.FormatText, content.FormatPDF:
		out = replacer(req.Content)
	case content.FormatJSON:
		jsonOutput, err := content.TransformJSONStringLeaves(req.Content, func(v string) (string, error) {
			return replacer(v), nil
		})
		if err != nil {
			return nil, err
		}
		out = jsonOutput
	case content.FormatNDJSON, content.FormatLogs:
		forceJSON := requestedFormat == content.FormatNDJSON
		jsonFn := func(v string) (string, error) {
			return content.TransformJSONStringLeaves(v, func(s string) (string, error) {
				return replacer(s), nil
			})
		}
		textFn := func(v string) (string, error) { return replacer(v), nil }
		lineOut, err := content.TransformLines(req.Content, forceJSON, jsonFn, textFn)
		if err != nil {
			return nil, err
		}
		out = lineOut
	default:
		return nil, fmt.Errorf("unsupported content_format %q", req.ContentFormat)
	}

	span.BytesOut(len(out))
	return &RevealResponse{
		TenantID:            req.TenantID,
		ID:                  req.ID,
		DeanonymizedContent: out,
		Resolved:            detok.Resolved,
	}, nil
}
