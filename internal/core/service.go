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

// maxIdentifierBytes bounds a caller-supplied tenant_id / id. Keeping it
// well under the bbolt composite key's uint16 tenant-length prefix (65536)
// makes that on-disk key collision-free by invariant. 4096 is far above
// any real identifier.
const maxIdentifierBytes = 4096

// validateIdentifier rejects a caller-supplied tenant_id / id that is
// over-long or NUL-bearing — the two inputs that could forge a colliding
// composite key. Empty passes on purpose: the required check is each
// method's job, and an empty id is legal on Ingest, which mints one.
func validateIdentifier(name, value string) error {
	if len(value) > maxIdentifierBytes {
		return fmt.Errorf("%s exceeds %d bytes", name, maxIdentifierBytes)
	}
	if strings.IndexByte(value, 0x00) >= 0 {
		return fmt.Errorf("%s must not contain a NUL byte", name)
	}
	return nil
}

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
	if err := validateIdentifier("tenant_id", rec.TenantID); err != nil {
		return err
	}
	if err := validateIdentifier("id", rec.ID); err != nil {
		return err
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
	if err := validateIdentifier("tenant_id", tenantID); err != nil {
		return StoreRecord{}, err
	}
	if err := validateIdentifier("id", id); err != nil {
		return StoreRecord{}, err
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
	if err := validateIdentifier("tenant_id", tenantID); err != nil {
		return DeleteResult{}, err
	}
	if err := validateIdentifier("id", id); err != nil {
		return DeleteResult{}, err
	}

	record, err := s.store.Get(ctx, tenantID, id)
	if err != nil {
		if errors.Is(err, ErrRecordNotFound) {
			// Missing record → nothing to do. Dangling vault entries from an
			// interrupted ingest aren't enumerable without a reverse-index.
			return DeleteResult{}, nil
		}
		// A real backend failure is not an idempotent no-op — surface it.
		return DeleteResult{}, fmt.Errorf("load anonymization for delete: %w", err)
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

// maxMintAttempts bounds the collision-skip loop so a wedged vault can't
// spin forever; exceeding it is a hard error, never a silent overwrite.
const maxMintAttempts = 1 << 20

// mintAndStoreToken mints a fresh token and stores its cleartext mapping,
// guaranteeing the token doesn't already map a DIFFERENT cleartext. The
// in-memory counter (tokens.go) resets on restart, so a persistent vault
// could re-mint a live token and overwrite it; the probe skips occupied
// tokens and the Vault.Put ErrTokenCollision guard is the atomic backstop
// for the concurrent-mint race.
func (s *Service) mintAndStoreToken(ctx context.Context, tenantID, entityType, cleartext string) (string, error) {
	for attempts := 0; attempts < maxMintAttempts; attempts++ {
		token := s.mintToken(tenantID, entityType)
		// Occupied (a prior lifecycle minted it): try the next counter value.
		if _, err := s.vault.Get(ctx, tenantID, token); err == nil {
			continue
		}
		s.metrics.VaultOp("put")
		err := s.vault.Put(ctx, tenantID, VaultEntry{
			Token:      token,
			EntityType: entityType,
			Cleartext:  cleartext,
		})
		if err == nil {
			return token, nil
		}
		if errors.Is(err, ErrTokenCollision) {
			// Lost the race to a concurrent mint; retry with a new token.
			continue
		}
		return "", fmt.Errorf("store vault mapping: %w", err)
	}
	return "", fmt.Errorf("mint token for tenant %q: no free token after %d attempts", tenantID, maxMintAttempts)
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

	analyzableContent, err := content.ExtractAnalyzable(ctx, req.Content, format)
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
	anonCfg := anonymizer.AnonymizerConfig{Operators: anonymizer.OperatorMap{"*": syn}}
	allFindings := make([]analyzer.RecognizerResult, 0, 8)

	// base is the byte offset of input within the submitted document; it
	// shifts the analyzer's input-local finding offsets to document-relative
	// ones before they land in the response. For the plain-text path base is
	// 0; for JSON leaves / NDJSON+log lines it locates the leaf/line in the
	// submitted document (see content.TransformJSONStringLeavesAt /
	// TransformLinesAt for the exactness caveats).
	synText := func(input string, base int) (string, error) {
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
		for _, finding := range findings {
			finding.Start += base
			finding.End += base
			allFindings = append(allFindings, finding)
		}
		if len(findings) == 0 {
			return input, nil
		}
		// Anonymize needs the input-local spans, so pass the unshifted
		// findings here — only the response copy above is document-relative.
		out, err := s.anonymize.Anonymize(input, findings, anonCfg)
		if err != nil {
			return "", fmt.Errorf("synthesize: %w", err)
		}
		return out.Text, nil
	}
	segmentFn := func(value string, base int) (string, error) { return synText(value, base) }
	jsonDocFn := func(line string, base int) (string, error) {
		return content.TransformJSONStringLeavesAt(line, func(value string, leafBase int) (string, error) {
			return synText(value, base+leafBase)
		})
	}

	var synthesized string
	switch format {
	case content.FormatText, content.FormatPDF:
		synthesized, err = synText(analyzableContent, 0)
	case content.FormatJSON:
		synthesized, err = content.TransformJSONStringLeavesAt(analyzableContent, segmentFn)
	case content.FormatNDJSON:
		synthesized, err = content.TransformLinesAt(analyzableContent, true, jsonDocFn, segmentFn)
	case content.FormatLogs:
		synthesized, err = content.TransformLinesAt(analyzableContent, false, jsonDocFn, segmentFn)
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
	if err := validateIdentifier("tenant_id", req.TenantID); err != nil {
		return nil, err
	}
	if err := validateIdentifier("id", req.ID); err != nil {
		return nil, err
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

	analyzableContent, err := content.ExtractAnalyzable(ctx, req.Content, format)
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

	// mintedTokens records every vault entry created during THIS ingest so a
	// failure after minting (analyze / anonymize / store error) can roll them
	// back. Token mappings are written to the vault before the record that
	// references them, so without rollback a failed ingest strands cleartext
	// PII in the vault with no record — never enumerable, never cleaned up.
	// committed flips true only once the record is durably stored.
	mintedTokens := make([]string, 0, 16)
	committed := false
	defer func() {
		if committed {
			return
		}
		// Best-effort rollback on a detached context so a cancelled request
		// still cleans up. A stranded cleartext mapping is worse than a
		// redundant delete of a token nothing else references (minting skips
		// occupied tokens, so these are unique to this ingest).
		for _, tok := range mintedTokens {
			_ = s.vault.Delete(context.Background(), req.TenantID, tok)
		}
	}()

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

	// anonymizeText anonymizes one input segment. base is the byte offset of
	// that segment within the submitted document, used to translate the
	// analyzer's segment-local finding offsets into the document-relative
	// offsets the response (Findings + TokenRef.Start/End) is contracted to
	// carry. For the plain-text path base is 0 (segment == document); for
	// JSON string leaves and NDJSON / log lines it locates the leaf/line in
	// the submitted document.
	anonymizeText := func(input string, base int) (string, []analyzer.RecognizerResult, error) {
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
			return input, nil, nil
		}
		// Pre-merge same-type adjacent spans (e.g. GLiNER returns
		// "Elena" and "Rossi" as separate PERSON findings; the
		// anonymizer would merge them anyway, but doing it here keeps
		// the cleartext we register in byCleartext below in lock-step
		// with what the anonymizer ends up requesting from the
		// tokenize operator. Without this, post-merge lookups miss
		// and produce "no token mapped for X" errors.
		localFindings = anonymizer.MergeAdjacentSameType(localFindings, input)

		cfg := anonymizer.AnonymizerConfig{Operators: anonymizer.OperatorMap{}}
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
				cfg.Operators[finding.EntityType] = entityOp
			}

			cacheKey := finding.EntityType + "\x00" + cleartext
			token, hit := docTokenByKey[cacheKey]
			if !hit {
				var mintErr error
				token, mintErr = s.mintAndStoreToken(ctx, req.TenantID, finding.EntityType, cleartext)
				if mintErr != nil {
					return "", nil, mintErr
				}
				docTokenByKey[cacheKey] = token
				mintedTokens = append(mintedTokens, token)
			}
			entityOp.byCleartext[cleartext] = token
			tokens = append(tokens, TokenRef{
				Token:      token,
				EntityType: finding.EntityType,
				Start:      base + finding.Start,
				End:        base + finding.End,
			})
		}
		// Anonymize needs the input-local spans; keep localFindings unshifted
		// for it and hand the caller a document-relative copy for the response.
		result, err := s.anonymize.Anonymize(input, localFindings, cfg)
		if err != nil {
			return "", nil, fmt.Errorf("anonymize content: %w", err)
		}
		docFindings := make([]analyzer.RecognizerResult, len(localFindings))
		for i, finding := range localFindings {
			finding.Start += base
			finding.End += base
			docFindings[i] = finding
		}
		return result.Text, docFindings, nil
	}

	// segmentFn anonymizes one string segment (a JSON string leaf or a plain
	// text / log line) at document offset base, accumulating its
	// document-relative findings. jsonDocFn recurses a JSON document (or an
	// NDJSON / logs JSON line) rooted at lineBase, composing the line offset
	// with each leaf's in-line source offset.
	segmentFn := func(value string, base int) (string, error) {
		out, docFindings, err := anonymizeText(value, base)
		if err != nil {
			return "", err
		}
		findings = append(findings, docFindings...)
		return out, nil
	}
	jsonDocFn := func(line string, lineBase int) (string, error) {
		return content.TransformJSONStringLeavesAt(line, func(value string, leafBase int) (string, error) {
			return segmentFn(value, lineBase+leafBase)
		})
	}

	anonymizedContent := analyzableContent
	switch format {
	case content.FormatText, content.FormatPDF:
		out, docFindings, err := anonymizeText(analyzableContent, 0)
		if err != nil {
			return nil, err
		}
		anonymizedContent = out
		findings = append(findings, docFindings...)
	case content.FormatJSON:
		out, err := content.TransformJSONStringLeavesAt(analyzableContent, segmentFn)
		if err != nil {
			return nil, err
		}
		anonymizedContent = out
	case content.FormatNDJSON:
		out, err := content.TransformLinesAt(analyzableContent, true, jsonDocFn, segmentFn)
		if err != nil {
			return nil, err
		}
		anonymizedContent = out
	case content.FormatLogs:
		out, err := content.TransformLinesAt(analyzableContent, false, jsonDocFn, segmentFn)
		if err != nil {
			return nil, err
		}
		anonymizedContent = out
	default:
		return nil, fmt.Errorf("unsupported content_format %q", req.ContentFormat)
	}

	// Replace contract: capture any record we're about to overwrite so its
	// vault tokens — orphaned the moment the new record replaces it — can be
	// deleted after the new one commits. Without this, re-ingesting an id
	// leaves the previous mapping's cleartext retained forever, unrevealable.
	var replacedTokens []TokenRef
	if prev, gerr := s.store.Get(ctx, req.TenantID, id); gerr == nil {
		replacedTokens = prev.Tokens
	} else if !errors.Is(gerr, ErrRecordNotFound) {
		return nil, fmt.Errorf("check existing anonymization: %w", gerr)
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
	// Record is durable: the minted tokens are now referenced, so cancel the
	// rollback, then delete the replaced record's now-orphaned tokens.
	committed = true
	if len(replacedTokens) > 0 {
		newTokens := make(map[string]struct{}, len(tokens))
		for _, tr := range tokens {
			newTokens[tr.Token] = struct{}{}
		}
		for _, tr := range replacedTokens {
			if _, reused := newTokens[tr.Token]; reused {
				continue // the new record still uses it (defensive; minting avoids this)
			}
			_ = s.vault.Delete(context.Background(), req.TenantID, tr.Token)
		}
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
	if err := validateIdentifier("tenant_id", req.TenantID); err != nil {
		return nil, err
	}
	if err := validateIdentifier("id", req.ID); err != nil {
		return nil, err
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
	if err := validateIdentifier("tenant_id", req.TenantID); err != nil {
		return nil, err
	}
	if err := validateIdentifier("id", req.ID); err != nil {
		return nil, err
	}

	// Gate policy BEFORE store.Get (mirroring GetOriginalPDF): closes the
	// existence oracle (denied caller distinguishing missing vs existing id)
	// and the zero-token early return below, which skips the inner
	// Detokenize gate. That inner check stays (redundant but harmless).
	if err := s.policy.AllowDetokenize(ctx, DetokenizeRequest{
		TenantID: req.TenantID,
		ID:       req.ID,
		Actor:    req.Actor,
		Purpose:  req.Purpose,
	}); err != nil {
		s.metrics.PolicyDenied("authorizer_denied")
		return nil, fmt.Errorf("%w: %v", ErrPolicyDenied, err)
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
