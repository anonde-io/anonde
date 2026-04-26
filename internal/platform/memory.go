package platform

import (
	"context"
	"fmt"
	"sync"
)

// MemoryVault is an in-process token → cleartext store. Not persistent across restarts.
type MemoryVault struct {
	mu sync.RWMutex
	m  map[string]VaultEntry // key: tenantID+":"+token
}

func NewMemoryVault() *MemoryVault {
	return &MemoryVault{m: make(map[string]VaultEntry)}
}

func (v *MemoryVault) Put(_ context.Context, tenantID string, entry VaultEntry) error {
	v.mu.Lock()
	v.m[tenantID+":"+entry.Token] = entry
	v.mu.Unlock()
	return nil
}

func (v *MemoryVault) Get(_ context.Context, tenantID, token string) (VaultEntry, error) {
	v.mu.RLock()
	entry, ok := v.m[tenantID+":"+token]
	v.mu.RUnlock()
	if !ok {
		return VaultEntry{}, fmt.Errorf("token %q not found for tenant %q", token, tenantID)
	}
	return entry, nil
}

// MemoryStore is an in-process anonymized document store. Not persistent across restarts.
type MemoryStore struct {
	mu sync.RWMutex
	m  map[string]StoreRecord // key: tenantID+":"+docID
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{m: make(map[string]StoreRecord)}
}

func (s *MemoryStore) Put(_ context.Context, record StoreRecord) error {
	s.mu.Lock()
	s.m[record.TenantID+":"+record.DocID] = record
	s.mu.Unlock()
	return nil
}

func (s *MemoryStore) Get(_ context.Context, tenantID, docID string) (StoreRecord, error) {
	s.mu.RLock()
	rec, ok := s.m[tenantID+":"+docID]
	s.mu.RUnlock()
	if !ok {
		return StoreRecord{}, fmt.Errorf("document %q not found for tenant %q", docID, tenantID)
	}
	return rec, nil
}
