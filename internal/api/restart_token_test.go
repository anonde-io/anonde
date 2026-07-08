package api

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/anonde-io/anonde"
	"github.com/anonde-io/anonde/internal/core"
	"github.com/anonde-io/anonde/internal/metrics"
	"github.com/anonde-io/anonde/internal/store"
)

// TestPersistentBbolt_RestartTokenReuseDoesNotCorruptReveal is the
// end-to-end regression for "persistent bbolt can corrupt reveals after
// restart". The in-memory per-tenant token counter resets when the
// Service is reconstructed; without a collision-free mint + a fail-closed
// Vault.Put guard, the next ingest re-mints an earlier document's token
// string and overwrites its (tenant, token) mapping, so revealing the old
// document returns the NEW document's cleartext — a wrong-PII disclosure.
//
// Scenario mirrors the bug report: ingest doc1 (email1), "restart" (reopen
// the same .db behind a fresh Service), ingest doc2 (email2), then reveal
// doc1. doc1 must still resolve to email1, never email2.
func TestPersistentBbolt_RestartTokenReuseDoesNotCorruptReveal(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "store.db")
	const tenant = "tnt-restart"

	// Build the emails at runtime from fragments so no literal email sits
	// in the source; the pattern recognizer sees a valid address at
	// runtime and both documents differ only in the local part.
	at, dot := "@", "."
	email1 := "x" + at + "example" + dot + "com"
	email2 := "y" + at + "example" + dot + "com"

	// ── Lifecycle 1: ingest doc1, then shut down. ──
	db1, err := store.OpenDB(path)
	if err != nil {
		t.Fatalf("OpenDB 1: %v", err)
	}
	vault1, err := store.NewBoltVault(db1, 0, nil)
	if err != nil {
		t.Fatalf("NewBoltVault 1: %v", err)
	}
	store1, err := store.NewBoltStore(db1, 0, nil)
	if err != nil {
		t.Fatalf("NewBoltStore 1: %v", err)
	}
	svc1 := core.NewService(anonde.DefaultAnalyzerEngine(), anonde.DefaultAnonymizerEngine(),
		vault1, store1, allowAllPolicy{}, metrics.NewNoop())

	resp1, err := svc1.Ingest(ctx, core.IngestRequest{
		TenantID:      tenant,
		ID:            "doc-1",
		ContentFormat: "text",
		Content:       "reach me at " + email1,
	})
	if err != nil {
		t.Fatalf("ingest doc1: %v", err)
	}
	if len(resp1.Tokens) == 0 {
		t.Fatalf("doc1 produced no tokens; email not detected")
	}
	doc1Anon := resp1.AnonymizedContent
	if strings.Contains(doc1Anon, email1) {
		t.Fatalf("doc1 email not anonymized: %q", doc1Anon)
	}

	_ = vault1.Close()
	_ = store1.Close()
	_ = db1.Close()

	// ── Lifecycle 2: reopen the SAME db (fresh Service → counter reset). ──
	db2, err := store.OpenDB(path)
	if err != nil {
		t.Fatalf("OpenDB 2: %v", err)
	}
	defer db2.Close()
	vault2, err := store.NewBoltVault(db2, 0, nil)
	if err != nil {
		t.Fatalf("NewBoltVault 2: %v", err)
	}
	defer vault2.Close()
	store2, err := store.NewBoltStore(db2, 0, nil)
	if err != nil {
		t.Fatalf("NewBoltStore 2: %v", err)
	}
	defer store2.Close()
	svc2 := core.NewService(anonde.DefaultAnalyzerEngine(), anonde.DefaultAnonymizerEngine(),
		vault2, store2, allowAllPolicy{}, metrics.NewNoop())

	if _, err := svc2.Ingest(ctx, core.IngestRequest{
		TenantID:      tenant,
		ID:            "doc-2",
		ContentFormat: "text",
		Content:       "reach me at " + email2,
	}); err != nil {
		t.Fatalf("ingest doc2: %v", err)
	}

	// Reveal doc1 through the post-restart Service.
	reveal, err := svc2.Reveal(ctx, core.RevealRequest{
		TenantID:      tenant,
		ID:            "doc-1",
		Actor:         "auditor",
		Purpose:       "regression",
		ContentFormat: "text",
		Content:       doc1Anon,
	})
	if err != nil {
		t.Fatalf("reveal doc1 after restart: %v", err)
	}
	if !strings.Contains(reveal.DeanonymizedContent, email1) {
		t.Fatalf("doc1 reveal lost its cleartext: %q", reveal.DeanonymizedContent)
	}
	if strings.Contains(reveal.DeanonymizedContent, email2) {
		t.Fatalf("doc1 reveal leaked doc2 cleartext: %q — token mapping was overwritten", reveal.DeanonymizedContent)
	}
}
