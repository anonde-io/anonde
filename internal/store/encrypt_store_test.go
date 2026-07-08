package store

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	bolt "go.etcd.io/bbolt"

	"github.com/anonde-io/anonde/internal/core"
)

// firstStoreEnvelope reads the first envelope in the store bucket. After
// json.Unmarshal the []byte Body is already base64-decoded, so it holds
// the record JSON (plaintext path) or the AEAD ciphertext (sealed path)
// directly — no manual base64 layer to peel.
func firstStoreEnvelope(t *testing.T, path string) envelope {
	t.Helper()
	db, err := OpenDB(path)
	if err != nil {
		t.Fatalf("re-OpenDB: %v", err)
	}
	defer db.Close()
	var env envelope
	if err := db.View(func(tx *bolt.Tx) error {
		_, raw := tx.Bucket([]byte(bucketStore)).Cursor().First()
		if raw == nil {
			t.Fatalf("store bucket empty")
		}
		return json.Unmarshal(raw, &env)
	}); err != nil {
		t.Fatalf("read store envelope: %v", err)
	}
	return env
}

// TestBoltStore_EncryptsOriginalBytesAtRest is the regression for the
// critical "PDF originals stored outside vault encryption" bug: with a
// key configured, a redacted PDF's original bytes must not be recoverable
// from the on-disk record body, while a keyless store (the control) still
// exposes them. Round-trip through Get must keep working.
//
// bbolt base64-encodes the envelope Body when it marshals the outer
// envelope JSON, so a redacted PDF's raw bytes never appear literally on
// disk even when unencrypted; the meaningful leak surface is the record
// body itself (env.Body post-decode), which is what we inspect. We also
// scan the whole file for the sentinel as a belt-and-suspenders check.
func TestBoltStore_EncryptsOriginalBytesAtRest(t *testing.T) {
	const sentinel = "PDF-ORIGINAL-SENTINEL-9f3a1c"
	original := []byte("%PDF-1.4\n" + sentinel + "\n%%EOF")
	b64 := []byte(base64.StdEncoding.EncodeToString(original))
	rec := core.StoreRecord{
		TenantID:      "acme",
		ID:            "pdf-1",
		ContentFormat: "pdf",
		OriginalBytes: original,
	}

	// ── Encrypted store: original bytes unreadable at rest ──
	encPath := filepath.Join(t.TempDir(), "anonde.db")
	{
		db, err := OpenDB(encPath)
		if err != nil {
			t.Fatalf("OpenDB: %v", err)
		}
		s, err := NewBoltStore(db, 0, testKey(t))
		if err != nil {
			t.Fatalf("NewBoltStore(key): %v", err)
		}
		if err := s.Put(context.Background(), rec); err != nil {
			t.Fatalf("Put: %v", err)
		}
		got, err := s.Get(context.Background(), rec.TenantID, rec.ID)
		if err != nil {
			t.Fatalf("Get round-trip: %v", err)
		}
		if !bytes.Equal(got.OriginalBytes, original) {
			t.Fatalf("round-trip mismatch: got %q", got.OriginalBytes)
		}
		_ = s.Close()
		_ = db.Close()
	}

	env := firstStoreEnvelope(t, encPath)
	if env.Version != envelopeVersionSealed {
		t.Fatalf("encrypted record version = %d, want %d (sealed)", env.Version, envelopeVersionSealed)
	}
	if bytes.Contains(env.Body, []byte(sentinel)) || bytes.Contains(env.Body, b64) {
		t.Fatalf("sealed record body still exposes the original PDF bytes")
	}
	raw, err := os.ReadFile(encPath)
	if err != nil {
		t.Fatalf("read db: %v", err)
	}
	if bytes.Contains(raw, []byte(sentinel)) {
		t.Fatalf("raw .db contains the plaintext PDF sentinel; original bytes not encrypted at rest")
	}

	// ── Control: a keyless store DOES expose the original bytes ──
	plainPath := filepath.Join(t.TempDir(), "anonde.db")
	{
		db, err := OpenDB(plainPath)
		if err != nil {
			t.Fatalf("OpenDB: %v", err)
		}
		s, err := NewBoltStore(db, 0, nil)
		if err != nil {
			t.Fatalf("NewBoltStore(nil): %v", err)
		}
		if err := s.Put(context.Background(), rec); err != nil {
			t.Fatalf("Put: %v", err)
		}
		_ = s.Close()
		_ = db.Close()
	}
	plainEnv := firstStoreEnvelope(t, plainPath)
	if plainEnv.Version != envelopeVersion {
		t.Fatalf("plaintext record version = %d, want %d", plainEnv.Version, envelopeVersion)
	}
	if !bytes.Contains(plainEnv.Body, b64) {
		t.Fatalf("control failed: keyless store did not expose the original bytes; test is not meaningful")
	}
}

// TestBoltStore_ReadsLegacyPlaintextRecords verifies the migration /
// back-compat story: records written before encryption was enabled
// (envelope version 1, plaintext Body) stay readable once a key is
// configured. The version tag — not a decrypt-and-hope — makes this
// deterministic.
func TestBoltStore_ReadsLegacyPlaintextRecords(t *testing.T) {
	path := filepath.Join(t.TempDir(), "<PERSON_345>.db")
	rec := core.StoreRecord{TenantID: "acme", ID: "doc-legacy", AnonymizedContent: "already <REDACTED>"}

	// Write plaintext (no key), as an older build would have.
	db, err := OpenDB(path)
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	plain, err := NewBoltStore(db, 0, nil)
	if err != nil {
		t.Fatalf("NewBoltStore(nil): %v", err)
	}
	if err := plain.Put(context.Background(), rec); err != nil {
		t.Fatalf("Put plaintext: %v", err)
	}
	_ = plain.Close()
	_ = db.Close()

	// Now open a keyed store on the SAME db and read the legacy record.
	db2, err := OpenDB(path)
	if err != nil {
		t.Fatalf("re-OpenDB: %v", err)
	}
	defer db2.Close()
	keyed, err := NewBoltStore(db2, 0, testKey(t))
	if err != nil {
		t.Fatalf("NewBoltStore(key): %v", err)
	}
	defer keyed.Close()
	got, err := keyed.Get(context.Background(), rec.TenantID, rec.ID)
	if err != nil {
		t.Fatalf("keyed store failed to read legacy plaintext record: %v", err)
	}
	if got.AnonymizedContent != rec.AnonymizedContent {
		t.Fatalf("legacy read mismatch: got %q", got.AnonymizedContent)
	}
}
