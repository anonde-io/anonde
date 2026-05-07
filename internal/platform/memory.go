package platform

import (
	"context"
	"fmt"
	"sync"
	"time"
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

func (v *MemoryVault) Put(_ context.Context, tenantID string, entry VaultEntry) error {
	v.mu.Lock()
	v.m[tenantID+":"+entry.Token] = vaultEntry{
		Value:     entry,
		ExpiresAt: expirationFromNow(v.ttl),
	}
	v.sweepExpiredIfDueLocked()
	v.mu.Unlock()
	return nil
}

func (v *MemoryVault) Get(_ context.Context, tenantID, token string) (VaultEntry, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	entry, ok := v.m[tenantID+":"+token]
	if !ok {
		return VaultEntry{}, fmt.Errorf("token %q not found for tenant %q", token, tenantID)
	}
	if entry.expiredAt(time.Now()) {
		delete(v.m, tenantID+":"+token)
		return VaultEntry{}, fmt.Errorf("token %q not found for tenant %q", token, tenantID)
	}
	return entry.Value, nil
}

// MemoryStore is an in-process anonymized document store. Not persistent across restarts.
type MemoryStore struct {
	mu            sync.Mutex
	m             map[string]storeEntry // key: tenantID+":"+docID
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

func (s *MemoryStore) Put(_ context.Context, record StoreRecord) error {
	s.mu.Lock()
	s.m[record.TenantID+":"+record.DocID] = storeEntry{
		Value:     record,
		ExpiresAt: expirationFromNow(s.ttl),
	}
	s.sweepExpiredIfDueLocked()
	s.mu.Unlock()
	return nil
}

func (s *MemoryStore) Get(_ context.Context, tenantID, docID string) (StoreRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.m[tenantID+":"+docID]
	if !ok {
		return StoreRecord{}, fmt.Errorf("document %q not found for tenant %q", docID, tenantID)
	}
	if rec.expiredAt(time.Now()) {
		delete(s.m, tenantID+":"+docID)
		return StoreRecord{}, fmt.Errorf("document %q not found for tenant %q", docID, tenantID)
	}
	return rec.Value, nil
}

type vaultEntry struct {
	Value     VaultEntry
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
	Value     StoreRecord
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

