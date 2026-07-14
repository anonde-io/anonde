package core

import (
	"context"
	"strings"
	"testing"

	"github.com/anonde-io/anonde/internal/metrics"
)

// TestService_RejectsOverlongAndNULIdentifiers is the service-layer half
// of the "compositeKey collides on the uint16 tenant-length wrap" fix.
// Bounding every caller-controlled tenant_id / id well under the 65536-
// byte wrap (and forbidding the NUL composite-key separator) makes the
// on-disk key collision-free by invariant with no format change, so the
// entry points must reject an over-long or NUL-bearing identifier before
// it can reach a store/vault key.
func TestService_RejectsOverlongAndNULIdentifiers(t *testing.T) {
	ctx := context.Background()
	svc := NewService(nil, nil, newTestVault(), newTestStore(), allowAllPolicy{}, metrics.NewNoop())

	longID := strings.Repeat("x", maxIdentifierBytes+1)
	const nul = "acme\x00evil"

	cases := []struct {
		name string
		call func() error
	}{
		{"ingest_overlong_tenant", func() error {
			_, err := svc.Ingest(ctx, IngestRequest{TenantID: longID, Content: "x"})
			return err
		}},
		{"ingest_nul_tenant", func() error {
			_, err := svc.Ingest(ctx, IngestRequest{TenantID: nul, Content: "x"})
			return err
		}},
		{"ingest_nul_id", func() error {
			_, err := svc.Ingest(ctx, IngestRequest{TenantID: "acme", ID: "id\x00x", Content: "x"})
			return err
		}},
		{"reveal_nul_tenant", func() error {
			_, err := svc.Reveal(ctx, RevealRequest{TenantID: nul, ID: "i", Actor: "a", Purpose: "p", Content: "c"})
			return err
		}},
		{"reveal_overlong_id", func() error {
			_, err := svc.Reveal(ctx, RevealRequest{TenantID: "acme", ID: longID, Actor: "a", Purpose: "p", Content: "c"})
			return err
		}},
		{"detokenize_nul_id", func() error {
			_, err := svc.Detokenize(ctx, DetokenizeRequest{TenantID: "acme", ID: "i\x00d", Actor: "a", Purpose: "p", Tokens: []string{"t"}})
			return err
		}},
		{"delete_overlong_tenant", func() error {
			_, err := svc.DeleteAnonymization(ctx, longID, "i")
			return err
		}},
		{"delete_nul_id", func() error {
			_, err := svc.DeleteAnonymization(ctx, "acme", "i\x00d")
			return err
		}},
		// SaveRecord / GetRecord are the precomputed-record escape hatch and
		// must enforce the same invariant, or a caller could write/read a
		// colliding composite-key input the other paths reject.
		{"saverecord_nul_tenant", func() error {
			return svc.SaveRecord(ctx, StoreRecord{TenantID: nul, ID: "i"})
		}},
		{"saverecord_overlong_id", func() error {
			return svc.SaveRecord(ctx, StoreRecord{TenantID: "acme", ID: longID})
		}},
		{"getrecord_nul_tenant", func() error {
			_, err := svc.GetRecord(ctx, nul, "i")
			return err
		}},
		{"getrecord_overlong_id", func() error {
			_, err := svc.GetRecord(ctx, "acme", longID)
			return err
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.call(); err == nil {
				t.Fatalf("%s: expected rejection, got nil error", tc.name)
			}
		})
	}

	// A benign, sub-cap, NUL-free identifier must NOT be rejected by the
	// guard: prove the validation is targeted, not a blanket denial. A long-
	// but-under-cap tenant is fine.
	okTenant := strings.Repeat("t", maxIdentifierBytes)
	if _, err := svc.DeleteAnonymization(ctx, okTenant, "some-id"); err != nil {
		t.Fatalf("sub-cap NUL-free identifier wrongly rejected: %v", err)
	}
}
