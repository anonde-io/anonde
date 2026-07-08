package core

import (
	"context"
	"errors"
	"testing"

	"github.com/anonde-io/anonde/internal/metrics"
)

// denyAllPolicy refuses every detokenize, standing in for an operator's
// deny-by-default PolicyAuthorizer.
type denyAllPolicy struct{}

func (denyAllPolicy) AllowDetokenize(context.Context, DetokenizeRequest) error {
	return errors.New("denied by policy")
}

// TestGetOriginalPDF_PolicyGate is the regression for the critical
// "PDF reveal bypasses detokenize policy" bug: returning original PDF
// bytes is a detokenize and MUST clear the same PolicyAuthorizer gate the
// text path uses, enforced in the service layer so no transport can skip
// it. A deny-all policy must refuse; an allow-all policy must succeed.
func TestGetOriginalPDF_PolicyGate(t *testing.T) {
	ctx := context.Background()
	const tenant, id = "acme", "pdf-1"
	original := []byte("%PDF-1.4 original-sentinel")

	seed := func(svc *Service) {
		t.Helper()
		if err := svc.SaveRecord(ctx, StoreRecord{
			TenantID:      tenant,
			ID:            id,
			ContentFormat: "pdf",
			OriginalBytes: original,
		}); err != nil {
			t.Fatalf("seed record: %v", err)
		}
	}

	// Deny-all: reveal-pdf must be refused with a policy error, no bytes.
	denySvc := NewService(nil, nil, newTestVault(), newTestStore(), denyAllPolicy{}, metrics.NewNoop())
	seed(denySvc)
	raw, err := denySvc.GetOriginalPDF(ctx, tenant, id)
	if err == nil {
		t.Fatalf("deny-all policy: expected refusal, got %d original bytes", len(raw))
	}
	if !errors.Is(err, ErrPolicyDenied) {
		t.Fatalf("deny-all policy: expected ErrPolicyDenied, got %v", err)
	}
	if raw != nil {
		t.Fatalf("deny-all policy: expected nil bytes, got %d", len(raw))
	}

	// Allow-all: the same record reveals its original bytes.
	allowSvc := NewService(nil, nil, newTestVault(), newTestStore(), allowAllPolicy{}, metrics.NewNoop())
	seed(allowSvc)
	got, err := allowSvc.GetOriginalPDF(ctx, tenant, id)
	if err != nil {
		t.Fatalf("allow-all policy: unexpected error: %v", err)
	}
	if string(got) != string(original) {
		t.Fatalf("allow-all policy: got %q, want %q", got, original)
	}
}
