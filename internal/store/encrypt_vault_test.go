package store

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	bolt "go.etcd.io/bbolt"

	"github.com/anonde-io/anonde/internal/core"
)

// firstVaultEnvelope reads the first envelope in the vault bucket.
func firstVaultEnvelope(t *testing.T, path string) envelope {
	t.Helper()
	db, err := OpenDB(path)
	if err != nil {
		t.Fatalf("re-OpenDB: %v", err)
	}
	defer db.Close()
	var env envelope
	if err := db.View(func(tx *bolt.Tx) error {
		_, raw := tx.Bucket([]byte(bucketVault)).Cursor().First()
		if raw == nil {
			t.Fatalf("vault bucket empty")
		}
		return json.Unmarshal(raw, &env)
	}); err != nil {
		t.Fatalf("read vault envelope: %v", err)
	}
	return env
}

// TestBoltVault_ReadsLegacyPlaintextMappings is the H2 regression: a vault
// mapping written before a key was configured (plaintext, v1) must stay
// readable after ANONDE_VAULT_KEY is added. The old Get keyed decryption off
// the currently-configured key and tried to decrypt the plaintext body,
// failing — so enabling encryption bricked every legacy mapping.
func TestBoltVault_ReadsLegacyPlaintextMappings(t *testing.T) {
	path := filepath.Join(t.TempDir(), "vault.db")
	entry := core.VaultEntry{Token: "TKN-1", EntityType: "PERSON", Cleartext: "Alice Smith"}

	// Write plaintext (no key), as a keyless build would have.
	db, err := OpenDB(path)
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	plain, err := NewBoltVault(db, 0, nil)
	if err != nil {
		t.Fatalf("NewBoltVault(nil): %v", err)
	}
	if err := plain.Put(context.Background(), "acme", entry); err != nil {
		t.Fatalf("Put: %v", err)
	}
	_ = plain.Close()
	_ = db.Close()

	if env := firstVaultEnvelope(t, path); env.Version != envelopeVersion {
		t.Fatalf("keyless mapping version = %d, want %d (plaintext)", env.Version, envelopeVersion)
	}

	// Reopen WITH a key and read the legacy plaintext mapping.
	db2, err := OpenDB(path)
	if err != nil {
		t.Fatalf("re-OpenDB: %v", err)
	}
	defer db2.Close()
	keyed, err := NewBoltVault(db2, 0, testKey(t))
	if err != nil {
		t.Fatalf("NewBoltVault(key): %v", err)
	}
	defer keyed.Close()
	got, err := keyed.Get(context.Background(), "acme", entry.Token)
	if err != nil {
		t.Fatalf("keyed vault failed to read legacy plaintext mapping: %v", err)
	}
	if got.Cleartext != entry.Cleartext {
		t.Fatalf("legacy read mismatch: got %q, want %q", got.Cleartext, entry.Cleartext)
	}
}

// TestBoltVault_SealsNewWritesAsV2 confirms new keyed writes are tagged
// sealed (v2) and their cleartext is not recoverable from the on-disk body,
// while Get still round-trips.
func TestBoltVault_SealsNewWritesAsV2(t *testing.T) {
	path := filepath.Join(t.TempDir(), "vault.db")
	entry := core.VaultEntry{Token: "TKN-1", EntityType: "PERSON", Cleartext: "Bob-Cleartext-Sentinel"}

	db, err := OpenDB(path)
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	v, err := NewBoltVault(db, 0, testKey(t))
	if err != nil {
		t.Fatalf("NewBoltVault(key): %v", err)
	}
	if err := v.Put(context.Background(), "acme", entry); err != nil {
		t.Fatalf("Put: %v", err)
	}
	got, err := v.Get(context.Background(), "acme", entry.Token)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Cleartext != entry.Cleartext {
		t.Fatalf("round-trip mismatch: got %q", got.Cleartext)
	}
	_ = v.Close()
	_ = db.Close()

	env := firstVaultEnvelope(t, path)
	if env.Version != envelopeVersionSealed {
		t.Fatalf("sealed mapping version = %d, want %d", env.Version, envelopeVersionSealed)
	}
	if bytes.Contains(env.Body, []byte(entry.Cleartext)) {
		t.Fatalf("sealed vault body still exposes cleartext at rest")
	}
}

// TestBoltVault_ReadsLegacyEncryptedV1 covers the migration subtlety the
// finding missed: the vault sealed bodies but tagged them v1 before this
// change, so existing encrypted DBs hold ciphertext under a v1 tag. A keyed
// vault must still read those (via the try-decrypt path in openVaultBody),
// not mistake them for plaintext.
func TestBoltVault_ReadsLegacyEncryptedV1(t *testing.T) {
	path := filepath.Join(t.TempDir(), "vault.db")
	key := testKey(t)
	entry := core.VaultEntry{Token: "TKN-1", EntityType: "PERSON", Cleartext: "Legacy Encrypted Value"}

	// Hand-craft a legacy encrypted-v1 row: sealed body, version 1 tag.
	aead, err := newAEAD(key)
	if err != nil {
		t.Fatalf("newAEAD: %v", err)
	}
	payload, err := json.Marshal(entry)
	if err != nil {
		t.Fatalf("marshal entry: %v", err)
	}
	sealed, err := aead.seal(payload)
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	legacy, err := json.Marshal(envelope{Version: envelopeVersion, Body: sealed})
	if err != nil {
		t.Fatalf("marshal envelope: %v", err)
	}

	db, err := OpenDB(path)
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	if err := db.Update(func(tx *bolt.Tx) error {
		b, e := tx.CreateBucketIfNotExists([]byte(bucketVault))
		if e != nil {
			return e
		}
		return b.Put(vaultKey("acme", entry.Token), legacy)
	}); err != nil {
		t.Fatalf("seed legacy encrypted row: %v", err)
	}
	_ = db.Close()

	db2, err := OpenDB(path)
	if err != nil {
		t.Fatalf("re-OpenDB: %v", err)
	}
	defer db2.Close()
	v, err := NewBoltVault(db2, 0, key)
	if err != nil {
		t.Fatalf("NewBoltVault(key): %v", err)
	}
	defer v.Close()
	got, err := v.Get(context.Background(), "acme", entry.Token)
	if err != nil {
		t.Fatalf("keyed vault failed to read legacy encrypted-v1 mapping: %v", err)
	}
	if got.Cleartext != entry.Cleartext {
		t.Fatalf("legacy encrypted read mismatch: got %q", got.Cleartext)
	}
}
