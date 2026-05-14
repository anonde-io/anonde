package core

import (
	"context"
	"fmt"
	"sync"
)

// Minimal in-test Vault/Store impls so core's tests don't pull in the
// store package (which would create import cycles during the rolling
// platform→{api,core,store} split). Production code uses
// internal/store/memory.go; these stubs only need to be correct enough
// to drive Service end-to-end.

type testVault struct {
	mu sync.Mutex
	m  map[string]VaultEntry
}

func newTestVault() *testVault { return &testVault{m: map[string]VaultEntry{}} }

func (v *testVault) Put(_ context.Context, tenantID string, e VaultEntry) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.m[tenantID+":"+e.Token] = e
	return nil
}

func (v *testVault) Get(_ context.Context, tenantID, token string) (VaultEntry, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	e, ok := v.m[tenantID+":"+token]
	if !ok {
		return VaultEntry{}, fmt.Errorf("token %q not found for tenant %q", token, tenantID)
	}
	return e, nil
}

func (v *testVault) Delete(_ context.Context, tenantID, token string) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	delete(v.m, tenantID+":"+token)
	return nil
}

type testStore struct {
	mu sync.Mutex
	m  map[string]StoreRecord
}

func newTestStore() *testStore { return &testStore{m: map[string]StoreRecord{}} }

func (s *testStore) Put(_ context.Context, r StoreRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[r.TenantID+":"+r.ID] = r
	return nil
}

func (s *testStore) Get(_ context.Context, tenantID, id string) (StoreRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.m[tenantID+":"+id]
	if !ok {
		return StoreRecord{}, fmt.Errorf("anonymization %q not found for tenant %q", id, tenantID)
	}
	return r, nil
}

func (s *testStore) Delete(_ context.Context, tenantID, id string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := tenantID + ":" + id
	_, ok := s.m[key]
	if ok {
		delete(s.m, key)
	}
	return ok, nil
}
