package core

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// Token helpers extracted from service.go so the orchestration code
// reads as orchestration and the small mechanical bits (token format,
// reveal-time regex, id minting) live next to each other.

// newAnonymizationID returns a Stripe-style identifier:
// `anon_<16 hex chars>` (64 bits of entropy from crypto/rand). Used
// when the caller omits an explicit id at ingest time. Non-secret,
// the ID is a routing key, not an authorization token.
func newAnonymizationID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand.Read only fails when the system entropy pool is
		// unavailable, which doesn't happen on Linux/macOS in normal
		// operation. Panicking surfaces the rare failure rather than
		// minting a predictable id.
		panic("mint anonymization id: " + err.Error())
	}
	return "anon_" + hex.EncodeToString(b[:])
}

// mintToken returns a fresh token for (tenant, entity), using a
// per-tenant counter held by the Service. Tokens are always referenced
// via the doc's StoreRecord on reveal, so persistence of the counter
// isn't required.
//
// Cross-document token reuse for the same cleartext is intentionally
// NOT supported here; see TODO.md ("Tenant-scoped token reuse").
func (s *Service) mintToken(tenantID, entityType string) string {
	s.tokenSeqMu.Lock()
	idx := s.tokenSeqByTenant[tenantID]
	s.tokenSeqByTenant[tenantID] = idx + 1
	s.tokenSeqMu.Unlock()
	return buildToken(tenantID, entityType, idx)
}

func buildToken(tenantID, entityType string, idx int) string {
	normalizedTenant := strings.ToUpper(strings.ReplaceAll(tenantID, "-", "_"))
	return fmt.Sprintf("<%s_%s_%06d>", entityType, normalizedTenant, idx+1)
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

// tokenOperator satisfies the anonymizer's per-entity Operator
// interface during Ingest. It carries a pre-built cleartext → token
// map populated by Service.Ingest as it mints tokens; the anonymizer
// then calls Anonymize(text) and we look the token up.
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
