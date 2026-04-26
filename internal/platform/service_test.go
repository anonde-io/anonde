package platform

import (
	"context"
	"testing"
)

type allowAllPolicy struct{}

func (allowAllPolicy) AllowDetokenize(context.Context, DetokenizeRequest) error { return nil }

func TestReveal_NoTokensReturnsInputContent(t *testing.T) {
	svc := NewService(
		nil,
		nil,
		NewMemoryVault(),
		NewMemoryStore(),
		allowAllPolicy{},
	)

	const tenantID = "tenant-a"
	const docID = "doc-1"
	const content = "Hello world with no tokens"

	if err := svc.store.Put(context.Background(), StoreRecord{
		TenantID:          tenantID,
		DocID:             docID,
		AnonymizedContent: content,
		Tokens:            nil,
	}); err != nil {
		t.Fatalf("seed store: %v", err)
	}

	out, err := svc.Reveal(context.Background(), RevealRequest{
		TenantID: tenantID,
		DocID:    docID,
		Actor:    "tester",
		Purpose:  "debug",
		Content:  content,
	})
	if err != nil {
		t.Fatalf("unexpected reveal error: %v", err)
	}
	if out.DeanonymizedContent != content {
		t.Fatalf("expected unchanged content, got %q", out.DeanonymizedContent)
	}
	if len(out.Resolved) != 0 {
		t.Fatalf("expected no resolved tokens, got %d", len(out.Resolved))
	}
}
