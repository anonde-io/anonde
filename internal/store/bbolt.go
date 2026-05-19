package store

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	bolt "go.etcd.io/bbolt"

	"github.com/anonde-io/anonde/internal/core"
)

// bbolt-backed implementations of core.Vault and core.Store.
//
// Layout on disk:
//
//	one file at the path the operator provided, two top-level buckets:
//	  - "vault" : key = tenantID + 0x00 + token       → AEAD(JSON(VaultEntry))+meta
//	  - "store" : key = tenantID + 0x00 + id          → JSON(StoreRecord)+meta
//
// Why a single file: trivially backup-able (`cp anonde.db`), atomic
// crash semantics from bbolt's 2-page meta swap, and one fd at runtime.
// The two buckets share the same writer lock, but at our RPS that's
// invisible — the bottleneck is fsync on commit, not lock contention.
//
// Why NUL as the tenant/key separator: tenant IDs and tokens are
// human-controlled but never legitimately contain a NUL byte, so the
// composite-key collisions that bite when separators are "." or ":"
// (e.g. tenant "a:b" + token "c" colliding with tenant "a" + token
// "b:c") can't happen here.
//
// Why JSON for the payload: small fields, schema-stable across the
// project's life, debuggable with `bbolt buckets / get` plus `jq`.
// We considered protobuf — saves bytes, but we lose ad-hoc inspection
// and add a build-time codegen dependency on the store package. The
// vault payload is tiny (token + cleartext + entity_type, usually
// well under 200 bytes); the size win is theoretical.
//
// TTL handling: same shape as MemoryVault — every value carries an
// ExpiresAt; Get returns "not found" for expired rows; a background
// sweeper periodically deletes them so disk doesn't grow forever.
// Expiry is stored alongside the value (not as a separate index)
// because cleanup is a full-scan sweep and the keys themselves give
// us no ordering hint.

const (
	bucketVault = "vault"
	bucketStore = "store"
)

// envelope is the on-disk record shape. The same struct serves both
// buckets — the difference is that vault Body is encrypted bytes
// while store Body is plaintext JSON. Keeping the format identical
// makes the sweeper schema-blind.
type envelope struct {
	Version   uint8     `json:"v"`
	ExpiresAt time.Time `json:"exp,omitzero"`
	Body      []byte    `json:"body"`
}

const envelopeVersion uint8 = 1

// BoltStore is the persistent core.Store implementation.
type BoltStore struct {
	db            *bolt.DB
	ttl           time.Duration
	sweepInterval time.Duration

	sweepMu  sync.Mutex
	stopCh   chan struct{}
	stopOnce sync.Once
}

// BoltVault is the persistent core.Vault implementation. When constructed
// with a key, values are AES-256-GCM sealed and the key never lands on
// disk. When constructed with a nil/empty key, values are written as
// plaintext JSON — operators opt into encryption by setting
// ANONDE_VAULT_KEY.
type BoltVault struct {
	db            *bolt.DB
	aead          *aeadCipher // nil → encryption disabled
	ttl           time.Duration
	sweepInterval time.Duration

	sweepMu  sync.Mutex
	stopCh   chan struct{}
	stopOnce sync.Once
}

// OpenDB opens (or creates) the on-disk bbolt file and ensures both
// top-level buckets exist. Returns the *bolt.DB so callers can wire
// both BoltVault and BoltStore against the same file. The caller owns
// `Close()` and should defer it.
//
// File mode 0600: only the running user can read the vault — matters
// when the host is shared (Fly machine, multi-tenant dev box).
func OpenDB(path string) (*bolt.DB, error) {
	db, err := bolt.Open(path, 0o600, &bolt.Options{
		Timeout: 2 * time.Second, // give up rather than hang on a stuck lock
	})
	if err != nil {
		return nil, fmt.Errorf("bbolt open %q: %w", path, err)
	}
	err = db.Update(func(tx *bolt.Tx) error {
		for _, name := range []string{bucketVault, bucketStore} {
			if _, err := tx.CreateBucketIfNotExists([]byte(name)); err != nil {
				return fmt.Errorf("create bucket %q: %w", name, err)
			}
		}
		return nil
	})
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

// NewBoltVault returns a Vault backed by the given *bolt.DB. Pass a
// 32-byte key (use LoadVaultKey to read from env) to enable AES-256-GCM
// at-rest encryption, or nil/empty to store values as plaintext JSON.
// ttl=0 disables expiry. Closes the sweeper when Close() is called.
func NewBoltVault(db *bolt.DB, ttl time.Duration, key []byte) (*BoltVault, error) {
	var aead *aeadCipher
	if len(key) > 0 {
		var err error
		aead, err = newAEAD(key)
		if err != nil {
			return nil, err
		}
	}
	v := &BoltVault{
		db:            db,
		aead:          aead,
		ttl:           ttl,
		sweepInterval: computeSweepInterval(ttl),
		stopCh:        make(chan struct{}),
	}
	v.startSweeperLocked()
	return v, nil
}

// NewBoltStore returns a Store backed by the given *bolt.DB.
func NewBoltStore(db *bolt.DB, ttl time.Duration) *BoltStore {
	s := &BoltStore{
		db:            db,
		ttl:           ttl,
		sweepInterval: computeSweepInterval(ttl),
		stopCh:        make(chan struct{}),
	}
	s.startSweeperLocked()
	return s
}

// Close stops the background sweeper. Idempotent. Does NOT close the
// underlying *bolt.DB — the caller (cmd/anonde/main.go) owns its
// lifecycle since two adapters share one file.
func (v *BoltVault) Close() error { v.stopOnce.Do(func() { close(v.stopCh) }); return nil }
func (s *BoltStore) Close() error { s.stopOnce.Do(func() { close(s.stopCh) }); return nil }

// ─── BoltVault implements core.Vault ───────────────────────────────

func (v *BoltVault) Put(_ context.Context, tenantID string, entry core.VaultEntry) error {
	payload, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("marshal vault entry: %w", err)
	}
	body := payload
	if v.aead != nil {
		body, err = v.aead.seal(payload)
		if err != nil {
			return fmt.Errorf("seal vault entry: %w", err)
		}
	}
	env := envelope{
		Version:   envelopeVersion,
		ExpiresAt: expirationFromNow(v.ttl),
		Body:      body,
	}
	encoded, err := json.Marshal(env)
	if err != nil {
		return fmt.Errorf("marshal vault envelope: %w", err)
	}
	return v.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket([]byte(bucketVault)).Put(vaultKey(tenantID, entry.Token), encoded)
	})
}

func (v *BoltVault) Get(_ context.Context, tenantID, token string) (core.VaultEntry, error) {
	var (
		env   envelope
		found bool
	)
	err := v.db.View(func(tx *bolt.Tx) error {
		raw := tx.Bucket([]byte(bucketVault)).Get(vaultKey(tenantID, token))
		if raw == nil {
			return nil
		}
		found = true
		return json.Unmarshal(raw, &env)
	})
	if err != nil {
		return core.VaultEntry{}, fmt.Errorf("read vault: %w", err)
	}
	if !found || expired(env.ExpiresAt) {
		// Lazy cleanup: if it's expired, drop it on the floor. The
		// sweeper will reclaim disk later. We keep the "not found"
		// response identical so callers can't observe the difference
		// between "never existed" and "expired".
		if found && expired(env.ExpiresAt) {
			_ = v.deleteRaw(tenantID, token)
		}
		return core.VaultEntry{}, fmt.Errorf("token %q not found for tenant %q", token, tenantID)
	}
	plaintext := env.Body
	if v.aead != nil {
		var err error
		plaintext, err = v.aead.open(env.Body)
		if err != nil {
			return core.VaultEntry{}, fmt.Errorf("decrypt vault entry: %w", err)
		}
	}
	var out core.VaultEntry
	if err := json.Unmarshal(plaintext, &out); err != nil {
		return core.VaultEntry{}, fmt.Errorf("unmarshal vault entry: %w", err)
	}
	return out, nil
}

func (v *BoltVault) Delete(_ context.Context, tenantID, token string) error {
	return v.deleteRaw(tenantID, token)
}

func (v *BoltVault) deleteRaw(tenantID, token string) error {
	return v.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket([]byte(bucketVault)).Delete(vaultKey(tenantID, token))
	})
}

// ─── BoltStore implements core.Store ───────────────────────────────

func (s *BoltStore) Put(_ context.Context, record core.StoreRecord) error {
	payload, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("marshal store record: %w", err)
	}
	env := envelope{
		Version:   envelopeVersion,
		ExpiresAt: expirationFromNow(s.ttl),
		Body:      payload,
	}
	encoded, err := json.Marshal(env)
	if err != nil {
		return fmt.Errorf("marshal store envelope: %w", err)
	}
	return s.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket([]byte(bucketStore)).Put(storeKey(record.TenantID, record.ID), encoded)
	})
}

func (s *BoltStore) Get(_ context.Context, tenantID, id string) (core.StoreRecord, error) {
	var (
		env   envelope
		found bool
	)
	err := s.db.View(func(tx *bolt.Tx) error {
		raw := tx.Bucket([]byte(bucketStore)).Get(storeKey(tenantID, id))
		if raw == nil {
			return nil
		}
		found = true
		return json.Unmarshal(raw, &env)
	})
	if err != nil {
		return core.StoreRecord{}, fmt.Errorf("read store: %w", err)
	}
	if !found || expired(env.ExpiresAt) {
		if found && expired(env.ExpiresAt) {
			_ = s.deleteRaw(tenantID, id)
		}
		return core.StoreRecord{}, fmt.Errorf("anonymization %q not found for tenant %q", id, tenantID)
	}
	var out core.StoreRecord
	if err := json.Unmarshal(env.Body, &out); err != nil {
		return core.StoreRecord{}, fmt.Errorf("unmarshal store record: %w", err)
	}
	return out, nil
}

func (s *BoltStore) Delete(_ context.Context, tenantID, id string) (bool, error) {
	existed := false
	err := s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(bucketStore))
		k := storeKey(tenantID, id)
		if raw := b.Get(k); raw != nil {
			var env envelope
			if json.Unmarshal(raw, &env) == nil {
				existed = !expired(env.ExpiresAt)
			}
		}
		return b.Delete(k)
	})
	return existed, err
}

func (s *BoltStore) deleteRaw(tenantID, id string) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket([]byte(bucketStore)).Delete(storeKey(tenantID, id))
	})
}

// ─── Sweeper ───────────────────────────────────────────────────────

// startSweeperLocked launches the background sweeper in its own
// goroutine. The shape mirrors MemoryVault/Store's behaviour — we run
// a sweep at most once per sweepInterval and skip entirely when ttl=0.
func (v *BoltVault) startSweeperLocked() {
	if v.sweepInterval <= 0 {
		return
	}
	go runSweeper(v.stopCh, v.sweepInterval, func() {
		_ = sweepBucket(v.db, []byte(bucketVault))
	})
}

func (s *BoltStore) startSweeperLocked() {
	if s.sweepInterval <= 0 {
		return
	}
	go runSweeper(s.stopCh, s.sweepInterval, func() {
		_ = sweepBucket(s.db, []byte(bucketStore))
	})
}

func runSweeper(stopCh <-chan struct{}, every time.Duration, do func()) {
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	for {
		select {
		case <-stopCh:
			return
		case <-ticker.C:
			do()
		}
	}
}

// sweepBucket scans a bucket and deletes envelopes whose ExpiresAt is
// in the past. Done in a single write transaction so concurrent
// readers see either the pre-sweep or post-sweep view consistently.
func sweepBucket(db *bolt.DB, bucket []byte) error {
	return db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucket)
		if b == nil {
			return nil
		}
		c := b.Cursor()
		// Collect keys to delete first; deleting via cursor mid-iteration
		// is allowed by bbolt but the docs caution about subtle pitfalls,
		// and our buckets are tiny so the extra slice is harmless.
		var toDelete [][]byte
		for k, v := c.First(); k != nil; k, v = c.Next() {
			var env envelope
			if json.Unmarshal(v, &env) != nil {
				continue
			}
			if expired(env.ExpiresAt) {
				keyCopy := make([]byte, len(k))
				copy(keyCopy, k)
				toDelete = append(toDelete, keyCopy)
			}
		}
		for _, k := range toDelete {
			if err := b.Delete(k); err != nil {
				return err
			}
		}
		return nil
	})
}

// ─── Helpers ───────────────────────────────────────────────────────

func vaultKey(tenantID, token string) []byte {
	return compositeKey(tenantID, token)
}

func storeKey(tenantID, id string) []byte {
	return compositeKey(tenantID, id)
}

// compositeKey builds tenant\x00field. The leading 2-byte length
// prefix on the tenant guards against ambiguity if someone ever stores
// a NUL byte in a tenant (proto3 strings are valid UTF-8 by spec, but
// belt-and-braces).
func compositeKey(tenantID, field string) []byte {
	var buf bytes.Buffer
	buf.Grow(2 + len(tenantID) + 1 + len(field))
	_ = binary.Write(&buf, binary.BigEndian, uint16(len(tenantID)))
	buf.WriteString(tenantID)
	buf.WriteByte(0x00)
	buf.WriteString(field)
	return buf.Bytes()
}

func expired(t time.Time) bool {
	return !t.IsZero() && time.Now().After(t)
}

// ensure the impls satisfy the interfaces at compile time.
var (
	_ core.Vault = (*BoltVault)(nil)
	_ core.Store = (*BoltStore)(nil)
)

// ErrBucketMissing — kept as a sentinel for future callers if we ever
// expose lower-level access to the buckets. Not currently surfaced.
var ErrBucketMissing = errors.New("bbolt bucket missing")
