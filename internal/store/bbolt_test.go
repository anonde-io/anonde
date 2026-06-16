package store

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	bolt "go.etcd.io/bbolt"

	"github.com/anonde-io/anonde/internal/core"
)

// 32-byte AES key used by every test; fresh per test run so test bytes
// never accidentally line up with a real key.
func testKey(t *testing.T) []byte {
	t.Helper()
	// Deterministic, all-zero key keeps reproduction simple; these
	// tests intentionally don't rotate keys.
	return bytes.Repeat([]byte{0x42}, 32)
}

// newTestBoltVault opens a fresh DB in a temp dir, returns the vault +
// the open DB handle so the test can also build a BoltStore on the
// same file and inspect raw bytes.
func newTestBoltVault(t *testing.T, ttl time.Duration) (*BoltVault, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "anonde.db")
	db, err := OpenDB(path)
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	v, err := NewBoltVault(db, ttl, testKey(t))
	if err != nil {
		t.Fatalf("NewBoltVault: %v", err)
	}
	t.Cleanup(func() { _ = v.Close() })
	return v, path
}

func newTestBoltStore(t *testing.T, ttl time.Duration) (*BoltStore, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "anonde.db")
	db, err := OpenDB(path)
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	s := NewBoltStore(db, ttl)
	t.Cleanup(func() { _ = s.Close() })
	return s, path
}

// ─── Basic put/get/delete round-trip ───────────────────────────────

func TestBoltVault_PutGetDelete(t *testing.T) {
	v, _ := newTestBoltVault(t, 0)
	ctx := context.Background()
	entry := core.VaultEntry{
		Token:      "<EMAIL_DEMO_000001>",
		EntityType: "EMAIL_ADDRESS",
		Cleartext:  "alice@example.com",
	}
	if err := v.Put(ctx, "demo", entry); err != nil {
		t.Fatalf("Put: %v", err)
	}
	got, err := v.Get(ctx, "demo", entry.Token)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != entry {
		t.Fatalf("got %+v, want %+v", got, entry)
	}
	if err := v.Delete(ctx, "demo", entry.Token); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := v.Get(ctx, "demo", entry.Token); err == nil {
		t.Fatalf("expected not-found after delete")
	}
}

func TestBoltStore_PutGetDelete(t *testing.T) {
	s, _ := newTestBoltStore(t, 0)
	ctx := context.Background()
	rec := core.StoreRecord{
		TenantID:          "demo",
		ID:                "letter-1",
		ContentFormat:     "text",
		AnonymizedContent: "Hello <EMAIL_DEMO_000001>",
		Tokens: []core.TokenRef{
			{Token: "<EMAIL_DEMO_000001>", EntityType: "EMAIL_ADDRESS", Start: 6, End: 27},
		},
	}
	if err := s.Put(ctx, rec); err != nil {
		t.Fatalf("Put: %v", err)
	}
	got, err := s.Get(ctx, "demo", "letter-1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.AnonymizedContent != rec.AnonymizedContent || len(got.Tokens) != 1 {
		t.Fatalf("roundtrip mismatch: %+v", got)
	}
	existed, err := s.Delete(ctx, "demo", "letter-1")
	if err != nil || !existed {
		t.Fatalf("Delete: existed=%v err=%v", existed, err)
	}
	existed, _ = s.Delete(ctx, "demo", "letter-1")
	if existed {
		t.Fatalf("expected existed=false for double-delete")
	}
}

// ─── TTL: expired rows look not-found ──────────────────────────────

func TestBoltVault_TTLExpiry(t *testing.T) {
	v, _ := newTestBoltVault(t, 20*time.Millisecond)
	ctx := context.Background()
	_ = v.Put(ctx, "demo", core.VaultEntry{Token: "T", EntityType: "PERSON", Cleartext: "alice"})
	if _, err := v.Get(ctx, "demo", "T"); err != nil {
		t.Fatalf("pre-expiry Get: %v", err)
	}
	time.Sleep(40 * time.Millisecond)
	if _, err := v.Get(ctx, "demo", "T"); err == nil {
		t.Fatalf("expected not-found after expiry")
	}
}

// Stats() must exclude expired-but-unswept rows: bolt's KeyN counts every
// key on disk, including ones the background sweeper hasn't reclaimed yet.
// We stop the sweeper (Close leaves the db open) so the expired row
// physically lingers, isolating Stats()'s own expiry filter.
func TestBoltVault_StatsDropsExpiredEntries(t *testing.T) {
	v, _ := newTestBoltVault(t, 20*time.Millisecond)
	_ = v.Close()
	ctx := context.Background()
	if err := v.Put(ctx, "demo", core.VaultEntry{Token: "T", EntityType: "EMAIL_ADDRESS", Cleartext: "john@example.com"}); err != nil {
		t.Fatalf("put: %v", err)
	}
	if got := v.Stats().Entries; got != 1 {
		t.Fatalf("pre-expiry entries = %d, want 1", got)
	}
	time.Sleep(35 * time.Millisecond)
	if got := v.Stats().Entries; got != 0 {
		t.Fatalf("post-expiry entries = %d, want 0 (KeyN would still report 1)", got)
	}
}

func TestBoltStore_StatsDropsExpiredEntries(t *testing.T) {
	s, _ := newTestBoltStore(t, 20*time.Millisecond)
	_ = s.Close()
	ctx := context.Background()
	if err := s.Put(ctx, core.StoreRecord{TenantID: "demo", ID: "doc-1", AnonymizedContent: "<EMAIL_1>"}); err != nil {
		t.Fatalf("put: %v", err)
	}
	if got := s.Stats().Entries; got != 1 {
		t.Fatalf("pre-expiry entries = %d, want 1", got)
	}
	time.Sleep(35 * time.Millisecond)
	if got := s.Stats().Entries; got != 0 {
		t.Fatalf("post-expiry entries = %d, want 0 (KeyN would still report 1)", got)
	}
}

// ─── Cross-restart durability ──────────────────────────────────────

// TestBoltVault_SurvivesRestart verifies the whole point of persistence:
// write, close the file, reopen, the data is still there. Catches any
// "I forgot to call Update / I held the value in a Go map" regression
// that an in-memory test would never notice.
func TestBoltVault_SurvivesRestart(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "anonde.db")
	key := testKey(t)

	// Open, write, close.
	{
		db, err := OpenDB(path)
		if err != nil {
			t.Fatalf("OpenDB: %v", err)
		}
		v, err := NewBoltVault(db, 0, key)
		if err != nil {
			t.Fatalf("NewBoltVault: %v", err)
		}
		if err := v.Put(context.Background(), "demo", core.VaultEntry{
			Token:      "<PERSON_DEMO_000001>",
			EntityType: "PERSON",
			Cleartext:  "Anna Schmidt",
		}); err != nil {
			t.Fatalf("Put: %v", err)
		}
		_ = v.Close()
		_ = db.Close()
	}

	// Reopen, read.
	db, err := OpenDB(path)
	if err != nil {
		t.Fatalf("re-OpenDB: %v", err)
	}
	defer db.Close()
	v, err := NewBoltVault(db, 0, key)
	if err != nil {
		t.Fatalf("re-NewBoltVault: %v", err)
	}
	defer v.Close()
	got, err := v.Get(context.Background(), "demo", "<PERSON_DEMO_000001>")
	if err != nil {
		t.Fatalf("Get after restart: %v", err)
	}
	if got.Cleartext != "Anna Schmidt" {
		t.Fatalf("got %q, want %q", got.Cleartext, "Anna Schmidt")
	}
}

// ─── Encryption sanity: raw bytes on disk must not contain cleartext ─

// TestBoltVault_OnDiskCiphertext is the privacy promise of the vault:
// even with the file in hand, an attacker without the key cannot read
// PII. We assert by writing a unique sentinel cleartext and then
// scanning every byte of the file for it.
func TestBoltVault_OnDiskCiphertext(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "anonde.db")
	const sentinel = "supercalifragilistic-PII-sentinel-XYZ"

	db, err := OpenDB(path)
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	v, err := NewBoltVault(db, 0, testKey(t))
	if err != nil {
		t.Fatalf("NewBoltVault: %v", err)
	}
	if err := v.Put(context.Background(), "demo", core.VaultEntry{
		Token:      "<PERSON_DEMO_000001>",
		EntityType: "PERSON",
		Cleartext:  sentinel,
	}); err != nil {
		t.Fatalf("Put: %v", err)
	}
	// Force fsync by closing; bbolt's Sync option also works but
	// closing is the most-realistic flush path.
	_ = v.Close()
	_ = db.Close()

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if bytes.Contains(raw, []byte(sentinel)) {
		t.Fatalf("cleartext sentinel found in on-disk bytes; vault encryption failed")
	}
	// The token name (a placeholder) is NOT secret; we don't assert
	// either way. The entity_type may or may not appear depending on
	// JSON formatting; not under test.
}

// ─── Wrong-key tampering: Open() rejects ───────────────────────────

func TestBoltVault_WrongKey(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "anonde.db")

	// Write with key A.
	db, _ := OpenDB(path)
	good := bytes.Repeat([]byte{0x01}, 32)
	v1, _ := NewBoltVault(db, 0, good)
	_ = v1.Put(context.Background(), "demo", core.VaultEntry{Token: "T", EntityType: "PERSON", Cleartext: "secret"})
	_ = v1.Close()
	_ = db.Close()

	// Reopen with key B → Get must fail at AEAD authentication.
	db2, _ := OpenDB(path)
	bad := bytes.Repeat([]byte{0x02}, 32)
	v2, _ := NewBoltVault(db2, 0, bad)
	defer v2.Close()
	defer db2.Close()
	if _, err := v2.Get(context.Background(), "demo", "T"); err == nil {
		t.Fatalf("expected decrypt error with wrong key, got nil")
	}
}

// ─── Opt-in encryption: nil key → plaintext on disk ───────────────

// TestBoltVault_NilKeyPlaintext locks in the opt-in encryption contract:
// constructed with a nil key, the vault round-trips values correctly AND
// writes them as plaintext JSON inside the envelope. If someone re-tightens
// NewBoltVault to reject nil keys, this test fails; which is the point,
// because the cmd/anonde wiring depends on the unencrypted path.
//
// We assert "plaintext on disk" by reopening the bbolt file with a raw
// bolt cursor, parsing the envelope, and confirming envelope.Body is
// directly valid VaultEntry JSON; the AEAD path would leave envelope.Body
// as nonce||ciphertext, which would fail json.Unmarshal.
func TestBoltVault_NilKeyPlaintext(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "anonde.db")
	const sentinel = "plaintext-PII-sentinel-ABC"

	db, err := OpenDB(path)
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	v, err := NewBoltVault(db, 0, nil)
	if err != nil {
		t.Fatalf("NewBoltVault(nil key): %v", err)
	}
	entry := core.VaultEntry{
		Token:      "<PERSON_DEMO_000001>",
		EntityType: "PERSON",
		Cleartext:  sentinel,
	}
	if err := v.Put(context.Background(), "demo", entry); err != nil {
		t.Fatalf("Put: %v", err)
	}
	got, err := v.Get(context.Background(), "demo", entry.Token)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != entry {
		t.Fatalf("roundtrip mismatch: got %+v, want %+v", got, entry)
	}
	_ = v.Close()
	_ = db.Close()

	// Reopen with a fresh handle and inspect the raw envelope.
	db2, err := OpenDB(path)
	if err != nil {
		t.Fatalf("re-OpenDB: %v", err)
	}
	defer db2.Close()
	var body []byte
	err = db2.View(func(tx *bolt.Tx) error {
		c := tx.Bucket([]byte(bucketVault)).Cursor()
		_, raw := c.First()
		if raw == nil {
			t.Fatalf("vault bucket empty")
		}
		var env envelope
		if err := json.Unmarshal(raw, &env); err != nil {
			t.Fatalf("unmarshal envelope: %v", err)
		}
		body = env.Body
		return nil
	})
	if err != nil {
		t.Fatalf("View: %v", err)
	}
	var disk core.VaultEntry
	if err := json.Unmarshal(body, &disk); err != nil {
		t.Fatalf("envelope.Body is not plaintext JSON (looks encrypted): %v", err)
	}
	if disk.Cleartext != sentinel {
		t.Fatalf("on-disk cleartext = %q, want %q", disk.Cleartext, sentinel)
	}
}

// TestBoltVault_EmptyKeyPlaintext mirrors the nil-key contract for the
// `len(key) == 0` slice case, since the main.go wiring may pass either
// shape depending on how ANONDE_VAULT_KEY is parsed.
func TestBoltVault_EmptyKeyPlaintext(t *testing.T) {
	db, err := OpenDB(filepath.Join(t.TempDir(), "anonde.db"))
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	defer db.Close()
	v, err := NewBoltVault(db, 0, []byte{})
	if err != nil {
		t.Fatalf("NewBoltVault(empty key): %v", err)
	}
	defer v.Close()
	entry := core.VaultEntry{Token: "T", EntityType: "PERSON", Cleartext: "alice"}
	if err := v.Put(context.Background(), "demo", entry); err != nil {
		t.Fatalf("Put: %v", err)
	}
	got, err := v.Get(context.Background(), "demo", "T")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != entry {
		t.Fatalf("roundtrip mismatch: got %+v, want %+v", got, entry)
	}
}

// ─── Vault key loader ──────────────────────────────────────────────

func TestLoadVaultKey(t *testing.T) {
	// Save/restore env so tests don't leak.
	prev, hadPrev := os.LookupEnv("ANONDE_VAULT_KEY")
	t.Cleanup(func() {
		if hadPrev {
			_ = os.Setenv("ANONDE_VAULT_KEY", prev)
		} else {
			_ = os.Unsetenv("ANONDE_VAULT_KEY")
		}
	})

	t.Run("missing", func(t *testing.T) {
		_ = os.Unsetenv("ANONDE_VAULT_KEY")
		if _, err := LoadVaultKey(); err == nil {
			t.Fatalf("expected error when env var unset")
		}
	})

	t.Run("not base64", func(t *testing.T) {
		_ = os.Setenv("ANONDE_VAULT_KEY", "not-base64!")
		if _, err := LoadVaultKey(); err == nil {
			t.Fatalf("expected error on invalid base64")
		}
	})

	t.Run("wrong length", func(t *testing.T) {
		// 16 bytes (AES-128); we require AES-256.
		_ = os.Setenv("ANONDE_VAULT_KEY", base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{1}, 16)))
		if _, err := LoadVaultKey(); err == nil {
			t.Fatalf("expected error on wrong length")
		}
	})

	t.Run("valid", func(t *testing.T) {
		want := bytes.Repeat([]byte{0xab}, 32)
		_ = os.Setenv("ANONDE_VAULT_KEY", base64.StdEncoding.EncodeToString(want))
		got, err := LoadVaultKey()
		if err != nil {
			t.Fatalf("unexpected: %v", err)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("key mismatch")
		}
	})
}
