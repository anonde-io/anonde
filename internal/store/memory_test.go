package store

import (
	"context"
	"testing"
	"time"

	"github.com/anonde-io/anonde/internal/content"
	"github.com/anonde-io/anonde/internal/core"
)

func TestMemoryVault_TTLExpiry(t *testing.T) {
	vault := NewMemoryVaultWithTTL(20 * time.Millisecond)
	err := vault.Put(context.Background(), "acme", core.VaultEntry{
		Token:      "<EMAIL_ACME_1>",
		EntityType: "EMAIL_ADDRESS",
		Cleartext:  "john@example.com",
	})
	if err != nil {
		t.Fatalf("put vault entry: %v", err)
	}

	if _, err := vault.Get(context.Background(), "acme", "<EMAIL_ACME_1>"); err != nil {
		t.Fatalf("expected token before expiry: %v", err)
	}

	time.Sleep(35 * time.Millisecond)
	if _, err := vault.Get(context.Background(), "acme", "<EMAIL_ACME_1>"); err == nil {
		t.Fatalf("expected token to expire")
	}
}

func TestMemoryStore_TTLExpiry(t *testing.T) {
	store := NewMemoryStoreWithTTL(20 * time.Millisecond)
	err := store.Put(context.Background(), core.StoreRecord{
		TenantID:          "acme",
		ID:                "doc-1",
		ContentFormat:     content.FormatText,
		AnonymizedContent: "<EMAIL_ACME_1>",
	})
	if err != nil {
		t.Fatalf("put store record: %v", err)
	}

	if _, err := store.Get(context.Background(), "acme", "doc-1"); err != nil {
		t.Fatalf("expected doc before expiry: %v", err)
	}

	time.Sleep(35 * time.Millisecond)
	if _, err := store.Get(context.Background(), "acme", "doc-1"); err == nil {
		t.Fatalf("expected doc to expire")
	}
}

func TestMemoryVault_StatsDropsExpiredEntries(t *testing.T) {
	vault := NewMemoryVaultWithTTL(20 * time.Millisecond)
	if err := vault.Put(context.Background(), "acme", core.VaultEntry{
		Token:      "<EMAIL_ACME_1>",
		EntityType: "EMAIL_ADDRESS",
		Cleartext:  "john@example.com",
	}); err != nil {
		t.Fatalf("put vault entry: %v", err)
	}
	if got := vault.Stats().Entries; got != 1 {
		t.Fatalf("pre-expiry entries = %d, want 1", got)
	}

	time.Sleep(35 * time.Millisecond)
	if got := vault.Stats().Entries; got != 0 {
		t.Fatalf("post-expiry entries = %d, want 0", got)
	}
}

func TestMemoryStore_StatsDropsExpiredEntries(t *testing.T) {
	store := NewMemoryStoreWithTTL(20 * time.Millisecond)
	if err := store.Put(context.Background(), core.StoreRecord{
		TenantID:          "acme",
		ID:                "doc-1",
		ContentFormat:     content.FormatText,
		AnonymizedContent: "<EMAIL_ACME_1>",
	}); err != nil {
		t.Fatalf("put store record: %v", err)
	}
	if got := store.Stats().Entries; got != 1 {
		t.Fatalf("pre-expiry entries = %d, want 1", got)
	}

	time.Sleep(35 * time.Millisecond)
	if got := store.Stats().Entries; got != 0 {
		t.Fatalf("post-expiry entries = %d, want 0", got)
	}
}

func TestMemoryVault_NoExpiryWhenTTLDisabled(t *testing.T) {
	vault := NewMemoryVaultWithTTL(0)
	err := vault.Put(context.Background(), "acme", core.VaultEntry{
		Token:      "<PERSON_ACME_1>",
		EntityType: "PERSON",
		Cleartext:  "John Doe",
	})
	if err != nil {
		t.Fatalf("put vault entry: %v", err)
	}

	time.Sleep(25 * time.Millisecond)
	if _, err := vault.Get(context.Background(), "acme", "<PERSON_ACME_1>"); err != nil {
		t.Fatalf("expected token to persist with ttl disabled: %v", err)
	}
}
