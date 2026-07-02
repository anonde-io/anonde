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
// invisible; the bottleneck is fsync on commit, not lock contention.
//
// Why NUL as the tenant/key separator: tenant IDs and tokens are
// human-controlled but never legitimately contain a NUL byte, so the
// composite-key collisions that bite when separators are "." or ":"
// (e.g. tenant "a:b" + token "c" colliding with tenant "a" + token
// "b:c") can't happen here.
//
// Why JSON for the payload: small fields, schema-stable across the
// project's life, debuggable with `bbolt buckets / get` plus `jq`.
// We considered protobuf; saves bytes, but we lose ad-hoc inspection
// and add a build-time codegen dependency on the store package. The
// vault payload is tiny (token + cleartext + entity_type, usually
// well under 200 bytes); the size win is theoretical.
//
// TTL handling: same shape as MemoryVault; every value carries an
// ExpiresAt, Get returns "not found" for expired rows, a background
// sweeper periodically deletes them so disk doesn't grow forever.
// Expiry is stored alongside the value (not as a separate index)
// because cleanup is a full-scan sweep and the keys themselves give
// us no ordering hint.

const (
	bucketVault = "vault"
	bucketStore = "store"
)

// envelope is the on-disk record shape. The same struct serves both
// buckets; the difference is that vault Body is encrypted bytes
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
// plaintext JSON; operators opt into encryption by setting
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
// File mode 0600: only the running user can read the vault; matters
// when the host is shared (multi-tenant VM or dev box).
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

// Close stops the background sweeper and waits for any in-flight
// sweep tick to settle. Idempotent. Does NOT close the underlying
// *bolt.DB; the caller (cmd/anonde/main.go) owns its lifecycle since
// two adapters share one file.
//
// The sweepMu lock-after-stop is what gives the caller a real
// guarantee: when Close returns, the sweeper goroutine is either
// exited or no longer touching db. Without it, a sweep transaction
// could still be running against db when the caller tries to close
// the file, panicking inside bbolt.
func (v *BoltVault) Close() error {
	v.stopOnce.Do(func() { close(v.stopCh) })
	v.sweepMu.Lock()
	v.sweepMu.Unlock() //nolint:staticcheck // wait for in-flight tick
	return nil
}
func (s *BoltStore) Close() error {
	s.stopOnce.Do(func() { close(s.stopCh) })
	s.sweepMu.Lock()
	s.sweepMu.Unlock() //nolint:staticcheck // wait for in-flight tick
	return nil
}

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

// liveEntryCount reports the number of non-expired rows in a bucket and
// returns Bytes=-1 (see the note below).
//
// Fast path — expiry disabled (ttl <= 0): no row can ever be expired
// (expirationFromNow returns the zero time, which expired() never treats as
// past), so bbolt's KeyN from Bucket.Stats() IS the exact live count. KeyN is
// derived from page metadata — O(pages), and crucially it decodes NO row — so
// a persistent, no-TTL deployment no longer pays a per-row json.Unmarshal on
// every /metrics scrape.
//
// Slow path — a TTL is set: we honor the "Stats drops expired-but-unswept
// rows" contract (a deliberate choice so the gauge matches what Get would
// return, not what physically sits on disk ahead of the sweeper), which
// requires reading each row's expiry. That per-row scan is inherent to the
// exclude-expired semantics and is kept here.
//
// Bytes=-1 because computing the real byte total would require a full bucket
// walk decoding every payload on every scrape; a non-starter on a multi-MB
// vault. Operators who want a byte signal can look at the file size on disk,
// which bbolt grows in page-sized increments.
func liveEntryCount(db *bolt.DB, bucket []byte, ttl time.Duration) int64 {
	if ttl <= 0 {
		return bucketKeyN(db, bucket)
	}
	return countLiveEntries(db, bucket)
}

// bucketKeyN returns bbolt's KeyN for a bucket without decoding any row.
func bucketKeyN(db *bolt.DB, bucket []byte) int64 {
	var n int64
	_ = db.View(func(tx *bolt.Tx) error {
		if b := tx.Bucket(bucket); b != nil {
			n = int64(b.Stats().KeyN)
		}
		return nil
	})
	return n
}

// countLiveEntries counts the bucket's entries whose envelope has not
// expired, matching what Get would actually return. bolt's KeyN would
// over-count rows the background sweeper hasn't reclaimed yet, so we
// decode just the (plaintext, key-independent) expiry field per row.
// Read-only by design: unlike the memory backend's sweep-on-Stats, this
// does not mutate — disk reclamation stays the background sweeper's job.
func countLiveEntries(db *bolt.DB, bucket []byte) int64 {
	var n int64
	_ = db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucket)
		if b == nil {
			return nil
		}
		return b.ForEach(func(_, val []byte) error {
			var meta struct {
				ExpiresAt time.Time `json:"exp"`
			}
			// Undecodable rows are counted (they exist on disk and the
			// sweeper leaves them too); only cleanly-expired rows drop.
			if err := json.Unmarshal(val, &meta); err != nil || !expired(meta.ExpiresAt) {
				n++
			}
			return nil
		})
	})
	return n
}

func (v *BoltVault) Stats() core.VaultStats {
	return core.VaultStats{Entries: liveEntryCount(v.db, []byte(bucketVault), v.ttl), Bytes: -1}
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

// Stats; see liveEntryCount for the fast/slow-path rationale and Bytes=-1.
func (s *BoltStore) Stats() core.StoreStats {
	return core.StoreStats{Entries: liveEntryCount(s.db, []byte(bucketStore), s.ttl), Bytes: -1}
}

// ─── Sweeper ───────────────────────────────────────────────────────

// startSweeperLocked launches the background sweeper in its own
// goroutine. The shape mirrors MemoryVault/Store's behaviour; we run
// a sweep at most once per sweepInterval and skip entirely when ttl=0.
func (v *BoltVault) startSweeperLocked() {
	if v.sweepInterval <= 0 {
		return
	}
	go runSweeper(v.stopCh, v.sweepInterval, func() {
		// Hold sweepMu so Close() can synchronously wait for an
		// in-flight tick to finish before letting its caller close
		// the underlying *bolt.DB.
		v.sweepMu.Lock()
		defer v.sweepMu.Unlock()
		_ = sweepBucket(v.db, []byte(bucketVault))
	})
}

func (s *BoltStore) startSweeperLocked() {
	if s.sweepInterval <= 0 {
		return
	}
	go runSweeper(s.stopCh, s.sweepInterval, func() {
		s.sweepMu.Lock()
		defer s.sweepMu.Unlock()
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

// sweepBatchSize bounds how many rows a single sweep transaction examines.
// Sweeping in batches keeps the write lock short and the collected-key slice
// small even against a large expired backlog, instead of holding one write
// transaction (and one unbounded slice) across the entire bucket.
const sweepBatchSize = 1024

// sweepBucket deletes every envelope whose ExpiresAt is in the past, in
// bounded write-transaction batches. Each batch examines at most
// sweepBatchSize rows, deletes the expired ones it found, commits, then
// resumes the scan just past the last row it looked at (Cursor.Seek), so no
// row is decoded twice and the write lock is released between batches. When a
// batch examines fewer than sweepBatchSize rows the bucket has been fully
// scanned and the sweep is done.
//
// Unlike the old single-transaction sweep this is not atomic across the whole
// bucket, but that is safe: Get already treats an expired row as not-found
// regardless of whether the sweeper has physically deleted it yet, so a reader
// can never observe an expired row as live mid-sweep.
func sweepBucket(db *bolt.DB, bucket []byte) error {
	var resumeFrom []byte // nil first batch → start at the beginning
	for {
		var (
			lastKey  []byte
			examined int
		)
		err := db.Update(func(tx *bolt.Tx) error {
			b := tx.Bucket(bucket)
			if b == nil {
				return nil
			}
			c := b.Cursor()
			var k, v []byte
			if resumeFrom == nil {
				k, v = c.First()
			} else {
				k, v = c.Seek(resumeFrom)
			}
			toDelete := make([][]byte, 0, 16)
			for ; k != nil && examined < sweepBatchSize; k, v = c.Next() {
				examined++
				lastKey = append(lastKey[:0], k...) // resume point (own memory)
				var env envelope
				if json.Unmarshal(v, &env) != nil {
					continue
				}
				if expired(env.ExpiresAt) {
					toDelete = append(toDelete, append([]byte(nil), k...))
				}
			}
			for _, dk := range toDelete {
				if err := b.Delete(dk); err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil {
			return err
		}
		// Fewer than a full batch examined → reached the end of the bucket.
		if examined < sweepBatchSize {
			return nil
		}
		// Resume strictly after the last examined row: appending 0x00 yields
		// the smallest key sorting after lastKey, so Seek skips it.
		resumeFrom = append(append([]byte(nil), lastKey...), 0x00)
	}
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

// ErrBucketMissing; kept as a sentinel for future callers if we ever
// expose lower-level access to the buckets. Not currently surfaced.
var ErrBucketMissing = errors.New("bbolt bucket missing")
