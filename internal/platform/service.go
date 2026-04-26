package platform

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/moogacs/anonde/analyzer"
	"github.com/moogacs/anonde/anonymizer"
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
	analyzer  *analyzer.AnalyzerEngine
	anonymize *anonymizer.AnonymizerEngine
	vault     Vault
	store     Store
	policy    PolicyAuthorizer
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
		analyzer:  analyzerEngine,
		anonymize: anonymizerEngine,
		vault:     vault,
		store:     store,
		policy:    policy,
	}
}

func (s *Service) Ingest(ctx context.Context, req IngestRequest) (*IngestResponse, error) {
	if req.TenantID == "" || req.DocID == "" || req.Content == "" {
		return nil, fmt.Errorf("tenant_id, doc_id and content are required")
	}

	findings, err := s.analyzer.Analyze(ctx, req.Content, analyzer.AnalysisConfig{
		Language:        "en",
		ScoreThreshold:  0.3,
		RemoveConflicts: true,
	})
	if err != nil {
		return nil, fmt.Errorf("analyze content: %w", err)
	}

	cfg := anonymizer.AnonymizerConfig{}
	entityOperators := map[string]*tokenOperator{}
	tokens := make([]TokenRef, 0, len(findings))
	for _, finding := range findings {
		if finding.Start < 0 || finding.End < 0 || finding.Start > finding.End || finding.End > len(req.Content) {
			return nil, fmt.Errorf(
				"invalid finding span start=%d end=%d text_bytes=%d entity=%q",
				finding.Start, finding.End, len(req.Content), finding.EntityType,
			)
		}
		original := req.Content[finding.Start:finding.End]
		entityOp := entityOperators[finding.EntityType]
		if entityOp == nil {
			entityOp = &tokenOperator{byCleartext: map[string]string{}}
			entityOperators[finding.EntityType] = entityOp
			cfg[finding.EntityType] = entityOp
		}

		cleartext := original
		token, ok := entityOp.byCleartext[cleartext]
		if !ok {
			token = buildToken(req.TenantID, finding.EntityType, len(tokens))
			entityOp.byCleartext[cleartext] = token
		}
		tokens = append(tokens, TokenRef{
			Token:      token,
			EntityType: finding.EntityType,
			Start:      finding.Start,
			End:        finding.End,
		})

		if err := s.vault.Put(ctx, req.TenantID, VaultEntry{
			Token:      token,
			EntityType: finding.EntityType,
			Cleartext:  cleartext,
		}); err != nil {
			return nil, fmt.Errorf("store vault mapping: %w", err)
		}
	}

	result, err := s.anonymize.Anonymize(req.Content, findings, cfg)
	if err != nil {
		return nil, fmt.Errorf("anonymize content: %w", err)
	}

	record := StoreRecord{
		TenantID:          req.TenantID,
		DocID:             req.DocID,
		AnonymizedContent: result.Text,
		Tokens:            tokens,
	}
	if err := s.store.Put(ctx, record); err != nil {
		return nil, fmt.Errorf("store anonymized document: %w", err)
	}

	return &IngestResponse{
		TenantID:           req.TenantID,
		DocID:              req.DocID,
		AnonymizedContent:  result.Text,
		DetectedEntitySize: len(findings),
		Findings:           findings,
		Tokens:             tokens,
	}, nil
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

	// Build ordered token list from stored document metadata so policy and linkage checks
	// remain equivalent to explicit /v1/detokenize calls.
	tokenSet := make(map[string]struct{}, len(record.Tokens))
	orderedTokens := make([]string, 0, len(record.Tokens))
	for _, tokenRef := range record.Tokens {
		if _, seen := tokenSet[tokenRef.Token]; seen {
			continue
		}
		tokenSet[tokenRef.Token] = struct{}{}
		orderedTokens = append(orderedTokens, tokenRef.Token)
	}

	// Nothing to resolve: return input as-is for better UX.
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

	out := req.Content
	for _, token := range orderedTokens {
		value, ok := detok.Resolved[token]
		if !ok {
			continue
		}
		out = strings.ReplaceAll(out, token, value)
	}

	return &RevealResponse{
		TenantID:            req.TenantID,
		DocID:               req.DocID,
		DeanonymizedContent: out,
		Resolved:            detok.Resolved,
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
