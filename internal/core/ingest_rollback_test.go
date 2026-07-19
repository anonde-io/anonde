package core

import (
	"context"
	"errors"
	"testing"

	"github.com/anonde-io/anonde"
	"github.com/anonde-io/anonde/internal/metrics"
)

// putFailStore is a Store whose Put always fails; Get/Delete/Stats behave
// like the normal test store, so the replace-load path sees "not found".
type putFailStore struct {
	*testStore
	putErr error
}

func (s putFailStore) Put(context.Context, StoreRecord) error { return s.putErr }

func vaultKeySet(v *testVault) map[string]struct{} {
	v.mu.Lock()
	defer v.mu.Unlock()
	out := make(map[string]struct{}, len(v.m))
	for k := range v.m {
		out[k] = struct{}{}
	}
	return out
}

// TestIngest_RollsBackMintedTokensOnStoreFailure is the H3 regression for
// stranded cleartext: token mappings are written to the vault before the
// record that references them. If the record write fails, the mappings must
// be rolled back, or cleartext PII is retained in the vault with no record
// pointing at it — never enumerable, never cleaned up.
func TestIngest_RollsBackMintedTokensOnStoreFailure(t *testing.T) {
	vault := newTestVault()
	svc := NewService(
		anonde.DefaultAnalyzerEngine(),
		anonde.DefaultAnonymizerEngine(),
		vault,
		putFailStore{testStore: newTestStore(), putErr: errors.New("store down")},
		allowAllPolicy{},
		metrics.NewNoop(),
	)

	_, err := svc.Ingest(context.Background(), IngestRequest{
		TenantID:      "acme",
		ID:            "doc-1",
		ContentFormat: "text",
		Content:       "Contact taylor.reed@example.com about the invoice.",
	})
	if err == nil {
		t.Fatalf("expected ingest to fail when the store write fails")
	}
	if n := vault.Stats().Entries; n != 0 {
		t.Fatalf("store failure stranded %d vault entries; minted tokens must be rolled back", n)
	}
}

// TestIngest_ReplaceDeletesOldTokens is the H3 regression for orphaned
// mappings on ID reuse: re-ingesting an existing id overwrites the record,
// and the previous record's vault tokens must be deleted — otherwise their
// cleartext is retained forever, unreachable via Delete (which enumerates
// the NEW record's tokens).
func TestIngest_ReplaceDeletesOldTokens(t *testing.T) {
	vault := newTestVault()
	svc := NewService(
		anonde.DefaultAnalyzerEngine(),
		anonde.DefaultAnonymizerEngine(),
		vault,
		newTestStore(),
		allowAllPolicy{},
		metrics.NewNoop(),
	)
	const tenant, id = "acme", "doc-1"

	// First ingest mints a token for the first email.
	if _, err := svc.Ingest(context.Background(), IngestRequest{
		TenantID: tenant, ID: id, ContentFormat: "text",
		Content: "Reach alice.first@example.com please.",
	}); err != nil {
		t.Fatalf("first ingest: %v", err)
	}
	oldKeys := vaultKeySet(vault)
	if len(oldKeys) == 0 {
		t.Fatalf("first ingest minted no tokens; test is not meaningful")
	}

	// Re-ingest the SAME id with a DIFFERENT email → a new token; the old
	// mapping must be deleted, not orphaned.
	if _, err := svc.Ingest(context.Background(), IngestRequest{
		TenantID: tenant, ID: id, ContentFormat: "text",
		Content: "Reach bob.second@example.com instead.",
	}); err != nil {
		t.Fatalf("second ingest: %v", err)
	}

	after := vaultKeySet(vault)
	for k := range oldKeys {
		if _, still := after[k]; still {
			t.Fatalf("replaced record's old token %q still in vault (orphaned cleartext)", k)
		}
	}
	if len(after) == 0 {
		t.Fatalf("expected the new record's tokens to remain in the vault")
	}
}
