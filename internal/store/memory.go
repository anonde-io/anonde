package store

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/anonde-io/anonde/internal/core"
)

// MemoryVault is an in-process token → cleartext store. Not persistent across restarts.
type MemoryVault struct {
	mu            sync.Mutex
	m             map[string]vaultEntry // key: tenantID+":"+token
	ttl           time.Duration
	lastSweep     time.Time
	sweepInterval time.Duration
}

func NewMemoryVault() *MemoryVault {
	return NewMemoryVaultWithTTL(0)
}

func NewMemoryVaultWithTTL(ttl time.Duration) *MemoryVault {
	return &MemoryVault{
		m:             make(map[string]vaultEntry),
		ttl:           ttl,
		sweepInterval: computeSweepInterval(ttl),
	}
}

func (v *MemoryVault) Put(_ context.Context, tenantID string, entry core.VaultEntry) error {
	v.mu.Lock()
	v.m[tenantID+":"+entry.Token] = vaultEntry{
		Value:     entry,
		ExpiresAt: expirationFromNow(v.ttl),
	}
	v.sweepExpiredIfDueLocked()
	v.mu.Unlock()
	return nil
}

func (v *MemoryVault) Get(_ context.Context, tenantID, token string) (core.VaultEntry, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	entry, ok := v.m[tenantID+":"+token]
	if !ok {
		return core.VaultEntry{}, fmt.Errorf("token %q not found for tenant %q", token, tenantID)
	}
	if entry.expiredAt(time.Now()) {
		delete(v.m, tenantID+":"+token)
		return core.VaultEntry{}, fmt.Errorf("token %q not found for tenant %q", token, tenantID)
	}
	return entry.Value, nil
}

func (v *MemoryVault) Delete(_ context.Context, tenantID, token string) error {
	v.mu.Lock()
	delete(v.m, tenantID+":"+token)
	v.mu.Unlock()
	return nil
}

// Stats walks the live entry map under the mutex. The map is small
// in practice (the in-memory vault is reset every restart, so it
// never accumulates indefinitely) and computing both entries and
// bytes in one pass is cheap enough to run on every scrape — the
// alternative of maintaining live counters would add a write under
// every Put/Delete just to feed metrics, which isn't worth it.
func (v *MemoryVault) Stats() core.VaultStats {
	v.mu.Lock()
	defer v.mu.Unlock()
	var bytes int64
	for _, e := range v.m {
		bytes += int64(len(e.Value.Token) + len(e.Value.Cleartext) + len(e.Value.EntityType))
	}
	return core.VaultStats{Entries: int64(len(v.m)), Bytes: bytes}
}

// MemoryStore is an in-process anonymization store. Not persistent across restarts.
type MemoryStore struct {
	mu            sync.Mutex
	m             map[string]storeEntry // key: tenantID+":"+id
	ttl           time.Duration
	lastSweep     time.Time
	sweepInterval time.Duration
}

func NewMemoryStore() *MemoryStore {
	return NewMemoryStoreWithTTL(0)
}

func NewMemoryStoreWithTTL(ttl time.Duration) *MemoryStore {
	return &MemoryStore{
		m:             make(map[string]storeEntry),
		ttl:           ttl,
		sweepInterval: computeSweepInterval(ttl),
	}
}

func (s *MemoryStore) Put(_ context.Context, record core.StoreRecord) error {
	s.mu.Lock()
	s.m[record.TenantID+":"+record.ID] = storeEntry{
		Value:     record,
		ExpiresAt: expirationFromNow(s.ttl),
	}
	s.sweepExpiredIfDueLocked()
	s.mu.Unlock()
	return nil
}

func (s *MemoryStore) Get(_ context.Context, tenantID, id string) (core.StoreRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.m[tenantID+":"+id]
	if !ok {
		return core.StoreRecord{}, fmt.Errorf("anonymization %q not found for tenant %q", id, tenantID)
	}
	if rec.expiredAt(time.Now()) {
		delete(s.m, tenantID+":"+id)
		return core.StoreRecord{}, fmt.Errorf("anonymization %q not found for tenant %q", id, tenantID)
	}
	return rec.Value, nil
}

func (s *MemoryStore) Delete(_ context.Context, tenantID, id string) (bool, error) {
	key := tenantID + ":" + id
	s.mu.Lock()
	rec, ok := s.m[key]
	if ok {
		delete(s.m, key)
	}
	s.mu.Unlock()
	if !ok {
		return false, nil
	}
	// A record present-but-expired counts as "didn't exist" for the
	// caller; that lines up with Get's behavior.
	return !rec.expiredAt(time.Now()), nil
}

// Stats walks the records under the mutex. AnonymizedContent
// dominates the byte count — the token slice is small enough that we
// ignore it here to keep the loop cheap. Approximation, not audit.
func (s *MemoryStore) Stats() core.StoreStats {
	s.mu.Lock()
	defer s.mu.Unlock()
	var bytes int64
	for _, e := range s.m {
		bytes += int64(len(e.Value.AnonymizedContent))
	}
	return core.StoreStats{Entries: int64(len(s.m)), Bytes: bytes}
}

type vaultEntry struct {
	Value     core.VaultEntry
	ExpiresAt time.Time
}

func (e vaultEntry) expiredAt(now time.Time) bool {
	return !e.ExpiresAt.IsZero() && now.After(e.ExpiresAt)
}

func (v *MemoryVault) sweepExpiredIfDueLocked() {
	if v.sweepInterval <= 0 {
		return
	}
	now := time.Now()
	if !v.lastSweep.IsZero() && now.Sub(v.lastSweep) < v.sweepInterval {
		return
	}
	v.lastSweep = now
	for key, entry := range v.m {
		if entry.expiredAt(now) {
			delete(v.m, key)
		}
	}
}

type storeEntry struct {
	Value     core.StoreRecord
	ExpiresAt time.Time
}

func (e storeEntry) expiredAt(now time.Time) bool {
	return !e.ExpiresAt.IsZero() && now.After(e.ExpiresAt)
}

func (s *MemoryStore) sweepExpiredIfDueLocked() {
	if s.sweepInterval <= 0 {
		return
	}
	now := time.Now()
	if !s.lastSweep.IsZero() && now.Sub(s.lastSweep) < s.sweepInterval {
		return
	}
	s.lastSweep = now
	for key, entry := range s.m {
		if entry.expiredAt(now) {
			delete(s.m, key)
		}
	}
}

func expirationFromNow(ttl time.Duration) time.Time {
	if ttl <= 0 {
		return time.Time{}
	}
	return time.Now().Add(ttl)
}

func computeSweepInterval(ttl time.Duration) time.Duration {
	if ttl <= 0 {
		return 0
	}
	if ttl <= time.Minute {
		return ttl
	}
	return ttl / 2
}

