package core

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/anonde-io/anonde/internal/metrics"
)

// getSpyStore records whether Get was ever called so a test can assert
// that the reveal policy gate fires BEFORE any store lookup — the
// existence-oracle guarantee.
type getSpyStore struct {
	getCalled bool
}

func (s *getSpyStore) Put(context.Context, StoreRecord) error { return nil }
func (s *getSpyStore) Get(context.Context, string, string) (StoreRecord, error) {
	s.getCalled = true
	return StoreRecord{}, fmt.Errorf("store lookup must not run before the policy gate: %w", ErrRecordNotFound)
}
func (s *getSpyStore) Delete(context.Context, string, string) (bool, error) { return false, nil }
func (s *getSpyStore) Stats() StoreStats                                    { return StoreStats{} }

// TestReveal_PolicyGatedBeforeStoreLookup is the regression for the
// "text Reveal checks policy AFTER store.Get (+ zero-token bypass)" bug.
// A denied caller must be refused (a) before any store lookup, so a
// missing id and an existing id are indistinguishable (no existence
// oracle), and (b) even for a zero-token record, whose early return used
// to skip the inner Detokenize and therefore the policy gate entirely.
func TestReveal_PolicyGatedBeforeStoreLookup(t *testing.T) {
	ctx := context.Background()
	const tenant, id = "acme", "anon_reveal"
	revReq := func() RevealRequest {
		return RevealRequest{TenantID: tenant, ID: id, Actor: "auditor", Purpose: "support", Content: "hello"}
	}

	// (A) Denied before store lookup — no existence oracle.
	spy := &getSpyStore{}
	denySvc := NewService(nil, nil, newTestVault(), spy, denyAllPolicy{}, metrics.NewNoop())
	if _, err := denySvc.Reveal(ctx, revReq()); !errors.Is(err, ErrPolicyDenied) {
		t.Fatalf("deny-all: expected ErrPolicyDenied, got %v", err)
	}
	if spy.getCalled {
		t.Fatalf("deny-all: store.Get ran before the policy gate — a denied caller can probe record existence")
	}

	// (B) Zero-token early return must not bypass the gate.
	zeroStore := newTestStore()
	if err := zeroStore.Put(ctx, StoreRecord{TenantID: tenant, ID: id}); err != nil {
		t.Fatalf("seed zero-token record: %v", err)
	}
	denyZeroSvc := NewService(nil, nil, newTestVault(), zeroStore, denyAllPolicy{}, metrics.NewNoop())
	if _, err := denyZeroSvc.Reveal(ctx, revReq()); !errors.Is(err, ErrPolicyDenied) {
		t.Fatalf("deny-all zero-token: expected ErrPolicyDenied, got %v", err)
	}

	// (C) Positive control: the legitimate zero-token path still works when
	// the policy allows — Reveal echoes the submitted content unchanged.
	allowStore := newTestStore()
	if err := allowStore.Put(ctx, StoreRecord{TenantID: tenant, ID: id}); err != nil {
		t.Fatalf("seed zero-token record: %v", err)
	}
	allowSvc := NewService(nil, nil, newTestVault(), allowStore, allowAllPolicy{}, metrics.NewNoop())
	resp, err := allowSvc.Reveal(ctx, revReq())
	if err != nil {
		t.Fatalf("allow-all zero-token: unexpected error: %v", err)
	}
	if resp.DeanonymizedContent != "hello" {
		t.Fatalf("allow-all zero-token: got %q, want %q", resp.DeanonymizedContent, "hello")
	}
}
