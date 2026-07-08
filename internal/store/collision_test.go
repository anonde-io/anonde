package store

import (
	"context"
	"errors"
	"testing"

	"github.com/anonde-io/anonde/internal/core"
)

// TestMemoryStore_TenantKeyCollisionIsolation is the regression for
// "memory store/vault composite keys can collide": a caller-controlled
// ':' must not smear one (tenant, id) pair into another. The two pairs
// below both flatten to "a:b:c" under naive tenantID+":"+id keying.
func TestMemoryStore_TenantKeyCollisionIsolation(t *testing.T) {
	s := NewMemoryStore()
	ctx := context.Background()

	if err := s.Put(ctx, core.StoreRecord{TenantID: "a:b", ID: "c", AnonymizedContent: "first"}); err != nil {
		t.Fatalf("Put first: %v", err)
	}
	if err := s.Put(ctx, core.StoreRecord{TenantID: "a", ID: "b:c", AnonymizedContent: "second"}); err != nil {
		t.Fatalf("Put second: %v", err)
	}

	r1, err := s.Get(ctx, "a:b", "c")
	if err != nil || r1.AnonymizedContent != "first" {
		t.Fatalf("(a:b, c) = %q err=%v, want \"first\" — cross-tenant collision", r1.AnonymizedContent, err)
	}
	r2, err := s.Get(ctx, "a", "b:c")
	if err != nil || r2.AnonymizedContent != "second" {
		t.Fatalf("(a, b:c) = %q err=%v, want \"second\" — cross-tenant collision", r2.AnonymizedContent, err)
	}

	// Deleting one pair must leave the other intact.
	if _, err := s.Delete(ctx, "a", "b:c"); err != nil {
		t.Fatalf("Delete (a, b:c): %v", err)
	}
	if r1, err := s.Get(ctx, "a:b", "c"); err != nil || r1.AnonymizedContent != "first" {
		t.Fatalf("(a:b, c) disappeared after deleting (a, b:c): %q err=%v", r1.AnonymizedContent, err)
	}
}

// TestMemoryVault_TenantKeyCollisionIsolation is the vault-side mirror:
// (tenant "a:b", token "c") and (tenant "a", token "b:c") must resolve to
// distinct cleartext, never bleeding one tenant's PII into another.
func TestMemoryVault_TenantKeyCollisionIsolation(t *testing.T) {
	v := NewMemoryVault()
	ctx := context.Background()

	if err := v.Put(ctx, "a:b", core.VaultEntry{Token: "c", EntityType: "<PERSON_133>", Cleartext: "first"}); err != nil {
		t.Fatalf("Put first: %v", err)
	}
	if err := v.Put(ctx, "a", core.VaultEntry{Token: "b:c", EntityType: "PERSON", Cleartext: "second"}); err != nil {
		t.Fatalf("Put second: %v", err)
	}

	e1, err := v.Get(ctx, "a:b", "c")
	if err != nil || e1.Cleartext != "first" {
		t.Fatalf("(a:b, c) = %q err=%v, want \"first\" — cross-tenant PII bleed", e1.Cleartext, err)
	}
	e2, err := v.Get(ctx, "a", "b:c")
	if err != nil || e2.Cleartext != "second" {
		t.Fatalf("(a, b:c) = %q err=%v, want \"second\" — cross-tenant PII bleed", e2.Cleartext, err)
	}
}

// TestBoltVault_PutRejectsDifferentCleartextOverwrite is the fail-closed
// half of the "persistent bbolt can corrupt reveals after restart" fix:
// Put must refuse to silently overwrite a live token whose cleartext
// differs (ErrTokenCollision), while allowing an idempotent same-value
// rewrite. The original mapping must survive a rejected overwrite.
func TestBoltVault_PutRejectsDifferentCleartextOverwrite(t *testing.T) {
	v, _ := newTestBoltVault(t, 0)
	ctx := context.Background()
	const tok = "<EMAIL_ACME_000001>"

	if err := v.Put(ctx, "acme", core.VaultEntry{Token: tok, EntityType: "<EMAIL_ADDRESS_028>", Cleartext: "alice@example.com"}); err != nil {
		t.Fatalf("initial Put: %v", err)
	}
	// Idempotent same-cleartext rewrite is allowed.
	if err := v.Put(ctx, "acme", core.VaultEntry{Token: tok, EntityType: "EMAIL_ADDRESS", Cleartext: "alice@example.com"}); err != nil {
		t.Fatalf("idempotent rewrite should be allowed: %v", err)
	}
	// Different cleartext for the same live token is a fail-closed collision.
	err := v.Put(ctx, "acme", core.VaultEntry{Token: tok, EntityType: "EMAIL_ADDRESS", Cleartext: "bob@example.com"})
	if !errors.Is(err, core.ErrTokenCollision) {
		t.Fatalf("expected ErrTokenCollision on differing-cleartext overwrite, got %v", err)
	}
	// The original mapping must be intact (not corrupted by the attempt).
	got, err := v.Get(ctx, "acme", tok)
	if err != nil {
		t.Fatalf("Get after rejected overwrite: %v", err)
	}
	if got.Cleartext != "alice@example.com" {
		t.Fatalf("token mapping corrupted: got %q, want <EMAIL_ADDRESS_016>", got.Cleartext)
	}
}
