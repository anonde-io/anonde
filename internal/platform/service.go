package platform

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/moogacs/anonde/analyzer"
	"github.com/moogacs/anonde/anonymizer"
	"github.com/moogacs/anonde/anonymizer/operators"
)

// PolicyAuthorizer gates deanonymization access.
type PolicyAuthorizer interface {
	AllowDetokenize(ctx context.Context, req DetokenizeRequest) error
}

// Vault stores token -> cleartext mappings locally.
type Vault interface {
	Put(ctx context.Context, tenantID string, entry VaultEntry) error
	Get(ctx context.Context, tenantID, token string) (VaultEntry, error)
}

// Store persists anonymized documents only.
type Store interface {
	Put(ctx context.Context, record StoreRecord) error
	Get(ctx context.Context, tenantID, docID string) (StoreRecord, error)
}

// VaultEntry stores one token mapping.
type VaultEntry struct {
	Token      string
	EntityType string
	Cleartext  string
}

// Service coordinates recognition, tokenization and controlled reveal.
type Service struct {
	analyzer       *analyzer.AnalyzerEngine
	anonymize      *anonymizer.AnonymizerEngine
	vault          Vault
	store          Store
	policy         PolicyAuthorizer
	defaultScore   float64
	defaultLang    string
	docSeqMu       sync.Mutex
	docSeqByTenant map[string]int
}

var ErrPolicyDenied = errors.New("policy denied")

func NewService(
	analyzerEngine *analyzer.AnalyzerEngine,
	anonymizerEngine *anonymizer.AnonymizerEngine,
	vault Vault,
	store Store,
	policy PolicyAuthorizer,
) *Service {
	return &Service{
		analyzer:       analyzerEngine,
		anonymize:      anonymizerEngine,
		vault:          vault,
		store:          store,
		policy:         policy,
		defaultScore:   0.3,
		defaultLang:    "en",
		docSeqByTenant: map[string]int{},
	}
}

func (s *Service) Synthesize(ctx context.Context, req SynthesizeRequest) (*SynthesizeResponse, error) {
	if req.Content == "" {
		return nil, fmt.Errorf("content is required")
	}
	format := normalizeContentFormat(req.ContentFormat)
	if format == "" {
		return nil, fmt.Errorf("unsupported content_format %q", req.ContentFormat)
	}
	if format == contentFormatAuto {
		format = resolveAutoContentFormat(req.Content)
	}

	analyzableContent, err := extractAnalyzableText(req.Content, format)
	if err != nil {
		return nil, err
	}

	resolvedLang := req.Language
	if resolvedLang == "" {
		resolvedLang = detectLanguage(analyzableContent)
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
		input = sanitizeUTF8(stripANSI(input))
		if strings.TrimSpace(input) == "" {
			return input, nil
		}
		findings, err := s.analyzer.Analyze(ctx, input, analysisCfg)
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
		return transformJSONStringLeaves(value, jsonLeafFn)
	}

	var synthesized string
	switch format {
	case contentFormatText, contentFormatPDF:
		synthesized, err = synText(analyzableContent)
	case contentFormatJSON:
		synthesized, err = transformJSONStringLeaves(analyzableContent, jsonLeafFn)
	case contentFormatNDJSON:
		synthesized, err = transformLines(analyzableContent, true, jsonDocFn, synText)
	case contentFormatLogs:
		synthesized, err = transformLines(analyzableContent, false, jsonDocFn, synText)
	default:
		return nil, fmt.Errorf("unsupported content_format %q", req.ContentFormat)
	}
	if err != nil {
		return nil, err
	}

	return &SynthesizeResponse{
		Content:  synthesized,
		Findings: allFindings,
	}, nil
}

func (s *Service) Ingest(ctx context.Context, req IngestRequest) (*IngestResponse, error) {
	if req.TenantID == "" || req.DocID == "" || req.Content == "" {
		return nil, fmt.Errorf("tenant_id, doc_id and content are required")
	}
	format := normalizeContentFormat(req.ContentFormat)
	if format == "" {
		return nil, fmt.Errorf("unsupported content_format %q", req.ContentFormat)
	}
	if format == contentFormatAuto {
		format = resolveAutoContentFormat(req.Content)
	}

	analyzableContent, err := extractAnalyzableText(req.Content, format)
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
		resolvedLang = detectLanguage(analyzableContent)
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
		// caller hasn't done this; for ndjson/logs the line splitter has —
		// idempotent calls are cheap.
		input = sanitizeUTF8(stripANSI(input))
		if strings.TrimSpace(input) == "" {
			return input, nil, nil
		}
		localFindings, err := s.analyzer.Analyze(ctx, input, analysisCfg)
		if err != nil {
			return "", nil, fmt.Errorf("analyze content: %w", err)
		}
		if len(localFindings) == 0 {
			return input, localFindings, nil
		}

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
		return transformJSONStringLeaves(value, jsonLeafFn)
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
	case contentFormatText, contentFormatPDF:
		out, localFindings, err := anonymizeText(analyzableContent)
		if err != nil {
			return nil, err
		}
		anonymizedContent = out
		findings = append(findings, localFindings...)
	case contentFormatJSON:
		out, err := transformJSONStringLeaves(analyzableContent, jsonLeafFn)
		if err != nil {
			return nil, err
		}
		anonymizedContent = out
	case contentFormatNDJSON:
		out, err := transformLines(analyzableContent, true, jsonDocFn, textFn)
		if err != nil {
			return nil, err
		}
		anonymizedContent = out
	case contentFormatLogs:
		out, err := transformLines(analyzableContent, false, jsonDocFn, textFn)
		if err != nil {
			return nil, err
		}
		anonymizedContent = out
	default:
		return nil, fmt.Errorf("unsupported content_format %q", req.ContentFormat)
	}

	record := StoreRecord{
		TenantID:          req.TenantID,
		DocID:             req.DocID,
		ContentFormat:     format,
		AnonymizedContent: anonymizedContent,
		Tokens:            tokens,
	}
	if err := s.store.Put(ctx, record); err != nil {
		return nil, fmt.Errorf("store anonymized document: %w", err)
	}

	return &IngestResponse{
		TenantID:           req.TenantID,
		DocID:              req.DocID,
		AnonymizedContent:  anonymizedContent,
		DetectedEntitySize: len(findings),
		Findings:           findings,
		Tokens:             tokens,
	}, nil
}

// mintToken returns a fresh token for (tenant, entity), using a per-tenant
// counter held by the service. Tokens are always referenced via the doc's
// StoreRecord on reveal, so persistence of the counter isn't required.
//
// Cross-document token reuse for the same cleartext is intentionally NOT
// supported here — see TODO.md ("Tenant-scoped token reuse").
func (s *Service) mintToken(tenantID, entityType string) string {
	s.docSeqMu.Lock()
	idx := s.docSeqByTenant[tenantID]
	s.docSeqByTenant[tenantID] = idx + 1
	s.docSeqMu.Unlock()
	return buildToken(tenantID, entityType, idx)
}

func (s *Service) Detokenize(ctx context.Context, req DetokenizeRequest) (*DetokenizeResponse, error) {
	if req.TenantID == "" || req.DocID == "" || req.Actor == "" || req.Purpose == "" {
		return nil, fmt.Errorf("tenant_id, doc_id, actor and purpose are required")
	}
	if len(req.Tokens) == 0 {
		return nil, fmt.Errorf("at least one token is required")
	}

	if err := s.policy.AllowDetokenize(ctx, req); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrPolicyDenied, err)
	}

	record, err := s.store.Get(ctx, req.TenantID, req.DocID)
	if err != nil {
		return nil, fmt.Errorf("load anonymized document: %w", err)
	}

	allowed := make(map[string]struct{}, len(record.Tokens))
	for _, tokenRef := range record.Tokens {
		allowed[tokenRef.Token] = struct{}{}
	}

	resolved := make(map[string]string, len(req.Tokens))
	for _, token := range req.Tokens {
		if _, ok := allowed[token]; !ok {
			return nil, fmt.Errorf("token %q not linked to doc %q", token, req.DocID)
		}
		entry, err := s.vault.Get(ctx, req.TenantID, token)
		if err != nil {
			return nil, fmt.Errorf("lookup token %q: %w", token, err)
		}
		resolved[token] = entry.Cleartext
	}

	return &DetokenizeResponse{
		TenantID: req.TenantID,
		DocID:    req.DocID,
		Resolved: resolved,
	}, nil
}

func (s *Service) Reveal(ctx context.Context, req RevealRequest) (*RevealResponse, error) {
	if req.TenantID == "" || req.DocID == "" || req.Actor == "" || req.Purpose == "" || req.Content == "" {
		return nil, fmt.Errorf("tenant_id, doc_id, actor, purpose and content are required")
	}

	record, err := s.store.Get(ctx, req.TenantID, req.DocID)
	if err != nil {
		return nil, fmt.Errorf("load anonymized document: %w", err)
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
		return &RevealResponse{
			TenantID:            req.TenantID,
			DocID:               req.DocID,
			DeanonymizedContent: req.Content,
			Resolved:            map[string]string{},
		}, nil
	}

	detok, err := s.Detokenize(ctx, DetokenizeRequest{
		TenantID: req.TenantID,
		DocID:    req.DocID,
		Actor:    req.Actor,
		Purpose:  req.Purpose,
		Tokens:   orderedTokens,
	})
	if err != nil {
		return nil, err
	}

	requestedFormat := normalizeContentFormat(req.ContentFormat)
	if requestedFormat == "" {
		requestedFormat = record.ContentFormat
	}
	if requestedFormat == contentFormatAuto {
		requestedFormat = resolveAutoContentFormat(req.Content)
	}
	if requestedFormat == "" {
		requestedFormat = contentFormatText
	}

	replacer, err := buildTokenReplacer(orderedTokens, detok.Resolved)
	if err != nil {
		return nil, err
	}

	out := req.Content
	switch requestedFormat {
	case contentFormatText, contentFormatPDF:
		out = replacer(req.Content)
	case contentFormatJSON:
		jsonOutput, err := transformJSONStringLeaves(req.Content, func(v string) (string, error) {
			return replacer(v), nil
		})
		if err != nil {
			return nil, err
		}
		out = jsonOutput
	case contentFormatNDJSON, contentFormatLogs:
		forceJSON := requestedFormat == contentFormatNDJSON
		jsonFn := func(v string) (string, error) {
			return transformJSONStringLeaves(v, func(s string) (string, error) {
				return replacer(s), nil
			})
		}
		textFn := func(v string) (string, error) { return replacer(v), nil }
		lineOut, err := transformLines(req.Content, forceJSON, jsonFn, textFn)
		if err != nil {
			return nil, err
		}
		out = lineOut
	default:
		return nil, fmt.Errorf("unsupported content_format %q", req.ContentFormat)
	}

	return &RevealResponse{
		TenantID:            req.TenantID,
		DocID:               req.DocID,
		DeanonymizedContent: out,
		Resolved:            detok.Resolved,
	}, nil
}

// buildTokenReplacer returns a function that replaces every known token in
// the input with its cleartext in a single pass. Tokens are sorted
// longest-first so a longer token is never shadowed by a shorter prefix.
func buildTokenReplacer(orderedTokens []string, resolved map[string]string) (func(string) string, error) {
	if len(orderedTokens) == 0 {
		return func(s string) string { return s }, nil
	}
	sorted := make([]string, len(orderedTokens))
	copy(sorted, orderedTokens)
	sort.Slice(sorted, func(i, j int) bool { return len(sorted[i]) > len(sorted[j]) })
	parts := make([]string, 0, len(sorted))
	for _, t := range sorted {
		parts = append(parts, regexp.QuoteMeta(t))
	}
	re, err := regexp.Compile(strings.Join(parts, "|"))
	if err != nil {
		return nil, fmt.Errorf("compile token replacer: %w", err)
	}
	return func(s string) string {
		return re.ReplaceAllStringFunc(s, func(match string) string {
			if v, ok := resolved[match]; ok {
				return v
			}
			return match
		})
	}, nil
}

func buildToken(tenantID, entityType string, idx int) string {
	normalizedTenant := strings.ToUpper(strings.ReplaceAll(tenantID, "-", "_"))
	return fmt.Sprintf("<%s_%s_%06d>", entityType, normalizedTenant, idx+1)
}

type tokenOperator struct {
	byCleartext map[string]string
}

func (o *tokenOperator) Name() string {
	return "tokenize"
}

func (o *tokenOperator) Anonymize(text string, _ string) (string, error) {
	token, ok := o.byCleartext[text]
	if !ok {
		return "", fmt.Errorf("no token mapped for %q", text)
	}
	return token, nil
}
